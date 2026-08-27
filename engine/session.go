package engine

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

//go:generate cp ../biloba.js biloba.js
//go:embed biloba.js
var bilobaJS string

type Cookie struct {
	Name     string
	Value    string
	Domain   string
	Path     string
	Expires  time.Time
	Secure   bool
	HTTPOnly bool
	SameSite string

	// Session is populated by reads only, and is true for a cookie with no expiration.  Setting it
	// has no effect: a set cookie is a session cookie exactly when Expires is the zero time.
	Session bool
}

type Diagnostics struct {
	DOMOutline     string
	ScreenshotPath string
}

// Session is an isolated root tab. Operations on one session are serialized.
type Session struct {
	browser          *Browser
	ctx              context.Context
	cancel           context.CancelFunc
	browserContextID cdp.BrowserContextID
	targetID         target.ID
	openerID         target.ID
	ownsContext      bool
	artifactDir      string
	mu               sync.Mutex
	requestMu        sync.Mutex
	requests         []Request
	consoleMu        sync.Mutex
	consoleMessages  []ConsoleMessage
	holdMu           sync.Mutex
	holds            map[string]*responseHold
	holdOrder        []string
	holdSequence     uint64
	fetchEnabled     bool
	closed           bool
	installed        bool
	root             *Session
	initScriptIDs    []page.ScriptIdentifier
	// crashed is closed by Chrome's Inspector.targetCrashed listener, which runs on chromedp's event
	// goroutine rather than under mu - hence its own lock.  A channel rather than a bool because an
	// operation already in flight has to be interrupted, not merely refused next time: CDP does not
	// fail calls to a dead renderer, it stops answering them, so anything already waiting would sit
	// there until its deadline and report a timeout.  Recovery replaces the channel.
	crashMu sync.Mutex
	crashed chan struct{}
}

// ContextID identifies the isolated browser context shared by this session and its descendants.
func (s *Session) ContextID() cdp.BrowserContextID { return s.browserContextID }

// TargetID identifies this session's page target.
func (s *Session) TargetID() target.ID { return s.targetID }

// OpenerID identifies the target that opened this tab, or is empty for explicitly-created tabs.
func (s *Session) OpenerID() target.ID { return s.openerID }

// OwnsContext reports whether closing this session disposes its isolated browser context.
func (s *Session) OwnsContext() bool { return s.ownsContext }

// crashSignal is closed once this session's renderer dies, and replaced when a navigation recovers.
func (s *Session) crashSignal() <-chan struct{} {
	s.crashMu.Lock()
	defer s.crashMu.Unlock()
	if s.crashed == nil {
		s.crashed = make(chan struct{})
	}
	return s.crashed
}

func (s *Session) markCrashed() {
	s.crashMu.Lock()
	defer s.crashMu.Unlock()
	if s.crashed == nil {
		s.crashed = make(chan struct{})
	}
	select {
	case <-s.crashed: // already reported
	default:
		close(s.crashed)
	}
}

func (s *Session) clearCrashed() {
	s.crashMu.Lock()
	defer s.crashMu.Unlock()
	s.crashed = make(chan struct{})
}

func (s *Session) hasCrashed() bool {
	select {
	case <-s.crashSignal():
		return true
	default:
		return false
	}
}

func (s *Session) Close() error {
	if s.ownsContext && s.browser != nil {
		ctx, cancel := context.WithTimeout(s.browser.ctx, 5*time.Second)
		_ = s.browser.closeContextDescendants(ctx, s)
		cancel()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.releaseAllResponseHolds()
	var disposeErr error
	if s.browser != nil && s.ownsContext {
		ctx, cancel := context.WithTimeout(s.browser.ctx, 5*time.Second)
		disposeErr = s.withBrowserExecutor(ctx, func(browserCtx context.Context) error {
			return target.DisposeBrowserContext(s.browserContextID).Do(browserCtx)
		})
		cancel()
	} else if s.browser != nil {
		ctx, cancel := context.WithTimeout(s.browser.ctx, 5*time.Second)
		disposeErr = s.withBrowserExecutor(ctx, func(browserCtx context.Context) error {
			return target.CloseTarget(s.targetID).Do(browserCtx)
		})
		cancel()
	}
	s.cancel()
	s.mu.Unlock()
	if s.browser != nil {
		s.browser.removeSession(s)
	}
	if disposeErr != nil && !errors.Is(disposeErr, context.Canceled) {
		return contextError("close session", disposeErr)
	}
	return nil
}

// NewTab opens another target in this session's browser context, sharing cookies and storage.
func (s *Session) NewTab(ctx context.Context) (*Session, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, &Error{Code: CodeSessionClosed, Operation: "new tab", Message: "session is closed"}
	}
	browser := s.browser
	browserContextID := s.browserContextID
	s.mu.Unlock()
	if browser == nil {
		return nil, &Error{Code: CodeSessionClosed, Operation: "new tab", Message: "browser is closed"}
	}
	return browser.openTab(ctx, browserContextID, false, s.contextRoot())
}

func (s *Session) contextRoot() *Session {
	if s.root != nil {
		return s.root
	}
	return s
}

// AddInitScript installs JavaScript that runs before every future document in this tab.
func (s *Session) AddInitScript(ctx context.Context, script string) error {
	return s.serial(ctx, "add init script", func(opCtx context.Context) error {
		return chromedp.Run(opCtx, chromedp.ActionFunc(func(runCtx context.Context) error {
			identifier, err := page.AddScriptToEvaluateOnNewDocument(script).Do(runCtx)
			if err == nil {
				s.initScriptIDs = append(s.initScriptIDs, identifier)
			}
			return err
		}))
	})
}

// Activate brings this tab to the foreground.
func (s *Session) Activate(ctx context.Context) error {
	return s.serial(ctx, "activate tab", func(opCtx context.Context) error {
		return s.withBrowserExecutor(opCtx, func(browserCtx context.Context) error {
			return target.ActivateTarget(s.targetID).Do(browserCtx)
		})
	})
}

func (s *Session) Prepare(ctx context.Context) error {
	if s.ownsContext && s.browser != nil {
		if err := s.browser.closeContextDescendants(ctx, s); err != nil {
			return err
		}
	}
	return s.serial(ctx, "prepare", func(opCtx context.Context) error {
		if err := s.resetResponseHolds(opCtx); err != nil {
			return err
		}
		s.clearRequests()
		s.clearConsoleMessages()
		for _, identifier := range s.initScriptIDs {
			if err := chromedp.Run(opCtx, page.RemoveScriptToEvaluateOnNewDocument(identifier)); err != nil {
				return err
			}
		}
		s.initScriptIDs = nil
		// Clear storage while the tab still has its current origin. about:blank has an opaque
		// origin, so navigating first would leave localStorage behind for the next spec. A crashed
		// renderer has already lost that storage and no longer answers evaluation requests, so skip
		// straight to the browser-scoped cleanup and recovery navigation in that case.
		if !s.hasCrashed() {
			_ = EvaluateContext(opCtx, `try { window.localStorage.clear(); window.sessionStorage.clear(); } catch (e) {}`, false, nil)
		}
		if err := ClearCookiesContext(opCtx, s.browserContextID); err != nil {
			return err
		}
		if err := s.resetEmulation(opCtx); err != nil {
			return err
		}
		navigateBlank := func() error {
			return chromedp.Run(opCtx, chromedp.ActionFunc(func(runCtx context.Context) error {
				_, _, _, _, err := page.Navigate("about:blank").Do(runCtx)
				return err
			}))
		}
		err := navigateBlank()
		if err != nil && s.hasCrashed() {
			err = navigateBlank()
		}
		if err != nil {
			return err
		}
		s.installed = false
		s.clearCrashed()
		return nil
	})
}

// Navigate loads a URL and requires the main document response to have HTTP status 200.
func (s *Session) Navigate(ctx context.Context, destination string) error {
	return s.NavigateWithStatus(ctx, destination, http.StatusOK)
}

// NavigateWithStatus loads a URL and requires the main document response to have expectedStatus.
// A 4xx or 5xx page that renders perfectly good HTML is a legitimate thing to test - this is how you
// say so, and it mirrors the Go runner's Biloba.NavigateWithStatus.  Navigate's insistence on 200 is
// an assertion, not a transport rule: unexpected error pages are a broken fixture far more often
// than they are the subject of the test, and letting one through surfaces later as a confusing
// downstream failure instead of at the navigation that caused it.
func (s *Session) NavigateWithStatus(ctx context.Context, destination string, expectedStatus int) error {
	return s.serial(ctx, "navigate", func(opCtx context.Context) error {
		result, err := NavigateContext(opCtx, destination)
		// A navigation gives the target a fresh renderer, which is how a crashed page recovers - but
		// Chrome needs a beat to spawn one, and a navigation issued before it exists comes back
		// ERR_ABORTED.  Retrying once is what makes "navigate again to recover it" true advice
		// rather than something that works only if you happen to wait first.
		if err != nil && s.hasCrashed() {
			result, err = NavigateContext(opCtx, destination)
		}
		s.installed = false
		if err == nil {
			s.clearCrashed()
		}
		// Chrome reports a 4xx/5xx document as a loading failure, so that particular error is not
		// a navigation failure - the status we observed is what decides.  Any other error is.
		if err != nil && !result.HTTPFailure {
			return err
		}
		if result.Status != 0 {
			if result.Status != expectedStatus {
				return &Error{Code: CodeNavigation, Operation: "navigate", Message: fmt.Sprintf("expected HTTP status %d", expectedStatus), Observed: result.Status}
			}
			return nil
		}
		// No document response was observed; an HTTP loading failure we could not corroborate
		// with a status still has to surface rather than passing as a successful navigation.
		return err
	})
}

func (s *Session) SetCookies(ctx context.Context, cookies []Cookie) error {
	return s.serial(ctx, "set cookies", func(opCtx context.Context) error {
		var location string
		if err := chromedp.Run(opCtx, chromedp.Location(&location)); err != nil {
			return err
		}
		return SetCookiesContext(opCtx, s.browserContextID, location, cookies)
	})
}

// GetCookies reads every cookie in this session's isolated browser context.
func (s *Session) GetCookies(ctx context.Context) ([]Cookie, error) {
	var cookies []Cookie
	err := s.serial(ctx, "get cookies", func(opCtx context.Context) error {
		var readErr error
		cookies, readErr = GetCookiesContext(opCtx, s.browserContextID)
		return readErr
	})
	return cookies, err
}

// ClearCookies clears every cookie in this session's isolated browser context.
func (s *Session) ClearCookies(ctx context.Context) error {
	return s.serial(ctx, "clear cookies", func(opCtx context.Context) error {
		return ClearCookiesContext(opCtx, s.browserContextID)
	})
}

func (s *Session) Evaluate(ctx context.Context, script string) (any, error) {
	return s.evaluate(ctx, script, false)
}

// EvaluateAsync evaluates script and waits for a returned promise to settle.
func (s *Session) EvaluateAsync(ctx context.Context, script string) (any, error) {
	return s.evaluate(ctx, script, true)
}

func (s *Session) evaluate(ctx context.Context, script string, awaitPromise bool) (any, error) {
	var result any
	err := s.serial(ctx, "evaluate", func(opCtx context.Context) error {
		return EvaluateContext(opCtx, script, awaitPromise, &result)
	})
	return result, err
}

// SetWindowSize changes this session's emulated viewport.
func (s *Session) SetWindowSize(ctx context.Context, width, height int) error {
	if width <= 0 || height <= 0 {
		return &Error{Code: CodeInvalidArgument, Operation: "set window size", Message: "width and height must be positive"}
	}
	return s.serial(ctx, "set window size", func(opCtx context.Context) error {
		return EmulateViewportContext(opCtx, width, height)
	})
}

// SetUpload attaches paths to the first file input matching selector.
func (s *Session) SetUpload(ctx context.Context, selector Selector, paths []string) (Observation, error) {
	found := false
	err := s.serial(ctx, "set upload", func(opCtx context.Context) error {
		if len(paths) == 0 {
			return &Error{Code: CodeInvalidArgument, Operation: "set upload", Message: "at least one path is required"}
		}
		if err := s.ensureBiloba(opCtx); err != nil {
			return err
		}
		arguments, err := EncodeArgs(selector.Encoded())
		if err != nil {
			return err
		}
		found, err = SetFileInputFilesContext(opCtx, fmt.Sprintf("_biloba.node(...%s)", arguments), paths)
		return err
	})
	return Observation{Found: &found}, err
}

func (s *Session) Click(ctx context.Context, selector Selector) error {
	_, err := s.handler(ctx, "click", selector)
	return err
}

func (s *Session) SetValue(ctx context.Context, selector Selector, value any) error {
	_, err := s.handler(ctx, "setValue", selector, value)
	return err
}

func (s *Session) RealisticClick(ctx context.Context, selector Selector) error {
	return s.serial(ctx, "realistic click", func(opCtx context.Context) error {
		return s.realisticClick(opCtx, selector)
	})
}

// DragTo resolves stable, actionable centers for both endpoints and dispatches trusted pointer
// input through Chrome. It is intended for pointer-driven drag libraries rather than native HTML5
// draggable elements.
func (s *Session) DragTo(ctx context.Context, source, target Selector) error {
	return s.serial(ctx, "drag to", func(opCtx context.Context) error {
		sourcePoint, err := s.actionablePoint(opCtx, source)
		if err != nil {
			return err
		}
		targetPoint, err := s.actionablePoint(opCtx, target)
		if err != nil {
			return err
		}
		return DragContext(opCtx, sourcePoint.x, sourcePoint.y, targetPoint.x, targetPoint.y, 15)
	})
}

func (s *Session) RealisticSetValue(ctx context.Context, selector Selector, value any) error {
	return s.serial(ctx, "realistic set value", func(opCtx context.Context) error {
		kind, err := s.runHandler(opCtx, "inputKind", selector)
		if err != nil {
			return err
		}
		switch fmt.Sprint(kind.Result) {
		case "checkbox":
			desired, ok := value.(bool)
			if !ok {
				return &Error{Code: CodeInvalidArgument, Operation: "realistic set value", Message: "checkboxes only accept boolean values"}
			}
			current, currentErr := s.runHandler(opCtx, "getValue", selector)
			if currentErr != nil {
				return currentErr
			}
			if current.Result == desired {
				return nil
			}
			return s.realisticClick(opCtx, selector)
		case "text":
			if err := s.realisticClick(opCtx, selector); err != nil {
				return err
			}
			if _, err := s.runHandler(opCtx, "setProperty", selector, "value", ""); err != nil {
				return err
			}
			if err := KeyEventContext(opCtx, fmt.Sprint(value)); err != nil {
				return err
			}
			_, err := s.runHandler(opCtx, "blur", selector)
			return err
		default:
			_, err := s.runHandler(opCtx, "setValue", selector, value)
			return err
		}
	})
}

func (s *Session) Type(ctx context.Context, selector Selector, keys string, realistic bool) error {
	return s.serial(ctx, "type", func(opCtx context.Context) error {
		if realistic {
			if _, err := s.runHandler(opCtx, "scrollIntoView", selector); err != nil {
				return err
			}
		}
		if _, err := s.runHandler(opCtx, "focus", selector); err != nil {
			return err
		}
		return KeyEventContext(opCtx, keys)
	})
}

func (s *Session) SendKeys(ctx context.Context, keys string) error {
	return s.serial(ctx, "send keys", func(opCtx context.Context) error {
		return KeyEventContext(opCtx, keys)
	})
}

func (s *Session) Visible(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "isVisible", selector)
	return response.observation(response.Success), err
}

func (s *Session) Exists(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "exists", selector)
	return response.observation(response.Success), err
}

func (s *Session) Enabled(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "isEnabled", selector)
	return response.observation(response.Success), err
}

func (s *Session) Clickable(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "isClickable", selector)
	return response.observation(response.Success), err
}

func (s *Session) Text(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "getProperty", selector, "innerText")
	return response.observation(response.Result), err
}

func (s *Session) Count(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "count", selector)
	return response.observation(intValue(response.Result)), err
}

func (s *Session) Attribute(ctx context.Context, selector Selector, name string) (Observation, error) {
	response, err := s.handler(ctx, "getAttribute", selector, name)
	return response.observation(response.Result), err
}

func (s *Session) Property(ctx context.Context, selector Selector, name string) (Observation, error) {
	response, err := s.handler(ctx, "getProperty", selector, name)
	return response.observation(response.Result), err
}

func (s *Session) TextForEach(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "getPropertyForEach", selector, "textContent")
	return response.observation(response.Result), err
}

func (s *Session) Value(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "getValue", selector)
	return response.observation(response.Result), err
}

func (s *Session) URL(ctx context.Context) (Observation, error) {
	var location string
	err := s.serial(ctx, "read URL", func(opCtx context.Context) error {
		var err error
		location, err = LocationContext(opCtx)
		return err
	})
	return Observation{Value: location}, err
}

func (s *Session) CaptureDiagnostics(ctx context.Context, prefix string) (Diagnostics, error) {
	var diagnostics Diagnostics
	err := s.serial(ctx, "capture diagnostics", func(opCtx context.Context) error {
		if err := s.ensureBiloba(opCtx); err != nil {
			return err
		}
		response, err := s.runHandler(opCtx, "outline", Selector{})
		if err == nil {
			outline, _ := response.Result.(string)
			diagnostics.DOMOutline = capOutline(outline)
		}
		path := artifactPath(s.artifactDir, prefix, "png")
		if path == "" {
			return err
		}
		if mkdirErr := os.MkdirAll(s.artifactDir, 0o755); mkdirErr != nil {
			return mkdirErr
		}
		var image []byte
		if screenshotErr := chromedp.Run(opCtx, chromedp.ActionFunc(func(runCtx context.Context) error {
			var captureErr error
			image, captureErr = page.CaptureScreenshot().WithCaptureBeyondViewport(false).Do(runCtx)
			return captureErr
		})); screenshotErr != nil {
			return screenshotErr
		}
		if writeErr := os.WriteFile(path, image, 0o644); writeErr != nil {
			return writeErr
		}
		diagnostics.ScreenshotPath = path
		return err
	})
	return diagnostics, err
}

// outlineMaxBytes and outlineTruncationMarker mirror the root package's outline.go.  The engine
// cannot import the root package (the root imports the engine), so the cap is restated here rather
// than shared - it has to stay character-for-character identical to the runner's, since a truncated
// outline is a failure artifact a human compares across the two paths.
const outlineMaxBytes = 32768 // 32 KB hard cap (default; override with BILOBA_OUTLINE_MAX)
const outlineTruncationMarker = "\n... [truncated]"

// outlineCap resolves the byte cap for a captured DOM outline.  By default it is outlineMaxBytes,
// but BILOBA_OUTLINE_MAX lets you raise it (when a failing spec's DOM is truncated right where you
// need it) or disable truncation entirely: set it to a byte count (e.g. "131072") to raise the cap,
// or to "0"/"off"/"none"/"unlimited" to emit the whole outline.  An unparseable value falls back to
// the default.  Keep in sync with the root package's outlineCap.
func outlineCap() int {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("BILOBA_OUTLINE_MAX")))
	if v == "" {
		return outlineMaxBytes
	}
	switch v {
	case "0", "off", "none", "unlimited":
		return -1 // no cap
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return n
	}
	return outlineMaxBytes
}

func capOutline(s string) string {
	return capOutlineWithCap(s, outlineCap())
}

func capOutlineWithCap(s string, maxBytes int) string {
	if maxBytes < 0 || len(s) <= maxBytes {
		return s
	}
	// Find a newline boundary near the cap so we don't cut mid-line.
	cut := strings.LastIndex(s[:maxBytes], "\n")
	if cut < 0 {
		cut = maxBytes
	}
	return s[:cut] + outlineTruncationMarker
}

func (r HandlerResponse) observation(value any) Observation {
	return Observation{Value: value, Found: r.Found}
}

func (s *Session) handler(ctx context.Context, name string, selector Selector, args ...any) (HandlerResponse, error) {
	var response HandlerResponse
	err := s.serial(ctx, name, func(opCtx context.Context) error {
		var err error
		response, err = s.runHandler(opCtx, name, selector, args...)
		return err
	})
	return response, err
}

func (s *Session) runHandler(ctx context.Context, name string, selector Selector, args ...any) (HandlerResponse, error) {
	return s.runHandlerWithAwait(ctx, name, selector, false, args...)
}

func (s *Session) runHandlerAsync(ctx context.Context, name string, selector Selector, args ...any) (HandlerResponse, error) {
	return s.runHandlerWithAwait(ctx, name, selector, true, args...)
}

func (s *Session) runHandlerWithAwait(ctx context.Context, name string, selector Selector, awaitPromise bool, args ...any) (HandlerResponse, error) {
	if err := s.ensureBiloba(ctx); err != nil {
		return HandlerResponse{}, err
	}
	encodedSelector := selector.Encoded()
	if name == "outline" {
		encodedSelector = ""
	}
	run := RunHandlerContext
	if awaitPromise {
		run = RunHandlerAsyncContext
	}
	response, err := run(ctx, name, encodedSelector, args...)
	if err != nil {
		s.installed = false
		if installErr := s.ensureBiloba(ctx); installErr != nil {
			return response, installErr
		}
		response, err = run(ctx, name, encodedSelector, args...)
		if err != nil {
			return response, err
		}
	}
	if response.Err != "" {
		code := CodeActionFailed
		if name != "click" && name != "setValue" {
			code = CodeNotFound
		}
		return response, &Error{Code: code, Operation: name, Message: response.Err, Observed: response.Result}
	}
	if !response.Success {
		return response, &Error{Code: CodeConditionNotMet, Operation: name, Message: "operation did not succeed", Observed: response.Result}
	}
	return response, nil
}

func (s *Session) realisticClick(ctx context.Context, selector Selector) error {
	point, err := s.actionablePoint(ctx, selector)
	if err != nil {
		return err
	}
	return ClickXYContext(ctx, point.x, point.y)
}

type actionPoint struct{ x, y float64 }

func (s *Session) actionablePoint(ctx context.Context, selector Selector) (actionPoint, error) {
	response, err := s.runHandlerAsync(ctx, "scrollToStablePoint", selector)
	if err != nil {
		return actionPoint{}, err
	}
	point, ok := response.Result.(map[string]any)
	if !ok {
		return actionPoint{}, &Error{Code: CodeActionFailed, Operation: "resolve pointer target", Message: fmt.Sprintf("unexpected scroll point: %v", response.Result)}
	}
	x, xOK := number(point["x"])
	y, yOK := number(point["y"])
	if !xOK || !yOK || point["enabled"] != true || point["inViewport"] != true || point["hittable"] != true {
		return actionPoint{}, &Error{Code: CodeActionFailed, Operation: "resolve pointer target", Message: "element is not actionable"}
	}
	return actionPoint{x: x, y: y}, nil
}

func (s *Session) ensureBiloba(ctx context.Context) error {
	if s.installed {
		return nil
	}
	if err := EvaluateContext(ctx, bilobaJS, false, nil); err != nil {
		return err
	}
	s.installed = true
	return nil
}

func (s *Session) serial(requestCtx context.Context, operation string, run func(context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return &Error{Code: CodeSessionClosed, Operation: operation, Message: "session is closed"}
	}
	// Navigate and prepare are how a crashed page comes back, so they always get through; anything
	// else would otherwise sit on a renderer that will never answer until its deadline expires.
	recovers := operation == "navigate" || operation == "prepare"
	pageCrashed := func() *Error {
		return &Error{
			Code:      CodePageCrashed,
			Operation: operation,
			Message:   "the page crashed; navigate the session again to recover it",
		}
	}
	if !recovers && s.hasCrashed() {
		return pageCrashed()
	}
	opCtx, cancel := executorContext(s.ctx, requestCtx)
	defer cancel()
	if !recovers {
		// Interrupt an operation that is already waiting on the renderer when it dies.  Without this
		// the crash is only noticed by the *next* call, and this one still burns its whole deadline.
		crashed := s.crashSignal()
		go func() {
			select {
			case <-crashed:
				cancel()
			case <-opCtx.Done():
			}
		}()
	}
	err := run(opCtx)
	if err == nil {
		return nil
	}
	if !recovers && s.hasCrashed() {
		return pageCrashed()
	}
	var engineErr *Error
	if errors.As(err, &engineErr) {
		return engineErr
	}
	// Both a caller cancelling and the browser exiting arrive here as context.Canceled, because
	// opCtx descends from both the session and the request.  Telling them apart is the whole
	// difference between "you asked me to stop" and "Chrome is gone": only the second is fatal, and
	// only the second should stop a poll rather than be retried until the deadline.
	if s.ctx.Err() != nil && requestCtx.Err() == nil {
		return &Error{
			Code:      CodeBrowserGone,
			Operation: operation,
			Message:   "the browser is no longer available (it exited, crashed, or was closed)",
			Cause:     err,
		}
	}
	return contextError(operation, err)
}

func (s *Session) withBrowserExecutor(ctx context.Context, run func(context.Context) error) error {
	chrome := chromedp.FromContext(s.ctx)
	return run(cdp.WithExecutor(ctx, chrome.Browser))
}

func intValue(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}

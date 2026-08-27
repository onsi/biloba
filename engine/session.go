package engine

import (
	"context"
	_ "embed"
	"errors"
	"os"
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
	artifactDir      string
	mu               sync.Mutex
	closed           bool
	installed        bool
	// crashed is closed by Chrome's Inspector.targetCrashed listener, which runs on chromedp's event
	// goroutine rather than under mu - hence its own lock.  A channel rather than a bool because an
	// operation already in flight has to be interrupted, not merely refused next time: CDP does not
	// fail calls to a dead renderer, it stops answering them, so anything already waiting would sit
	// there until its deadline and report a timeout.  Recovery replaces the channel.
	crashMu sync.Mutex
	crashed chan struct{}
}

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
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	var disposeErr error
	if s.browser != nil {
		ctx, cancel := context.WithTimeout(s.browser.ctx, 5*time.Second)
		disposeErr = s.withBrowserExecutor(ctx, func(browserCtx context.Context) error {
			return target.DisposeBrowserContext(s.browserContextID).Do(browserCtx)
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

func (s *Session) Prepare(ctx context.Context) error {
	return s.serial(ctx, "prepare", func(opCtx context.Context) error {
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
			if result.Status != 200 {
				return &Error{Code: CodeNavigation, Operation: "navigate", Message: "expected HTTP status 200", Observed: result.Status}
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

func (s *Session) Evaluate(ctx context.Context, script string) (any, error) {
	var result any
	err := s.serial(ctx, "evaluate", func(opCtx context.Context) error {
		return EvaluateContext(opCtx, script, false, &result)
	})
	return result, err
}

func (s *Session) Click(ctx context.Context, selector Selector) error {
	_, err := s.handler(ctx, "click", selector)
	return err
}

func (s *Session) SetValue(ctx context.Context, selector Selector, value any) error {
	_, err := s.handler(ctx, "setValue", selector, value)
	return err
}

func (s *Session) Visible(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "isVisible", selector)
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
			diagnostics.DOMOutline, _ = response.Result.(string)
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
	if err := s.ensureBiloba(ctx); err != nil {
		return HandlerResponse{}, err
	}
	encodedSelector := selector.Encoded()
	if name == "outline" {
		encodedSelector = ""
	}
	response, err := RunHandlerContext(ctx, name, encodedSelector, args...)
	if err != nil {
		s.installed = false
		if installErr := s.ensureBiloba(ctx); installErr != nil {
			return response, installErr
		}
		response, err = RunHandlerContext(ctx, name, encodedSelector, args...)
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
		return response, &Error{Code: CodeActionFailed, Operation: name, Message: "operation did not succeed", Observed: response.Result}
	}
	return response, nil
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

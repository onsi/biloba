package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

type BrowserConfig struct {
	ExecutablePath string
	WebSocketURL   string
	Mode           ChromeMode
	Arguments      []string
	WindowWidth    int
	WindowHeight   int
	ArtifactDir    string
	AutoInstall    bool
}

// LaunchMetadata is the resolved process configuration used by a Browser. Attached browsers set
// Attached but leave launch-only fields empty because they did not observe the shared process start.
type LaunchMetadata struct {
	Mode           ChromeMode
	ExecutablePath string
	Arguments      []string
	WindowWidth    int
	WindowHeight   int
	Attached       bool
	AutoInstalled  bool
}

// ChromeMode selects the Chrome process variant without coupling callers to chromedp flags.
type ChromeMode string

const (
	// ChromeModeHeadlessShell uses the lightweight chrome-headless-shell defaults.
	ChromeModeHeadlessShell ChromeMode = "headless-shell"
	// ChromeModeHeadless launches a full Chrome executable in modern headless mode.
	ChromeModeHeadless ChromeMode = "headless"
	// ChromeModeHeadful launches a visible full Chrome executable.
	ChromeModeHeadful ChromeMode = "headful"
)

// Browser owns one supplied Chrome process and opens isolated runner-neutral sessions within it.
type Browser struct {
	ctx          context.Context
	cancel       context.CancelFunc
	artifactDir  string
	mode         ChromeMode
	windowWidth  int
	windowHeight int
	launch       LaunchMetadata
	mu           sync.Mutex
	sessions     map[*Session]struct{}
	closed       bool
	webSocketURL string
}

// StartBrowser starts exactly one Chrome process using the supplied executable.
func StartBrowser(ctx context.Context, config BrowserConfig) (*Browser, error) {
	if config.WebSocketURL != "" {
		return connectBrowser(ctx, config)
	}
	mode := config.Mode
	if mode == "" {
		mode = ChromeModeHeadlessShell
	}
	if mode != ChromeModeHeadlessShell && mode != ChromeModeHeadless && mode != ChromeModeHeadful {
		return nil, &Error{Code: CodeInvalidArgument, Operation: "start browser", Message: "mode must be headless-shell, headless, or headful", Observed: config.Mode}
	}
	type chromeArgument struct {
		name  string
		value any
	}
	arguments := make([]chromeArgument, 0, len(config.Arguments))
	for _, argument := range config.Arguments {
		name, value, parseErr := parseChromeArgument(argument)
		if parseErr != nil {
			return nil, parseErr
		}
		arguments = append(arguments, chromeArgument{name: name, value: value})
	}
	executablePath := config.ExecutablePath
	autoInstalled := false
	if mode == ChromeModeHeadlessShell {
		var resolveErr error
		executablePath, autoInstalled, resolveErr = ResolveHeadlessShell(ctx, executablePath, config.AutoInstall)
		if resolveErr != nil {
			return nil, &Error{Code: CodeBrowserStart, Operation: "start browser", Message: resolveErr.Error(), Cause: resolveErr}
		}
	} else {
		executablePath = LocateFullChrome(executablePath)
	}
	if executablePath == "" {
		return nil, &Error{Code: CodeBrowserStart, Operation: "start browser", Message: "could not find a full Chrome executable; provide ExecutablePath"}
	}
	width, height := config.WindowWidth, config.WindowHeight
	if width <= 0 {
		width = 1024
	}
	if height <= 0 {
		height = 768
	}
	profile, err := os.MkdirTemp("", "biloba-engine-profile-")
	if err != nil {
		return nil, typedError(CodeIO, "start browser", err)
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(executablePath),
		chromedp.WindowSize(width, height),
		chromedp.UserDataDir(profile),
		chromedp.WSURLReadTimeout(60*time.Second),
	)
	switch mode {
	case ChromeModeHeadlessShell:
	case ChromeModeHeadless:
		opts = append(opts, chromedp.Flag("headless", "new"))
	case ChromeModeHeadful:
		opts = append(opts, chromedp.Flag("headless", false))
	}
	for _, argument := range arguments {
		opts = append(opts, chromedp.Flag(argument.name, argument.value))
	}
	allocCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, opts...)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	cancel := func() {
		cancelBrowser()
		cancelAllocator()
		_ = os.RemoveAll(profile)
	}
	if err := allocateBrowserWithoutTab(browserCtx); err != nil {
		cancel()
		return nil, typedError(CodeBrowserStart, "start browser", err)
	}
	wsURL, err := readWebSocketURL(profile)
	if err != nil {
		cancel()
		return nil, typedError(CodeBrowserStart, "read browser websocket URL", err)
	}
	return &Browser{
		ctx: browserCtx, cancel: cancel, artifactDir: config.ArtifactDir,
		sessions: map[*Session]struct{}{}, webSocketURL: wsURL,
		mode: mode, windowWidth: width, windowHeight: height,
		launch: LaunchMetadata{Mode: mode, ExecutablePath: executablePath, Arguments: append([]string(nil), config.Arguments...), WindowWidth: width, WindowHeight: height, AutoInstalled: autoInstalled},
	}, nil
}

func parseChromeArgument(argument string) (string, any, error) {
	if !strings.HasPrefix(argument, "--") || len(argument) <= 2 {
		return "", nil, &Error{Code: CodeInvalidArgument, Operation: "start browser", Message: "Chrome arguments must use --name or --name=value", Observed: argument}
	}
	parts := strings.SplitN(strings.TrimPrefix(argument, "--"), "=", 2)
	if parts[0] == "" {
		return "", nil, &Error{Code: CodeInvalidArgument, Operation: "start browser", Message: "Chrome argument name cannot be empty", Observed: argument}
	}
	if !validChromeArgumentName.MatchString(parts[0]) {
		return "", nil, &Error{Code: CodeInvalidArgument, Operation: "start browser", Message: "Chrome argument name must contain only letters, digits, and hyphens", Observed: argument}
	}
	if len(parts) == 1 {
		return parts[0], true, nil
	}
	return parts[0], parts[1], nil
}

var validChromeArgumentName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*$`)

func connectBrowser(ctx context.Context, config BrowserConfig) (*Browser, error) {
	allocCtx, cancelAllocator := chromedp.NewRemoteAllocator(ctx, config.WebSocketURL, chromedp.NoModifyURL)
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	cancel := func() { cancelBrowser(); cancelAllocator() }
	if err := allocateBrowserWithoutTab(browserCtx); err != nil {
		cancel()
		return nil, typedError(CodeBrowserStart, "connect browser", err)
	}
	return &Browser{
		ctx: browserCtx, cancel: cancel, artifactDir: config.ArtifactDir,
		sessions: map[*Session]struct{}{}, webSocketURL: config.WebSocketURL,
		mode: config.Mode, windowWidth: config.WindowWidth, windowHeight: config.WindowHeight,
		launch: LaunchMetadata{Attached: true},
	}, nil
}

// WebSocketURL is the browser-level DevTools endpoint. It can be passed to
// other Browser instances so independent workers share one Chrome process.
func (b *Browser) WebSocketURL() string { return b.webSocketURL }

// LaunchMetadata returns a defensive snapshot of this browser's resolved launch configuration.
func (b *Browser) LaunchMetadata() LaunchMetadata {
	metadata := b.launch
	metadata.Arguments = append([]string(nil), metadata.Arguments...)
	return metadata
}

func readWebSocketURL(profile string) (string, error) {
	contents, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 2 || lines[0] == "" || lines[1] == "" {
		return "", fmt.Errorf("invalid DevToolsActivePort contents")
	}
	return "ws://127.0.0.1:" + lines[0] + lines[1], nil
}

// allocateBrowserWithoutTab brings the browser connection up on browserCtx without giving it a
// target.  chromedp allocates lazily on the first chromedp.Run, and Run always pairs allocation
// with a target: for a RemoteAllocator (a worker daemon attaching to an already-running Chrome)
// that means Target.createTarget, i.e. a real idle about:blank tab in the *default* browser
// context that would then sit there for the daemon's whole life - N workers sharing one Chrome,
// N stray tabs.  Every browser-scoped command we issue on this context (creating a session's
// isolated browser context and target, disposing it again) is dispatched on the Browser executor
// and needs no target at all, so we allocate explicitly and leave the browser tab-less.
// Allocator/Browser are exported chromedp fields; this is what chromedp.Run itself does, minus
// the target.
//
// Caveat, because it cannot be enforced in code: chromedp's own initContextBrowser also forwards
// the context's browserOpts to Allocate and appends its browserListeners to the new Browser.  Both
// fields are unexported, so this cannot replicate them - which means a chromedp.WithBrowserOption
// (WithLogf/WithErrorf/WithDebugf) or a ListenBrowser registered against the contexts built in
// StartBrowser/connectBrowser would be silently ignored.  Neither is used here today.  If you add
// one, go through chromedp.Run instead and deal with the stray tab another way.
func allocateBrowserWithoutTab(browserCtx context.Context) error {
	chrome := chromedp.FromContext(browserCtx)
	if chrome == nil {
		return errors.New("not a chromedp context")
	}
	if chrome.Browser != nil {
		return nil // already allocated; allocating again would leak the first browser
	}
	browser, err := chrome.Allocator.Allocate(browserCtx)
	if err != nil {
		return err
	}
	chrome.Browser = browser
	return nil
}

func (b *Browser) OpenSession(ctx context.Context) (*Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, &Error{Code: CodeSessionClosed, Operation: "open session", Message: "browser is closed"}
	}
	opCtx, cancel := executorContext(b.ctx, ctx)
	defer cancel()
	// These are browser-scoped commands, so they go straight to the Browser executor rather than
	// through chromedp.Run - Run would give this browser context a target of its own (see
	// allocateBrowserWithoutTab); the only tab we want is the session's.
	chrome := chromedp.FromContext(opCtx)
	browserExecutor := cdp.WithExecutor(opCtx, chrome.Browser)
	browserContextID, err := target.CreateBrowserContext().WithDisposeOnDetach(true).Do(browserExecutor)
	if err != nil {
		return nil, contextError("open session", err)
	}
	keepBrowserContext := false
	defer func() {
		if keepBrowserContext {
			return
		}
		// The request context may be the reason opening failed, so cleanup must use the browser's
		// lifetime context. Without this, a failed target creation or attachment leaves an isolated
		// browser context behind until the daemon disconnects from Chrome.
		cleanupCtx, cleanupCancel := context.WithTimeout(b.ctx, 5*time.Second)
		defer cleanupCancel()
		cleanupExecutor := cdp.WithExecutor(cleanupCtx, chromedp.FromContext(b.ctx).Browser)
		_ = target.DisposeBrowserContext(browserContextID).Do(cleanupExecutor)
	}()
	session, err := b.openTabLocked(ctx, browserContextID, true, nil, browserExecutor)
	if err != nil {
		return nil, err
	}
	keepBrowserContext = true
	return session, nil
}

func (b *Browser) openTab(ctx context.Context, browserContextID cdp.BrowserContextID, ownsContext bool, root *Session) (*Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, &Error{Code: CodeSessionClosed, Operation: "new tab", Message: "browser is closed"}
	}
	opCtx, cancel := executorContext(b.ctx, ctx)
	defer cancel()
	chrome := chromedp.FromContext(opCtx)
	return b.openTabLocked(ctx, browserContextID, ownsContext, root, cdp.WithExecutor(opCtx, chrome.Browser))
}

func (b *Browser) openTabLocked(ctx context.Context, browserContextID cdp.BrowserContextID, ownsContext bool, root *Session, browserExecutor context.Context) (*Session, error) {
	targetID, err := target.CreateTarget("about:blank").
		WithBrowserContextID(browserContextID).
		WithNewWindow(true).
		Do(browserExecutor)
	if err != nil {
		return nil, contextError("open tab", err)
	}
	tabCtx, cancelTab := chromedp.NewContext(b.ctx, chromedp.WithTargetID(targetID))
	attachDone := make(chan error, 1)
	go func() {
		// The first Run owns chromedp's target executor for the session lifetime. It must use the
		// persistent tab context; binding it to a request deadline tears the executor down afterward.
		attachDone <- chromedp.Run(tabCtx, chromedp.Evaluate("1", nil))
	}()
	select {
	case err = <-attachDone:
	case <-ctx.Done():
		cancelTab()
		err = ctx.Err()
	}
	if err != nil {
		cancelTab()
		return nil, contextError("open tab", err)
	}
	session := &Session{
		browser: b, ctx: tabCtx, cancel: cancelTab, browserContextID: browserContextID,
		targetID: targetID, ownsContext: ownsContext, artifactDir: b.artifactDir, root: root,
		initialWidth: b.windowWidth, initialHeight: b.windowHeight,
		highFidelity: b.mode == ChromeModeHeadless, cacheEnabled: true,
	}
	if session.initialWidth <= 0 || session.initialHeight <= 0 {
		width, height, sizeErr := ViewportDimensionsContext(tabCtx)
		if sizeErr != nil {
			cancelTab()
			return nil, contextError("read initial window size", sizeErr)
		}
		session.initialWidth, session.initialHeight = int(width), int(height)
	}
	if err := session.applyViewport(tabCtx, session.initialWidth, session.initialHeight); err != nil {
		cancelTab()
		return nil, contextError("apply initial viewport", err)
	}
	if ownsContext {
		session.root = session
	}
	session.eventsEnabled.Store(true)
	b.listenToSession(session)
	if err := session.setupDownloads(tabCtx); err != nil {
		cancelTab()
		if ownsContext {
			_ = os.RemoveAll(session.downloadDir)
		}
		return nil, contextError("configure downloads", err)
	}
	b.sessions[session] = struct{}{}
	return session, nil
}

func (b *Browser) listenToSession(session *Session) {
	// Chrome announces a dead renderer once, on this target, and then goes quiet: subsequent CDP
	// calls neither succeed nor fail, they simply never answer.  Catching the announcement is the
	// only way to tell a crashed page from a slow one.
	chromedp.ListenTarget(session.ctx, func(event any) {
		if _, ok := event.(*inspector.EventTargetCrashed); ok {
			session.markCrashed()
		}
		if request, ok := event.(*network.EventRequestWillBeSent); ok {
			if session.eventsEnabled.Load() {
				session.recordRequest(request)
				if request.Type != network.ResourceTypeWebSocket {
					session.trackRequest(request.RequestID)
				}
			}
		}
		if response, ok := event.(*network.EventResponseReceived); ok {
			if session.eventsEnabled.Load() {
				session.recordResponse(response)
			}
		}
		if finished, ok := event.(*network.EventLoadingFinished); ok {
			session.finishRequest(finished.RequestID)
		}
		if failed, ok := event.(*network.EventLoadingFailed); ok {
			session.finishRequest(failed.RequestID)
		}
		if paused, ok := event.(*fetch.EventRequestPaused); ok {
			session.handlePausedEvent(paused)
		}
		if console, ok := event.(*runtime.EventConsoleAPICalled); ok {
			if session.eventsEnabled.Load() {
				session.recordConsoleMessage(console)
			}
		}
		if dialog, ok := event.(*page.EventJavascriptDialogOpening); ok {
			session.handleDialog(dialog)
		}
		if download, ok := event.(*cdpbrowser.EventDownloadWillBegin); ok {
			session.handleDownloadBegin(download)
		}
		if progress, ok := event.(*cdpbrowser.EventDownloadProgress); ok {
			session.handleDownloadProgress(progress)
		}
	})
}

// Sessions returns a stable snapshot of the targets this Browser currently owns or has discovered.
func (b *Browser) Sessions() []*Session {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*Session, 0, len(b.sessions))
	for session := range b.sessions {
		out = append(out, session)
	}
	return out
}

func (b *Browser) closeContextDescendants(ctx context.Context, root *Session) error {
	// Discover browser-opened descendants before taking the snapshot, so Prepare invalidates handles
	// for explicit siblings and popups alike.
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if !closed {
		_, _ = root.Tabs(ctx)
	}
	var firstErr error
	for _, session := range b.Sessions() {
		if session != root && session.browserContextID == root.browserContextID {
			if err := session.Close(); err != nil {
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

func (b *Browser) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	sessions := make([]*Session, 0, len(b.sessions))
	for session := range b.sessions {
		sessions = append(sessions, session)
	}
	b.mu.Unlock()
	for _, session := range sessions {
		_ = session.Close()
	}
	b.cancel()
	return nil
}

func (b *Browser) removeSession(session *Session) {
	b.mu.Lock()
	delete(b.sessions, session)
	b.mu.Unlock()
}

func (b *Browser) contextHasActiveDownloads(browserContextID cdp.BrowserContextID) bool {
	for _, session := range b.Sessions() {
		if session.browserContextID == browserContextID && session.hasActiveDownloads() {
			return true
		}
	}
	return false
}

func (b *Browser) contextDownloadCount(browserContextID cdp.BrowserContextID, now time.Time) int {
	count := 0
	for _, session := range b.Sessions() {
		if session.browserContextID == browserContextID {
			count += session.activeOrRecentDownloadCount(now)
		}
	}
	return count
}

func executorContext(executorCtx, requestCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(executorCtx)
	if deadline, ok := requestCtx.Deadline(); ok {
		var deadlineCancel context.CancelFunc
		ctx, deadlineCancel = context.WithDeadline(ctx, deadline)
		baseCancel := cancel
		cancel = func() { deadlineCancel(); baseCancel() }
	}
	go func() {
		select {
		case <-requestCtx.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

func typedError(code ErrorCode, operation string, err error) *Error {
	return &Error{Code: code, Operation: operation, Message: err.Error(), Cause: err}
}

func contextError(operation string, err error) *Error {
	code := CodeJavaScript
	if errors.Is(err, context.Canceled) {
		code = CodeCanceled
	} else if errors.Is(err, context.DeadlineExceeded) {
		code = CodeDeadline
	} else if isScriptSyntaxError(err) {
		code = CodeInvalidScript
	}
	return typedError(code, operation, err)
}

// isScriptSyntaxError separates the one JavaScript failure that polling can never resolve from the
// ones it routinely does.  A script that does not parse will not start parsing; a ReferenceError or
// a TypeError, though, is the ordinary shape of "the page has not got there yet" - Biloba retries
// exactly those (a missing element read through querySelector, or biloba.js not yet installed), so
// only the syntax case is fatal.  V8 reports it in the exception message, which is the only place
// CDP surfaces the distinction.
func isScriptSyntaxError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "SyntaxError")
}

func artifactPath(dir, prefix, suffix string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d.%s", prefix, time.Now().UnixNano(), suffix))
}

package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

type BrowserConfig struct {
	ExecutablePath string
	WebSocketURL   string
	WindowWidth    int
	WindowHeight   int
	ArtifactDir    string
}

// Browser owns one supplied Chrome process and opens isolated runner-neutral sessions within it.
type Browser struct {
	ctx          context.Context
	cancel       context.CancelFunc
	artifactDir  string
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
	if config.ExecutablePath == "" {
		return nil, &Error{Code: CodeBrowserStart, Operation: "start browser", Message: "ExecutablePath is required"}
	}
	width, height := config.WindowWidth, config.WindowHeight
	if width <= 0 {
		width = 1920
	}
	if height <= 0 {
		height = 1080
	}
	profile, err := os.MkdirTemp("", "biloba-engine-profile-")
	if err != nil {
		return nil, typedError(CodeIO, "start browser", err)
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(config.ExecutablePath),
		chromedp.WindowSize(width, height),
		chromedp.UserDataDir(profile),
		chromedp.WSURLReadTimeout(60*time.Second),
	)
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
	}, nil
}

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
	}, nil
}

// WebSocketURL is the browser-level DevTools endpoint. It can be passed to
// other Browser instances so independent workers share one Chrome process.
func (b *Browser) WebSocketURL() string { return b.webSocketURL }

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
	targetID, err := target.CreateTarget("about:blank").
		WithBrowserContextID(browserContextID).
		WithNewWindow(true).
		Do(browserExecutor)
	if err != nil {
		return nil, contextError("open session", err)
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
		return nil, contextError("open session", err)
	}
	session := &Session{
		browser: b, ctx: tabCtx, cancel: cancelTab, browserContextID: browserContextID,
		artifactDir: b.artifactDir,
	}
	// Chrome announces a dead renderer once, on this target, and then goes quiet: subsequent CDP
	// calls neither succeed nor fail, they simply never answer.  Catching the announcement is the
	// only way to tell a crashed page from a slow one.
	chromedp.ListenTarget(tabCtx, func(event any) {
		if _, ok := event.(*inspector.EventTargetCrashed); ok {
			session.markCrashed()
		}
	})
	b.sessions[session] = struct{}{}
	keepBrowserContext = true
	return session, nil
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

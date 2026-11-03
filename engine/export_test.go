package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
)

type VisualOperationHooksForTest struct {
	EmulateColorScheme func(context.Context, string) error
	RunHandler         func(context.Context, string, string) (HandlerResponse, error)
}

// SetVisualOperationHooksForTest injects failures at the two visual lifecycle boundaries.
func SetVisualOperationHooksForTest(session *Session, hooks VisualOperationHooksForTest) func() {
	previousEmulate, previousHandler := session.visual.emulate, session.visual.handler
	session.visual.emulate, session.visual.handler = hooks.EmulateColorScheme, hooks.RunHandler
	return func() {
		session.visual.emulate, session.visual.handler = previousEmulate, previousHandler
	}
}

// RecentDownloadWindowForTest is how long a finished download keeps a download slot, so a spec can
// work out when a saturated window closes.
const RecentDownloadWindowForTest = recentDownloadWindow

// SessionContextForTest exposes a session's chromedp target context so engine_test.go can
// exercise the context-level primitives (NavigateContext and friends) against a live tab.
func SessionContextForTest(session *Session) context.Context {
	return session.ctx
}

// HTTPStatusFailureForTest exposes the classifier NavigateContext uses to tell Chrome's
// loading failure for a 4xx/5xx document from a genuine transport/target failure. Which
// responses Chrome reports that way varies by Chrome version, so the classification is pinned
// here rather than by racing a browser that may or may not produce the error.
func HTTPStatusFailureForTest(err error) bool {
	return httpStatusFailure(err)
}

// MarkSessionCrashedForTest records the crash signal without killing a renderer, allowing the
// recovery state machine to be tested without disturbing other specs sharing the test browser.
func MarkSessionCrashedForTest(session *Session) {
	session.markCrashed()
}

func EmitWarningForTest(session *Session, warning Warning) {
	session.recordWarning(warning)
}

// SetHeadlessShellInstallerForTest replaces the network installer for deterministic acquisition specs.
func SetHeadlessShellInstallerForTest(installer func(context.Context) (string, error)) func() {
	previous := headlessShellInstaller
	headlessShellInstaller = installer
	return func() { headlessShellInstaller = previous }
}

// SetChromeLocatorForTest replaces the local-binary search ResolveHeadlessShell consults before
// installing, letting specs force the "not found" branch deterministically instead of depending on
// whether the host happens to have a real chrome-headless-shell on PATH or in a cache root - which
// this suite's own SynchronizedBeforeSuite guarantees it does.
func SetChromeLocatorForTest(locator func(string) string) func() {
	previous := locateChromeForResolve
	locateChromeForResolve = locator
	return func() { locateChromeForResolve = previous }
}

// InstallHeadlessShellArchiveForTest exercises atomic cache publication without network access.
func InstallHeadlessShellArchiveForTest(archivePath, destination, platform string) error {
	return installHeadlessShellArchive(archivePath, destination, platform)
}

// ChromeCacheStagingMarkerForTest exposes the marker a still-extracting install's staging
// directory carries, so specs can construct one without duplicating the literal.
const ChromeCacheStagingMarkerForTest = chromeCacheStagingMarker

// SetChromeCacheRootsForTest replaces the cache roots LocateChrome searches for a cached
// chrome-headless-shell, letting specs point it at hermetic fixture directories instead of
// whatever puppeteer/Biloba caches happen to exist on the host.
func SetChromeCacheRootsForTest(roots []string) func() {
	previous := chromeCacheRoots
	chromeCacheRoots = func() []string { return roots }
	return func() { chromeCacheRoots = previous }
}

// SetSandboxGOOSForTest overrides the GOOS ChromeSandboxDisabled sees, letting specs drive its
// Linux-only branches (and rule them out on other platforms) without running on a matching host.
func SetSandboxGOOSForTest(goos string) func() {
	previous := sandboxGOOS
	sandboxGOOS = func() string { return goos }
	return func() { sandboxGOOS = previous }
}

// SetSandboxEuidForTest overrides the effective UID ChromeSandboxDisabled sees, letting specs
// drive the root-on-Linux branch without actually running as root.
func SetSandboxEuidForTest(euid int) func() {
	previous := sandboxEuid
	sandboxEuid = func() int { return euid }
	return func() { sandboxEuid = previous }
}

// SetSandboxApparmorRestrictedForTest overrides the AppArmor proc-file read ChromeSandboxDisabled
// consults, letting specs drive the restricted/unrestricted/missing-file branches deterministically.
func SetSandboxApparmorRestrictedForTest(restricted bool, err error) func() {
	previous := sandboxApparmorRestricted
	sandboxApparmorRestricted = func() (bool, error) { return restricted, err }
	return func() { sandboxApparmorRestricted = previous }
}

// SetMinResponseBodyTimeoutForTest shortens the floor on intercepted body reads, so a spec can show a
// longer TransformTimeout extending it without waiting five seconds.  Call before the response is
// intercepted; the returned func restores the default.
func SetMinResponseBodyTimeoutForTest(timeout time.Duration) func() {
	previous := minResponseBodyTimeout
	minResponseBodyTimeout = timeout
	return func() { minResponseBodyTimeout = previous }
}

// AccessibilityTreeDuringNavigationForTest runs navigation after the initial document
// check, at the boundary where Chrome would handle the frame-ID-based AX command.
func AccessibilityTreeDuringNavigationForTest(ctx context.Context, frameID cdp.FrameID, navigate func()) ([]*accessibility.Node, error) {
	return accessibilityTreeForFrameContext(ctx, frameID, func(readCtx context.Context, id cdp.FrameID) ([]*accessibility.Node, error) {
		navigate()
		return accessibilityTreeContext(readCtx, id)
	})
}

// ScreencastFramesSoFarForTest reports how many frames a session's recording has received, while it
// runs and after Prepare or Close has discarded it.
func ScreencastFramesSoFarForTest(session *Session, id string) func() int {
	session.screencastMu.Lock()
	defer session.screencastMu.Unlock()
	rec := session.screencasts[id]
	return rec.framesSoFar
}

// BilobaSendsForTest is how many times a session has sent the whole of biloba.js into a page in this
// process, so a spec can tell a navigation that cost a re-send from one that did not.
func BilobaSendsForTest() int64 { return bilobaSends.Load() }

// SetBilobaRegistrationForTest replaces the call that registers biloba.js to run at the start of
// every new document, so a spec can make the registration fail.  Returns a restore func.
func SetBilobaRegistrationForTest(register func(context.Context, string) error) func() {
	previous := registerBilobaOnNewDocument
	registerBilobaOnNewDocument = register
	return func() { registerBilobaOnNewDocument = previous }
}

// LiveScreencastWorkersForTest counts the screencast delivery goroutines still running, so a spec
// can tell that every way a recording ends also ends its goroutine.
func LiveScreencastWorkersForTest() int64 { return liveScreencastWorkers.Load() }

// AttachSpawnedTabForTest attaches a session to targetID the way listing tabs does, and
// SessionBrowserContextForTest reports the browser context a session's tabs open in, so a spec can
// pin what a failed attach leaves behind.
func AttachSpawnedTabForTest(ctx context.Context, root *Session, targetID target.ID) (*Session, error) {
	return root.browser.sessionForTarget(ctx, targetID, "", root)
}

func SessionBrowserContextForTest(session *Session) cdp.BrowserContextID {
	return session.browserContextID
}

// StallAttachForTest holds the first message Chrome sends on the next session attached to targetID,
// and with it every reply behind it on that session's worker, until release is called.  An attach
// therefore gets through (Target.attachToTarget answers on the browser's session) and then stalls in
// chromedp's setup.  stalled is closed once a message is held; held then reports its session.
func StallAttachForTest(ctx context.Context, root *Session, targetID target.ID) (stalled <-chan struct{}, held func() target.SessionID, release func(), err error) {
	hub, _, err := hubFor(root.ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	// the listeners live as long as ctx, on root's browser
	listenCtx, stop := context.WithCancel(root.ctx)
	context.AfterFunc(ctx, stop)
	ctx = listenCtx
	holding, releasing := make(chan struct{}), make(chan struct{})
	var heldSession atomic.Value
	var holdOnce, releaseOnce sync.Once
	release = func() { releaseOnce.Do(func() { close(releasing) }) }
	err = ListenBrowser(ctx, func(event any) {
		attached, ok := event.(*target.EventAttachedToTarget)
		if !ok || attached.TargetInfo == nil || attached.TargetInfo.TargetID != targetID {
			return
		}
		session := attached.SessionID
		hub.add(session, &eventListener{ctx: ctx, fn: func(any) {
			holdOnce.Do(func() {
				heldSession.Store(session)
				close(holding)
				<-releasing
			})
		}})
	}, target.AttachedToTarget.Method)
	held = func() target.SessionID {
		session, _ := heldSession.Load().(target.SessionID)
		return session
	}
	return holding, held, release, err
}

// HasFrameWorldsForTest reports whether root's connection still keeps a frame-world registry for
// session.
func HasFrameWorldsForTest(root *Session, session target.SessionID) bool {
	hub, _, err := hubFor(root.ctx)
	if err != nil {
		return false
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	_, ok := hub.worlds[session]
	return ok
}

// FrameWorldForTest is the unique id of frameID's page world in the registry ctx was tracked with.
func FrameWorldForTest(ctx context.Context, frameID cdp.FrameID) (string, error) {
	world, err := mainFrameWorld(ctx, frameID)
	return world.uniqueID, err
}

// ObserveWritesForTest calls observe with the method and session of every command this process sends
// Chrome, until the returned func is called.
func ObserveWritesForTest(observe func(method string, session target.SessionID)) func() {
	fn := func(msg *cdproto.Message) { observe(string(msg.Method), msg.SessionID) }
	observeWrite.Store(&fn)
	return func() { observeWrite.Store(nil) }
}

// SetAttachAttemptReturnedForTest runs hook as each attach attempt returns, before the probe records
// whether it attached.  Returns a restore func.
func SetAttachAttemptReturnedForTest(hook func()) func() {
	previous := attachAttemptReturned
	attachAttemptReturned = hook
	return func() { attachAttemptReturned = previous }
}

package engine

import (
	"context"
	"sync"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
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

// SetLaunchAttemptTimeoutForTest shortens how long each StartBrowser launch attempt waits for
// Chrome's DevTools line, so a spec can drive the retry with a Chrome that never prints one.
func SetLaunchAttemptTimeoutForTest(timeout time.Duration) func() {
	previous := launchAttemptTimeout
	launchAttemptTimeout = timeout
	return func() { launchAttemptTimeout = previous }
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

// LiveHandoffWorkersForTest counts the Handoff goroutines still running (a screencast's frame
// delivery, in the engine), so a spec can tell that every way a recording ends also ends its goroutine.
func LiveHandoffWorkersForTest() int64 { return liveHandoffWorkers.Load() }

// AttachSpawnedTabForTest attaches a session to targetID the way listing tabs does, and
// SessionBrowserContextForTest reports the browser context a session's tabs open in, so a spec can
// pin what a failed attach leaves behind.
func AttachSpawnedTabForTest(ctx context.Context, root *Session, targetID target.ID) (*Session, error) {
	return root.browser.sessionForTarget(ctx, targetID, "", root)
}

func SessionBrowserContextForTest(session *Session) cdp.BrowserContextID {
	return session.browserContextID
}

// StallAttachForTest holds back every command sent on the next session attached to targetID after its
// first (the Runtime.enable chromedp's attach starts with), until release is called.  An attach
// therefore gets through (Target.attachToTarget answers on the browser's session), has Chrome announce
// the target's contexts, and then stalls in chromedp's setup.  stalled is closed once a command is
// held; held then reports its session.  release sends the held commands.
func StallAttachForTest(ctx context.Context, root *Session, targetID target.ID) (stalled <-chan struct{}, held func() target.SessionID, release func(), err error) {
	// the listener lives as long as ctx, on root's browser
	listenCtx, stop := context.WithCancel(root.ctx)
	context.AfterFunc(ctx, stop)
	holding := make(chan struct{})
	var lock sync.Mutex
	var session target.SessionID
	var sent int
	var heldOn *connection
	var commands []*cdproto.Message
	released := false
	err = ListenBrowser(listenCtx, func(event any) {
		attached, ok := event.(*target.EventAttachedToTarget)
		if !ok || attached.TargetInfo == nil || attached.TargetInfo.TargetID != targetID {
			return
		}
		lock.Lock()
		defer lock.Unlock()
		if session == "" {
			session = attached.SessionID
		}
	})
	if err != nil {
		stop()
		return nil, nil, nil, err
	}
	hook := func(conn *connection, msg *cdproto.Message) bool {
		lock.Lock()
		defer lock.Unlock()
		if released || session == "" || msg.SessionID != session {
			return false
		}
		if sent++; sent == 1 {
			return false
		}
		if heldOn == nil {
			heldOn = conn
			close(holding)
		}
		commands = append(commands, msg)
		return true
	}
	previous := writeHook.Swap(&hook)
	release = sync.OnceFunc(func() {
		stop()
		writeHook.CompareAndSwap(&hook, previous)
		lock.Lock()
		conn := heldOn
		lock.Unlock()
		if conn == nil {
			lock.Lock()
			released = true
			lock.Unlock()
			return
		}
		// under the connection's write lock, so a command sent on the session after this goes after them
		conn.sendHeld(context.Background(), func() []*cdproto.Message {
			lock.Lock()
			defer lock.Unlock()
			released = true
			return commands
		})
	})
	held = func() target.SessionID {
		lock.Lock()
		defer lock.Unlock()
		if heldOn == nil {
			return ""
		}
		return session
	}
	return holding, held, release, nil
}

// SwallowNextCommandForTest keeps the next command with method that is sent on session's target from
// reaching Chrome, so Chrome never answers it; the connection still answers it if the session
// detaches.  swallowed is closed once it has been kept back; sentAfter lists the methods of the
// commands sent on the session after it.
func SwallowNextCommandForTest(session *Session, method string) (swallowed <-chan struct{}, sentAfter func() []string, restore func()) {
	return swallowNextCommand(session, method, false)
}

// LoseNextCommandForTest is SwallowNextCommandForTest for a command the connection forgets too, as it
// would a command sent after chromedp took its session's detach: nothing will ever answer it.
func LoseNextCommandForTest(session *Session, method string) (lost <-chan struct{}, restore func()) {
	lost, _, restore = swallowNextCommand(session, method, true)
	return lost, restore
}

func swallowNextCommand(session *Session, method string, forget bool) (<-chan struct{}, func() []string, func()) {
	sessionID := chromedp.FromContext(session.ctx).Target.SessionID
	done := make(chan struct{})
	var lock sync.Mutex
	swallowed := false
	var after []string
	hook := func(conn *connection, msg *cdproto.Message) bool {
		if msg.SessionID != sessionID {
			return false
		}
		lock.Lock()
		defer lock.Unlock()
		if swallowed {
			after = append(after, string(msg.Method))
			return false
		}
		if string(msg.Method) != method {
			return false
		}
		swallowed = true
		close(done)
		if forget {
			conn.answered(msg.SessionID, msg.ID)
		}
		return true
	}
	previous := writeHook.Swap(&hook)
	sentAfter := func() []string {
		lock.Lock()
		defer lock.Unlock()
		return append([]string(nil), after...)
	}
	return done, sentAfter, func() { writeHook.CompareAndSwap(&hook, previous) }
}

// HasFrameWorldsForTest reports whether root's connection still keeps a frame-world record for
// session.
func HasFrameWorldsForTest(root *Session, session target.SessionID) bool {
	conn, _, err := connectionFor(root.ctx)
	if err != nil {
		return false
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	_, ok := conn.sessions[session]
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
	hook := func(_ *connection, msg *cdproto.Message) bool {
		observe(string(msg.Method), msg.SessionID)
		return false
	}
	previous := writeHook.Swap(&hook)
	return func() { writeHook.CompareAndSwap(&hook, previous) }
}

// SetAttachAttemptReturnedForTest runs hook as each attach attempt returns, before the probe records
// whether it attached.  Returns a restore func.
func SetAttachAttemptReturnedForTest(hook func()) func() {
	previous := attachAttemptReturned
	attachAttemptReturned = hook
	return func() { attachAttemptReturned = previous }
}

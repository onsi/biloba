package engine

import "context"

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

package biloba

import "time"

// CapOutlineForTest exposes capOutlineWithCap so that outline_test.go can
// exercise the truncation path with a small byte cap.
func CapOutlineForTest(s string, maxBytes int) string {
	return capOutlineWithCap(s, maxBytes)
}

// InlineImagesSupportedForTest exposes inlineImagesSupported for screenshots_test.go.
func InlineImagesSupportedForTest() bool {
	return inlineImagesSupported()
}

// InlineScreenshotsEnabledForTest exposes inlineScreenshotsEnabled for screenshots_test.go.
func (b *Biloba) InlineScreenshotsEnabledForTest() bool {
	return b.inlineScreenshotsEnabled()
}

// SanitizeForFilenameForTest exposes sanitizeForFilename for use in screenshots_test.go.
func SanitizeForFilenameForTest(s string) string {
	return sanitizeForFilename(s)
}

// LocateHeadlessShellForTest exposes locateHeadlessShell for headless_shell_test.go.
func LocateHeadlessShellForTest(explicit string) string {
	return locateHeadlessShell(explicit)
}

// HeadlessShellInstructionsForTest exposes headlessShellInstructions for headless_shell_test.go.
func HeadlessShellInstructionsForTest() string {
	return headlessShellInstructions()
}

// ChromeMajorVersionForTest exposes chromeMajorVersion for headless_shell_test.go.
func ChromeMajorVersionForTest(product string) int {
	return chromeMajorVersion(product)
}

// MinimumSupportedChromeMajorForTest exposes minimumSupportedChromeMajor for headless_shell_test.go.
func MinimumSupportedChromeMajorForTest() int {
	return minimumSupportedChromeMajor
}

// ChromeForTestingPlatformForTest exposes chromeForTestingPlatform for headless_shell_test.go.
func ChromeForTestingPlatformForTest() (string, error) {
	return chromeForTestingPlatform()
}

// InstallHeadlessShellForTest exposes installHeadlessShell for headless_shell_test.go.
func InstallHeadlessShellForTest() (string, error) {
	return installHeadlessShell()
}

// SafeAllTabScreenshotsForTest exposes safeAllTabScreenshots for integration tests.
func (b *Biloba) SafeAllTabScreenshotsForTest(width, height int) []TabScreenshotForTest {
	shots := b.safeAllTabScreenshots(width, height)
	out := make([]TabScreenshotForTest, len(shots))
	for i, s := range shots {
		out[i] = TabScreenshotForTest{Title: s.title, FilePath: s.filePath, Failure: s.failure, ImgcatScreenshot: s.imgcatScreenshot}
	}
	return out
}

// FailureArtifactConfigForTest exposes the resolved on-failure artifact configuration
// (after options and environment-driven policy are applied) for screenshots_test.go.
func (b *Biloba) FailureArtifactConfigForTest() (failureOutlines, failureScreenshots, inlineScreenshots bool, screenshotsDir string) {
	return b.failureOutlines, b.failureScreenshots, b.inlineScreenshots, b.screenshotsDir
}

// DefaultAutomationScreenshotsDirForTest exposes the automation default directory constant.
func DefaultAutomationScreenshotsDirForTest() string {
	return defaultAutomationScreenshotsDir
}

// SetAutomationDetectedForTest overrides the CI/agent detector so tests can pin a deterministic
// mode regardless of the environment they happen to run in.  It returns a restore func.
func SetAutomationDetectedForTest(fn func() bool) func() {
	prev := automationDetected
	automationDetected = fn
	return func() { automationDetected = prev }
}

// SetNavigationTimeoutForTest overrides navigationTimeout so navigation_test.go can exercise the
// wedged-navigation path without waiting the full production bound.  Returns a restore func.
func SetNavigationTimeoutForTest(d time.Duration) func() {
	prev := navigationTimeout
	navigationTimeout = d
	return func() { navigationTimeout = prev }
}

// SetTransientReadErrorForTest overrides the location()/title() read-error injection seam so
// navigation_test.go can simulate the transient CDP error a live navigation can legitimately throw
// (e.g. a target swapped mid-navigation) without reproducing the underlying race.  Returns a restore
// func.
func SetTransientReadErrorForTest(fn func() error) func() {
	prev := simulateTransientReadError
	simulateTransientReadError = fn
	return func() { simulateTransientReadError = prev }
}

// SafeAllTabConsoleErrorsForTest exposes safeAllTabConsoleErrors for logging_test.go - the captured
// console.error/console.assert messages Biloba replays at the top of the failure block.
func (b *Biloba) SafeAllTabConsoleErrorsForTest() []string {
	return b.safeAllTabConsoleErrors()
}

// DetachedNodeNoteForTest exposes the poll-trajectory recorder's detached-node annotation (the
// "matched N× then stopped matching" signal) for dom_test.go.
func (b *Biloba) DetachedNodeNoteForTest() string {
	return b.probes.renderDetachedNode()
}

// OccludedClicksNoteForTest exposes this tab's recorded occluded-click notes for interactions_test.go.
func (b *Biloba) OccludedClicksNoteForTest() string {
	return b.occlusions.render()
}

// ResetPollDiagnosticsForTest exposes resetPollDiagnostics so a spec can start from a clean slate.
func (b *Biloba) ResetPollDiagnosticsForTest() {
	b.resetPollDiagnostics()
}

// ShadowedHandlersNoteForTest exposes this tab's shadowed-network-handler note for network_test.go.
func (b *Biloba) ShadowedHandlersNoteForTest() string {
	return b.renderShadowedHandlers()
}

// SetVisualDirsForTest points the root's visual-regression directories - the committed baselines and
// the (gitignored) artifacts destination HaveScreenshot writes actual/diff PNGs to - at
// test-controlled locations, so visual_test.go can generate its baselines into a per-spec TempDir
// instead of committing pixels that vary by machine.  Returns a restore func: the suite's b is shared
// and created once, so a leaked value would corrupt every later spec.
func (b *Biloba) SetVisualDirsForTest(baselinesDir string, screenshotsDir string) func() {
	prevBaselines, prevScreenshots := b.root.baselinesDir, b.root.screenshotsDir
	b.root.baselinesDir, b.root.screenshotsDir = baselinesDir, screenshotsDir
	return func() { b.root.baselinesDir, b.root.screenshotsDir = prevBaselines, prevScreenshots }
}

// SetInlineScreenshotsForTest flips the shared root's inline-screenshots flag - the one
// BilobaConfigInlineScreenshots sets and automation detection clears - so visual_test.go can assert
// both sides of the human/agent gate on the diff image without standing up a second Biloba.  Returns
// a restore func.
func (b *Biloba) SetInlineScreenshotsForTest(enabled bool) func() {
	prev := b.root.inlineScreenshots
	b.root.inlineScreenshots = enabled
	return func() { b.root.inlineScreenshots = prev }
}

// SetUpdateScreenshotsForTest flips the BILOBA_UPDATE_SCREENSHOTS behavior for visual_test.go.
// Returns a restore func.
func (b *Biloba) SetUpdateScreenshotsForTest(update bool) func() {
	prev := b.root.updateScreenshots
	b.root.updateScreenshots = update
	return func() { b.root.updateScreenshots = prev }
}

// ScreenshotSettleScheduleForTest exposes the gaps update mode leaves between the captures it
// compares, in order, so visual_test.go can assert on the schedule itself rather than on wall-clock
// timings that a loaded machine makes noisy.
func ScreenshotSettleScheduleForTest() []time.Duration {
	schedule := []time.Duration{}
	for n := range screenshotSettleAttempts - 1 {
		schedule = append(schedule, screenshotSettleGap(n))
	}
	return schedule
}

// EmulateColorSchemeForTest applies (or, with the empty string, clears) a prefers-color-scheme
// override on this tab, so visual_test.go can simulate the capture whose deferred teardown never
// landed and assert that the next Prepare() cleans it up.
func (b *Biloba) EmulateColorSchemeForTest(scheme string) error {
	return b.emulateColorScheme(scheme)
}

// TruthyEnvForTest exposes truthyEnv (and its unrecognised-value warning) for visual_test.go,
// so the spelling table can be exercised without standing up a Biloba per spelling.
func (b *Biloba) TruthyEnvForTest(name string) bool { return b.truthyEnv(name) }

// ScreenshotToleranceForTest exposes the resolved suite-wide visual tolerance.
func (b *Biloba) ScreenshotToleranceForTest() (fraction float64, channel int) {
	return b.root.screenshotTolerance.fraction, b.root.screenshotTolerance.channel
}

// UpdateScreenshotsForTest exposes the resolved BILOBA_UPDATE_SCREENSHOTS setting.
func (b *Biloba) UpdateScreenshotsForTest() bool { return b.root.updateScreenshots }

// VisualBaselinesDirForTest exposes the resolved baselines directory.
func (b *Biloba) VisualBaselinesDirForTest() string { return b.visualBaselinesDir() }

// VisualArtifactsDirForTest exposes the resolved actual/diff artifacts directory.
func (b *Biloba) VisualArtifactsDirForTest() string { return b.visualArtifactsDir() }

// TabScreenshotForTest is a test-accessible view of tabScreenshot.
type TabScreenshotForTest struct {
	Title            string
	FilePath         string
	Failure          string
	ImgcatScreenshot string
}

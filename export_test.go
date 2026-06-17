package biloba

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

// SafeAllTabConsoleErrorsForTest exposes safeAllTabConsoleErrors for logging_test.go - the captured
// console.error/console.assert messages Biloba replays at the top of the failure block.
func (b *Biloba) SafeAllTabConsoleErrorsForTest() []string {
	return b.safeAllTabConsoleErrors()
}

// TabScreenshotForTest is a test-accessible view of tabScreenshot.
type TabScreenshotForTest struct {
	Title            string
	FilePath         string
	Failure          string
	ImgcatScreenshot string
}

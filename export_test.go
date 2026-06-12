package biloba

// CapOutlineForTest exposes capOutlineWithCap so that outline_test.go can
// exercise the truncation path with a small byte cap.
func CapOutlineForTest(s string, maxBytes int) string {
	return capOutlineWithCap(s, maxBytes)
}

// SanitizeForFilenameForTest exposes sanitizeForFilename for use in screenshots_test.go.
func SanitizeForFilenameForTest(s string) string {
	return sanitizeForFilename(s)
}

// SafeAllTabScreenshotsForTest exposes safeAllTabScreenshots for integration tests.
func (b *Biloba) SafeAllTabScreenshotsForTest(width, height int) []TabScreenshotForTest {
	shots := b.safeAllTabScreenshots(width, height)
	out := make([]TabScreenshotForTest, len(shots))
	for i, s := range shots {
		out[i] = TabScreenshotForTest{Title: s.title, FilePath: s.filePath, Failure: s.failure}
	}
	return out
}

// TabScreenshotForTest is a test-accessible view of tabScreenshot.
type TabScreenshotForTest struct {
	Title    string
	FilePath string
	Failure  string
}

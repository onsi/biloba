package biloba

// CapOutlineForTest exposes capOutlineWithCap so that outline_test.go can
// exercise the truncation path with a small byte cap.
func CapOutlineForTest(s string, maxBytes int) string {
	return capOutlineWithCap(s, maxBytes)
}

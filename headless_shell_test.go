package biloba_test

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("locating chrome-headless-shell", Label("no-browser"), func() {
	var origEnv string
	BeforeEach(func() {
		origEnv = os.Getenv("BILOBA_CHROME_HEADLESS_SHELL")
	})
	AfterEach(func() {
		os.Setenv("BILOBA_CHROME_HEADLESS_SHELL", origEnv)
	})

	It("returns an explicitly-provided binary path when it exists", func() {
		os.Unsetenv("BILOBA_CHROME_HEADLESS_SHELL")
		fake := filepath.Join(GinkgoT().TempDir(), "chrome-headless-shell")
		Expect(os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755)).To(Succeed())
		Expect(biloba.LocateHeadlessShellForTest(fake)).To(Equal(fake))
	})

	It("ignores an explicit path that does not exist (falls through to the search)", func() {
		os.Unsetenv("BILOBA_CHROME_HEADLESS_SHELL")
		missing := filepath.Join(GinkgoT().TempDir(), "nope")
		// the result must never be the bogus path
		Expect(biloba.LocateHeadlessShellForTest(missing)).ToNot(Equal(missing))
	})

	It("ignores an explicit path that is a directory", func() {
		os.Unsetenv("BILOBA_CHROME_HEADLESS_SHELL")
		dir := GinkgoT().TempDir()
		Expect(biloba.LocateHeadlessShellForTest(dir)).ToNot(Equal(dir))
	})

	It("honors the BILOBA_CHROME_HEADLESS_SHELL environment variable", func() {
		fake := filepath.Join(GinkgoT().TempDir(), "chrome-headless-shell")
		Expect(os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755)).To(Succeed())
		os.Setenv("BILOBA_CHROME_HEADLESS_SHELL", fake)
		Expect(biloba.LocateHeadlessShellForTest("")).To(Equal(fake))
	})
})

var _ = Describe("chrome-headless-shell acquisition helpers", Label("no-browser"), func() {
	It("maps common platforms to Chrome for Testing identifiers", func() {
		platform, err := biloba.ChromeForTestingPlatformForTest()
		// the test host is one of the supported platforms
		switch runtime.GOOS {
		case "darwin", "linux", "windows":
			Expect(err).ShouldNot(HaveOccurred())
			Expect(platform).ShouldNot(BeEmpty())
		default:
			Expect(err).Should(HaveOccurred())
		}
	})

	It("produces actionable instructions when the shell cannot be found", func() {
		msg := biloba.HeadlessShellInstructionsForTest()
		Expect(msg).To(ContainSubstring("chrome-headless-shell"))
		Expect(msg).To(ContainSubstring("BILOBA_CHROME_HEADLESS_SHELL"))
		Expect(msg).To(ContainSubstring("AutoInstallHeadlessShell"))
		Expect(msg).To(ContainSubstring("HighFidelityHeadless"))
	})
})

var _ = Describe("parsing the Chrome version", Label("no-browser"), func() {
	DescribeTable("extracting the major version from a Browser.getVersion product string",
		func(product string, expected int) {
			Expect(biloba.ChromeMajorVersionForTest(product)).To(Equal(expected))
		},
		Entry("headless shell", "HeadlessChrome/150.0.7871.24", 150),
		Entry("full chrome", "Chrome/150.0.7871.24", 150),
		Entry("a two-digit major", "Chrome/99.0.4844.51", 99),
		Entry("an empty string", "", 0),
		Entry("no slash", "Chrome", 0),
		Entry("a trailing slash with no version", "Chrome/", 0),
		Entry("a non-numeric version", "Chrome/abc.0.0", 0),
	)

	It("uses a minimum supported major that is a sane, non-zero floor", func() {
		// guards against the constant accidentally going to zero (which would disable the warning)
		Expect(biloba.MinimumSupportedChromeMajorForTest()).To(BeNumerically(">=", 100))
	})
})

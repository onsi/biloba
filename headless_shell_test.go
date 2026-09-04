package biloba_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/onsi/biloba"
	"github.com/onsi/biloba/engine"
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

	It("finds a shell in the current Puppeteer cache layout", func() {
		os.Unsetenv("BILOBA_CHROME_HEADLESS_SHELL")
		GinkgoT().Setenv("PATH", "")
		cacheRoot := filepath.Join(GinkgoT().TempDir(), ".cache", "puppeteer")
		GinkgoT().Setenv("HOME", filepath.Dir(filepath.Dir(cacheRoot)))
		fake := filepath.Join(cacheRoot, "chrome-headless-shell", "999.0.0", "chrome-headless-shell-test", biloba.HeadlessShellBinaryNameForTest())
		Expect(os.MkdirAll(filepath.Dir(fake), 0o755)).To(Succeed())
		Expect(os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755)).To(Succeed())

		Expect(biloba.LocateHeadlessShellForTest("")).To(Equal(fake))
	})
})

var _ = Describe("chrome-headless-shell acquisition helpers", Label("no-browser"), func() {
	It("delegates local resolution and installation policy to the engine", func() {
		GinkgoT().Setenv(engine.ChromeEnvVar, "")
		GinkgoT().Setenv("PATH", "")
		GinkgoT().Setenv("HOME", GinkgoT().TempDir())
		GinkgoT().Setenv("XDG_CACHE_HOME", GinkgoT().TempDir())
		calls := []bool{}
		restore := biloba.SetHeadlessShellResolverForTest(func(_ context.Context, explicit string, autoInstall bool) (string, bool, error) {
			Expect(explicit).To(Equal("/configured/shell"))
			calls = append(calls, autoInstall)
			if !autoInstall {
				return "", false, errors.New("engine did not find a shell")
			}
			return "/engine/cache/shell", true, nil
		})
		DeferCleanup(restore)

		path, err := biloba.ResolveHeadlessShellForTest(gt, "/configured/shell", false)
		Expect(path).To(BeEmpty())
		Expect(err).To(MatchError(And(
			ContainSubstring("AutoInstallHeadlessShell"),
			Not(ContainSubstring("engine did not find a shell")),
		)))
		Expect(string(gt.buffer.Contents())).To(BeEmpty(), "default resolution must not announce a download")

		path, err = biloba.ResolveHeadlessShellForTest(gt, "/configured/shell", true)
		Expect(err).NotTo(HaveOccurred())
		Expect(path).To(Equal("/engine/cache/shell"))
		Expect(calls).To(Equal([]bool{false, true}))
		Expect(string(gt.buffer.Contents())).To(ContainSubstring("downloading it via Chrome for Testing"))
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

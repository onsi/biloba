package engine_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("LocateChrome's cache search", func() {
	var puppeteerRoot, biloba1Root, biloba2Root string

	// writeFakeShell drops a fake (non-empty, executable) chrome-headless-shell binary at
	// <root>/chrome-headless-shell/<versionDir>/chrome-headless-shell-<platform>/<bin>, matching
	// the layout both @puppeteer/browsers and Biloba's own cache use, and returns its path.
	writeFakeShell := func(root, versionDir, platform string) string {
		GinkgoHelper()
		dir := filepath.Join(root, "chrome-headless-shell", versionDir, "chrome-headless-shell-"+platform)
		Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
		path := filepath.Join(dir, engine.ChromeBinaryName())
		Expect(os.WriteFile(path, []byte("fake chrome-headless-shell"), 0o755)).To(Succeed())
		return path
	}

	BeforeEach(func() {
		GinkgoT().Setenv(engine.ChromeEnvVar, "")
		GinkgoT().Setenv("PATH", GinkgoT().TempDir()) // hide any real chrome-headless-shell on PATH
		puppeteerRoot = GinkgoT().TempDir()
		biloba1Root = GinkgoT().TempDir()
		biloba2Root = GinkgoT().TempDir()
		// Mirror the real precedence order (puppeteer's default root, then puppeteer's per-OS
		// cache dir, then Biloba's own) so the "earlier root wins a tie" fallback is exercised
		// the same way it would be for real.
		DeferCleanup(engine.SetChromeCacheRootsForTest([]string{puppeteerRoot, biloba1Root, biloba2Root}))
	})

	It("returns \"\" when no cache root has a chrome-headless-shell", func() {
		Expect(engine.LocateChrome("")).To(BeEmpty())
	})

	It("picks the newest version across cache roots, not just the first root with any entry", func() {
		writeFakeShell(puppeteerRoot, "150.0.7871.24", "mac_arm")
		newest := writeFakeShell(biloba2Root, "153.0.8010.36", "mac-arm64")

		Expect(engine.LocateChrome("")).To(Equal(newest))
	})

	It("compares versions numerically rather than lexically, so 150.x beats 99.x", func() {
		// Lexically "99.0.4844.51" sorts after "150.0.7871.24" (a '9' beats a '1'), which is
		// exactly the bug this test guards against.
		writeFakeShell(puppeteerRoot, "99.0.4844.51", "mac_arm")
		newest := writeFakeShell(biloba2Root, "150.0.7871.24", "mac-arm64")

		Expect(engine.LocateChrome("")).To(Equal(newest))
	})

	It("ignores the puppeteer platform prefix when comparing versions", func() {
		newest := writeFakeShell(puppeteerRoot, "linux-153.0.8010.36", "linux64")
		writeFakeShell(biloba2Root, "150.0.7871.24", "linux-x64")

		Expect(engine.LocateChrome("")).To(Equal(newest))
	})

	It("keeps the earlier cache root's entry when versions tie", func() {
		first := writeFakeShell(puppeteerRoot, "150.0.7871.24", "mac_arm")
		writeFakeShell(biloba2Root, "150.0.7871.24", "mac-arm64")

		Expect(engine.LocateChrome("")).To(Equal(first))
	})

	It("still prefers an explicit path over a newer cache entry", func() {
		writeFakeShell(biloba2Root, "999.0.0.0", "mac-arm64")

		explicitDir := GinkgoT().TempDir()
		explicitPath := filepath.Join(explicitDir, engine.ChromeBinaryName())
		Expect(os.WriteFile(explicitPath, []byte("fake"), 0o755)).To(Succeed())

		Expect(engine.LocateChrome(explicitPath)).To(Equal(explicitPath))
	})

	It("still prefers the env var over a newer cache entry", func() {
		writeFakeShell(biloba2Root, "999.0.0.0", "mac-arm64")

		envDir := GinkgoT().TempDir()
		envPath := filepath.Join(envDir, engine.ChromeBinaryName())
		Expect(os.WriteFile(envPath, []byte("fake"), 0o755)).To(Succeed())
		GinkgoT().Setenv(engine.ChromeEnvVar, envPath)

		Expect(engine.LocateChrome("")).To(Equal(envPath))
	})

	It("still prefers a chrome-headless-shell on PATH over a newer cache entry", func() {
		writeFakeShell(biloba2Root, "999.0.0.0", "mac-arm64")

		pathDir := GinkgoT().TempDir()
		pathBinary := filepath.Join(pathDir, "chrome-headless-shell")
		Expect(os.WriteFile(pathBinary, []byte("fake"), 0o755)).To(Succeed())
		GinkgoT().Setenv("PATH", pathDir)

		Expect(engine.LocateChrome("")).To(Equal(pathBinary))
	})
})

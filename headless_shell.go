package biloba

import (
	"context"
	"fmt"

	"github.com/onsi/biloba/engine"
)

// BILOBA_CHROME_HEADLESS_SHELL lets you point Biloba at a chrome-headless-shell binary
// without code changes.  The search itself is runner-neutral and lives in the engine, so the
// bilobad daemon and this suite resolve the same binary.
const headlessShellEnvVar = engine.ChromeEnvVar

var resolveHeadlessShell = engine.ResolveHeadlessShell

// resolveHeadlessShellPath finds a chrome-headless-shell binary for the default (pragmatic)
// headless mode.  It searches local locations first and, only if cfg.autoInstall is set,
// downloads one via Chrome for Testing.  Otherwise it returns an actionable error.
func resolveHeadlessShellPath(ginkgoT GinkgoTInterface, cfg *spinUpConfig) (string, error) {
	if cfg.autoInstall && locateHeadlessShell(cfg.headlessShellPath) == "" {
		ginkgoT.Printf("Biloba: chrome-headless-shell not found locally; downloading it via Chrome for Testing (AutoInstallHeadlessShell)...\n")
	}
	path, _, err := resolveHeadlessShell(context.Background(), cfg.headlessShellPath, cfg.autoInstall)
	if err == nil {
		return path, nil
	}
	if cfg.autoInstall {
		return "", fmt.Errorf("Biloba could not auto-install chrome-headless-shell:\n%s\n\n%s", err.Error(), headlessShellInstructions())
	}
	return "", fmt.Errorf("%s", headlessShellInstructions())
}

// locateHeadlessShell returns the path to a chrome-headless-shell binary, searching (in order):
// an explicit path, the BILOBA_CHROME_HEADLESS_SHELL env var, $PATH, and the puppeteer / Biloba
// download caches.  It returns "" if none is found.
func locateHeadlessShell(explicit string) string { return engine.LocateChrome(explicit) }

func headlessShellBinaryName() string { return engine.ChromeBinaryName() }

func headlessShellInstructions() string {
	return fmt.Sprintf(`Biloba defaults to the lightweight chrome-headless-shell for speed, but could not find it.

Install it with:
    npx @puppeteer/browsers install chrome-headless-shell@stable

then add it to your PATH, set %s=/path/to/chrome-headless-shell, or pass biloba.HeadlessShellPath("...") to SpinUpChrome.

Alternatively:
  - pass biloba.AutoInstallHeadlessShell() to SpinUpChrome and Biloba will download it for you, or
  - pass biloba.HighFidelityHeadless() to SpinUpChrome to use the full (slower, higher-fidelity) headless Chrome instead.`, headlessShellEnvVar)
}

// installHeadlessShell downloads the Stable chrome-headless-shell for the current platform from
// Chrome for Testing into Biloba's cache and returns the path to the binary.  It is a no-op if a
// matching binary is already cached.
func installHeadlessShell() (string, error) {
	return engine.InstallHeadlessShell(context.Background())
}

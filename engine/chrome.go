package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
)

// ChromeEnvVar lets you point Biloba at a chrome-headless-shell binary without code changes.
// It is honored by the Ginkgo adapter, by the engine's own suite, and by the bilobad daemon.
const ChromeEnvVar = "BILOBA_CHROME_HEADLESS_SHELL"

var headlessShellInstaller = InstallHeadlessShell

// ResolveHeadlessShell finds a local chrome-headless-shell and, only when autoInstall is true,
// installs Chrome for Testing's stable shell into Biloba's cache as a fallback.
func ResolveHeadlessShell(ctx context.Context, explicit string, autoInstall bool) (string, bool, error) {
	if path := LocateChrome(explicit); path != "" {
		return path, false, nil
	}
	if !autoInstall {
		return "", false, fmt.Errorf("could not find chrome-headless-shell; install it, set %s, provide an explicit path, or opt in to auto-install", ChromeEnvVar)
	}
	path, err := headlessShellInstaller(ctx)
	if err != nil {
		return "", false, fmt.Errorf("auto-install chrome-headless-shell: %w", err)
	}
	return path, true, nil
}

// LocateChrome returns the path to a chrome-headless-shell binary, searching (in order): an
// explicit path, ChromeEnvVar, $PATH, and the puppeteer / Biloba download caches.  It returns ""
// if none is found.
//
// Finding a browser binary is runner-neutral, so every entry point resolves Chrome through this
// one search: a daemon started by a TypeScript worker and a Ginkgo suite on the same machine pick
// the same binary.
func LocateChrome(explicit string) string {
	for _, candidate := range []string{explicit, os.Getenv(ChromeEnvVar)} {
		if candidate != "" && IsExecutableFile(candidate) {
			return candidate
		}
	}
	if path, err := exec.LookPath("chrome-headless-shell"); err == nil {
		return path
	}
	binary := ChromeBinaryName()
	for _, cacheRoot := range chromeCacheRoots() {
		// download caches lay binaries out as <root>/chrome-headless-shell/<version>/chrome-headless-shell-<platform>/<bin>
		matches, _ := filepath.Glob(filepath.Join(cacheRoot, "chrome-headless-shell", "*", "chrome-headless-shell-*", binary))
		if len(matches) > 0 {
			sort.Strings(matches) // prefer the lexically-last (typically newest) version
			return matches[len(matches)-1]
		}
	}
	return ""
}

// LocateFullChrome returns a full Chrome/Chromium executable for high-fidelity or headful mode.
func LocateFullChrome(explicit string) string {
	if explicit != "" && IsExecutableFile(explicit) {
		return explicit
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		}
	case "windows":
		candidates = []string{
			"chrome", "chrome.exe",
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			filepath.Join(os.Getenv("USERPROFILE"), `AppData\Local\Google\Chrome\Application\chrome.exe`),
			filepath.Join(os.Getenv("USERPROFILE"), `AppData\Local\Chromium\Application\chrome.exe`),
		}
	default:
		candidates = []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "google-chrome-beta", "google-chrome-unstable", "/usr/bin/google-chrome", "/usr/local/bin/chrome", "/snap/bin/chromium", "chrome"}
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil && IsExecutableFile(path) {
			return path
		}
	}
	return ""
}

// ChromeBinaryName is the platform-specific chrome-headless-shell executable name.
func ChromeBinaryName() string {
	if runtime.GOOS == "windows" {
		return "chrome-headless-shell.exe"
	}
	return "chrome-headless-shell"
}

// IsExecutableFile reports whether path names an existing file (as opposed to a directory).
func IsExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func chromeCacheRoots() []string {
	roots := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".cache", "puppeteer")) // @puppeteer/browsers default
	}
	if cache, err := os.UserCacheDir(); err == nil {
		roots = append(roots, filepath.Join(cache, "puppeteer"), filepath.Join(cache, "biloba"))
	}
	return roots
}

package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
)

// ChromeEnvVar lets you point Biloba at a chrome-headless-shell binary without code changes.
// It is honored by the Ginkgo adapter, by the engine's own suite, and by the bilobad daemon.
const ChromeEnvVar = "BILOBA_CHROME_HEADLESS_SHELL"

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

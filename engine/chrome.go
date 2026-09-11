package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// ChromeEnvVar lets you point Biloba at a chrome-headless-shell binary without code changes.
// It is honored by the Ginkgo adapter, by the engine's own suite, and by the bilobad daemon.
const ChromeEnvVar = "BILOBA_CHROME_HEADLESS_SHELL"

// ErrChromeNotFound is the sentinel behind ResolveHeadlessShell's "could not find
// chrome-headless-shell" failure, wrapped so callers across the runner-neutral boundary (the
// bilobad daemon in particular, which has its own npm-flavored remedy to add) can detect it with
// errors.Is instead of matching on message text.
var ErrChromeNotFound = errors.New("could not find chrome-headless-shell")

var headlessShellInstaller = InstallHeadlessShell
var locateChromeForResolve = LocateChrome

// ResolveHeadlessShell finds a local chrome-headless-shell and, only when autoInstall is true,
// installs Chrome for Testing's stable shell into Biloba's cache as a fallback.
func ResolveHeadlessShell(ctx context.Context, explicit string, autoInstall bool) (string, bool, error) {
	if path := locateChromeForResolve(explicit); path != "" {
		return path, false, nil
	}
	if !autoInstall {
		return "", false, fmt.Errorf("%w; install it, set %s, provide an explicit path, or opt in to auto-install", ErrChromeNotFound, ChromeEnvVar)
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
// The download caches are searched together rather than one-root-wins: every chrome-headless-shell
// found under any cache root (see chromeCacheRoots) is a candidate, and the newest one - compared
// numerically on its dotted version, not lexically - is returned. That way a stale build left over
// in the puppeteer cache never shadows a newer one `bilobad install-chrome` just placed in Biloba's
// own cache (or vice versa).
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
	return newestCachedChromeHeadlessShell(ChromeBinaryName())
}

// chromeCacheVersionPattern matches the trailing dotted version number (e.g. "153.0.8010.36") off
// a cache version directory name. Biloba's own cache names the directory with the bare version;
// the puppeteer cache prefixes it with a platform tag ("mac_arm-150.0.7871.24", "linux-…",
// "win64-…"). Matching the trailing digits handles both without needing to enumerate every
// platform tag.
var chromeCacheVersionPattern = regexp.MustCompile(`(\d+(?:\.\d+){1,})$`)

// parseChromeCacheVersion returns dirName's dotted version as ordered numeric components, or nil
// if dirName doesn't end in one.
func parseChromeCacheVersion(dirName string) []int {
	match := chromeCacheVersionPattern.FindStringSubmatch(dirName)
	if match == nil {
		return nil
	}
	segments := strings.Split(match[1], ".")
	numbers := make([]int, len(segments))
	for i, segment := range segments {
		n, err := strconv.Atoi(segment)
		if err != nil {
			return nil
		}
		numbers[i] = n
	}
	return numbers
}

// compareChromeCacheVersions compares two version-component slices numerically, treating a
// missing trailing component as 0. It returns a positive number if a > b, negative if a < b, 0 if
// equal.
func compareChromeCacheVersions(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

// chromeCacheCandidate is one chrome-headless-shell binary found under a cache root.
type chromeCacheCandidate struct {
	path       string
	version    []int // nil if versionDir couldn't be parsed
	versionDir string
	rootIndex  int
}

// newer reports whether candidate c should be preferred over other. Parsed versions are compared
// numerically; a parsed version always beats an unparsed one. Ties (equal version, or both
// unparsed) fall back to a deterministic order: the earlier cache root wins - so
// chromeCacheRoots' order still acts as the tiebreak precedence it always has - and, failing
// that, the lexically greater version directory name wins.
func (c chromeCacheCandidate) newer(other chromeCacheCandidate) bool {
	if c.version != nil && other.version != nil {
		if cmp := compareChromeCacheVersions(c.version, other.version); cmp != 0 {
			return cmp > 0
		}
	} else if (c.version != nil) != (other.version != nil) {
		return c.version != nil
	}
	if c.rootIndex != other.rootIndex {
		return c.rootIndex < other.rootIndex
	}
	return c.versionDir > other.versionDir
}

// newestCachedChromeHeadlessShell searches every chromeCacheRoots entry for binary and returns
// the path to the newest one found (see LocateChrome), or "" if none exist.
func newestCachedChromeHeadlessShell(binary string) string {
	var best *chromeCacheCandidate
	for rootIndex, cacheRoot := range chromeCacheRoots() {
		// download caches lay binaries out as <root>/chrome-headless-shell/<version>/chrome-headless-shell-<platform>/<bin>
		matches, _ := filepath.Glob(filepath.Join(cacheRoot, "chrome-headless-shell", "*", "chrome-headless-shell-*", binary))
		for _, match := range matches {
			versionDir := filepath.Base(filepath.Dir(filepath.Dir(match)))
			candidate := chromeCacheCandidate{
				path:       match,
				version:    parseChromeCacheVersion(versionDir),
				versionDir: versionDir,
				rootIndex:  rootIndex,
			}
			if best == nil || candidate.newer(*best) {
				best = &candidate
			}
		}
	}
	if best == nil {
		return ""
	}
	return best.path
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

// chromeCacheRoots resolves the candidate roots LocateChrome searches for cached
// chrome-headless-shell binaries. It's a var, like headlessShellInstaller and
// locateChromeForResolve above, so tests can substitute a hermetic set of roots instead of
// depending on whatever happens to be in the host's real puppeteer/Biloba caches (see
// SetChromeCacheRootsForTest in export_test.go).
var chromeCacheRoots = func() []string {
	roots := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".cache", "puppeteer")) // @puppeteer/browsers default
	}
	if cache, err := os.UserCacheDir(); err == nil {
		roots = append(roots, filepath.Join(cache, "puppeteer"), filepath.Join(cache, "biloba"))
	}
	return roots
}

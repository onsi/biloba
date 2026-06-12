package biloba

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// BILOBA_CHROME_HEADLESS_SHELL lets you point Biloba at a chrome-headless-shell binary
// without code changes.
const headlessShellEnvVar = "BILOBA_CHROME_HEADLESS_SHELL"

const chromeForTestingVersionsURL = "https://googlechromelabs.github.io/chrome-for-testing/last-known-good-versions-with-downloads.json"

// resolveHeadlessShellPath finds a chrome-headless-shell binary for the default (pragmatic)
// headless mode.  It searches local locations first and, only if cfg.autoInstall is set,
// downloads one via Chrome for Testing.  Otherwise it returns an actionable error.
func resolveHeadlessShellPath(ginkgoT GinkgoTInterface, cfg *spinUpConfig) (string, error) {
	if p := locateHeadlessShell(cfg.headlessShellPath); p != "" {
		return p, nil
	}
	if cfg.autoInstall {
		ginkgoT.Printf("Biloba: chrome-headless-shell not found locally; downloading it via Chrome for Testing (AutoInstallHeadlessShell)...\n")
		p, err := installHeadlessShell()
		if err != nil {
			return "", fmt.Errorf("Biloba could not auto-install chrome-headless-shell:\n%s\n\n%s", err.Error(), headlessShellInstructions())
		}
		return p, nil
	}
	return "", fmt.Errorf("%s", headlessShellInstructions())
}

// locateHeadlessShell returns the path to a chrome-headless-shell binary, searching (in order):
// an explicit path, the BILOBA_CHROME_HEADLESS_SHELL env var, $PATH, and the puppeteer / Biloba
// download caches.  It returns "" if none is found.
func locateHeadlessShell(explicit string) string {
	for _, c := range []string{explicit, os.Getenv(headlessShellEnvVar)} {
		if c != "" && isExecutableFile(c) {
			return c
		}
	}
	if p, err := exec.LookPath("chrome-headless-shell"); err == nil {
		return p
	}
	bin := headlessShellBinaryName()
	for _, cacheRoot := range headlessShellCacheRoots() {
		// download caches lay binaries out as <root>/chrome-headless-shell/<version>/chrome-headless-shell-<platform>/<bin>
		matches, _ := filepath.Glob(filepath.Join(cacheRoot, "chrome-headless-shell", "*", "chrome-headless-shell-*", bin))
		if len(matches) > 0 {
			sort.Strings(matches) // prefer the lexically-last (typically newest) version
			return matches[len(matches)-1]
		}
	}
	return ""
}

func headlessShellBinaryName() string {
	if runtime.GOOS == "windows" {
		return "chrome-headless-shell.exe"
	}
	return "chrome-headless-shell"
}

func headlessShellCacheRoots() []string {
	roots := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".cache", "puppeteer")) // @puppeteer/browsers default
	}
	if cache, err := os.UserCacheDir(); err == nil {
		roots = append(roots, filepath.Join(cache, "puppeteer"), filepath.Join(cache, "biloba"))
	}
	return roots
}

func isExecutableFile(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

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
	platform, err := chromeForTestingPlatform()
	if err != nil {
		return "", err
	}
	url, version, err := stableHeadlessShellDownload(platform)
	if err != nil {
		return "", err
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("could not determine user cache dir: %w", err)
	}
	destDir := filepath.Join(cacheRoot, "biloba", "chrome-headless-shell", version)
	binPath := filepath.Join(destDir, "chrome-headless-shell-"+platform, headlessShellBinaryName())
	if isExecutableFile(binPath) {
		return binPath, nil // already installed
	}
	if err := downloadAndUnzip(url, destDir); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		os.Chmod(binPath, 0o755)
	}
	if !isExecutableFile(binPath) {
		return "", fmt.Errorf("downloaded chrome-headless-shell but the binary was not found at %s", binPath)
	}
	return binPath, nil
}

func chromeForTestingPlatform() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "mac-arm64", nil
	case "darwin/amd64":
		return "mac-x64", nil
	case "linux/amd64":
		return "linux64", nil
	case "windows/amd64":
		return "win64", nil
	case "windows/386":
		return "win32", nil
	default:
		return "", fmt.Errorf("chrome-headless-shell auto-install is not available for %s/%s via Chrome for Testing - install it manually", runtime.GOOS, runtime.GOARCH)
	}
}

func stableHeadlessShellDownload(platform string) (url string, version string, err error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(chromeForTestingVersionsURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to query Chrome for Testing: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Chrome for Testing returned status %d", resp.StatusCode)
	}
	var data struct {
		Channels map[string]struct {
			Version   string `json:"version"`
			Downloads struct {
				HeadlessShell []struct {
					Platform string `json:"platform"`
					URL      string `json:"url"`
				} `json:"chrome-headless-shell"`
			} `json:"downloads"`
		} `json:"channels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", fmt.Errorf("failed to decode Chrome for Testing response: %w", err)
	}
	stable, ok := data.Channels["Stable"]
	if !ok {
		return "", "", fmt.Errorf("Chrome for Testing response did not include a Stable channel")
	}
	for _, dl := range stable.Downloads.HeadlessShell {
		if dl.Platform == platform {
			return dl.URL, stable.Version, nil
		}
	}
	return "", "", fmt.Errorf("Chrome for Testing has no chrome-headless-shell download for platform %s", platform)
}

func downloadAndUnzip(url, destDir string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download of %s returned status %d", url, resp.StatusCode)
	}
	tmpFile, err := os.CreateTemp("", "chrome-headless-shell-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()
	return unzip(tmpFile.Name(), destDir)
}

func unzip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		fpath := filepath.Join(destDir, f.Name)
		// zip-slip guard
		if !strings.HasPrefix(fpath, filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in zip archive: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(fpath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

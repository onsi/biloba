package engine

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

const chromeForTestingVersionsURL = "https://googlechromelabs.github.io/chrome-for-testing/last-known-good-versions-with-downloads.json"

const (
	maxChromeManifestBytes = 8 << 20
	maxShellArchiveBytes   = 512 << 20
	maxShellExtractedBytes = 1 << 30
	maxShellArchiveEntries = 10_000
)

// InstallHeadlessShell downloads Chrome for Testing's stable chrome-headless-shell into Biloba's
// user cache. Callers must opt in; normal browser resolution never calls this function.
func InstallHeadlessShell(ctx context.Context) (string, error) {
	platform, err := chromeForTestingPlatform()
	if err != nil {
		return "", err
	}
	downloadURL, version, err := stableHeadlessShellDownload(ctx, platform)
	if err != nil {
		return "", err
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("determine user cache directory: %w", err)
	}
	destination := filepath.Join(cacheRoot, "biloba", "chrome-headless-shell", version)
	binaryPath := filepath.Join(destination, "chrome-headless-shell-"+platform, ChromeBinaryName())
	if IsExecutableFile(binaryPath) {
		return binaryPath, nil
	}
	archivePath, cleanup, err := downloadHeadlessShellArchive(ctx, downloadURL)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if err := installHeadlessShellArchive(archivePath, destination, platform); err != nil {
		return "", err
	}
	return binaryPath, nil
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
		return "", fmt.Errorf("chrome-headless-shell auto-install is unavailable for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func stableHeadlessShellDownload(ctx context.Context, platform string) (string, string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, chromeForTestingVersionsURL, nil)
	if err != nil {
		return "", "", err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return "", "", fmt.Errorf("query Chrome for Testing: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("Chrome for Testing returned status %d", response.StatusCode)
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
	if response.ContentLength > maxChromeManifestBytes {
		return "", "", fmt.Errorf("Chrome for Testing manifest exceeds %d bytes", maxChromeManifestBytes)
	}
	manifest, err := io.ReadAll(io.LimitReader(response.Body, maxChromeManifestBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read Chrome for Testing response: %w", err)
	}
	if len(manifest) > maxChromeManifestBytes {
		return "", "", fmt.Errorf("Chrome for Testing manifest exceeds %d bytes", maxChromeManifestBytes)
	}
	if err := json.Unmarshal(manifest, &data); err != nil {
		return "", "", fmt.Errorf("decode Chrome for Testing response: %w", err)
	}
	stable, ok := data.Channels["Stable"]
	if !ok {
		return "", "", fmt.Errorf("Chrome for Testing response has no Stable channel")
	}
	for _, download := range stable.Downloads.HeadlessShell {
		if download.Platform == platform {
			return download.URL, stable.Version, nil
		}
	}
	return "", "", fmt.Errorf("Chrome for Testing has no chrome-headless-shell download for %s", platform)
}

func downloadHeadlessShellArchive(ctx context.Context, downloadURL string) (string, func(), error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", func() {}, err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	response, err := client.Do(request)
	if err != nil {
		return "", func() {}, fmt.Errorf("download %s: %w", downloadURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", func() {}, fmt.Errorf("download %s returned status %d", downloadURL, response.StatusCode)
	}
	if response.ContentLength > maxShellArchiveBytes {
		return "", func() {}, fmt.Errorf("chrome-headless-shell archive exceeds %d bytes", maxShellArchiveBytes)
	}
	temporary, err := os.CreateTemp("", "chrome-headless-shell-*.zip")
	if err != nil {
		return "", func() {}, err
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	written, copyErr := io.Copy(temporary, io.LimitReader(response.Body, maxShellArchiveBytes+1))
	if copyErr != nil {
		temporary.Close()
		cleanup()
		return "", func() {}, copyErr
	}
	if written > maxShellArchiveBytes {
		temporary.Close()
		cleanup()
		return "", func() {}, fmt.Errorf("chrome-headless-shell archive exceeds %d bytes", maxShellArchiveBytes)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return temporaryPath, cleanup, nil
}

func installHeadlessShellArchive(archivePath, destination, platform string) error {
	finalBinary := filepath.Join(destination, "chrome-headless-shell-"+platform, ChromeBinaryName())
	if IsExecutableFile(finalBinary) {
		return nil
	}
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, filepath.Base(destination)+".partial-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := unzipHeadlessShell(archivePath, staging); err != nil {
		return err
	}
	stagedBinary := filepath.Join(staging, "chrome-headless-shell-"+platform, ChromeBinaryName())
	if runtime.GOOS != "windows" {
		if err := os.Chmod(stagedBinary, 0o755); err != nil {
			return fmt.Errorf("make chrome-headless-shell executable: %w", err)
		}
	}
	if !IsExecutableFile(stagedBinary) {
		return fmt.Errorf("downloaded chrome-headless-shell but no binary was found at %s", stagedBinary)
	}
	if err := os.Rename(staging, destination); err != nil {
		if IsExecutableFile(finalBinary) {
			return nil
		}
		return fmt.Errorf("publish chrome-headless-shell cache entry: %w", err)
	}
	return nil
}

func unzipHeadlessShell(archivePath, destination string) error {
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	if len(archive.File) > maxShellArchiveEntries {
		return fmt.Errorf("chrome-headless-shell archive has more than %d entries", maxShellArchiveEntries)
	}
	var extracted uint64
	for _, entry := range archive.File {
		if entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("chrome-headless-shell archive contains a symbolic link: %s", entry.Name)
		}
		if entry.UncompressedSize64 > maxShellExtractedBytes-extracted {
			return fmt.Errorf("chrome-headless-shell archive expands beyond %d bytes", maxShellExtractedBytes)
		}
		extracted += entry.UncompressedSize64
		path := filepath.Join(destination, entry.Name)
		relative, relativeErr := filepath.Rel(destination, path)
		if relativeErr != nil || relative == ".." || filepath.IsAbs(relative) || (len(relative) >= 3 && relative[:3] == ".."+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in zip archive: %s", entry.Name)
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		writer, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, entry.Mode())
		if err != nil {
			reader.Close()
			return err
		}
		written, copyErr := io.Copy(writer, io.LimitReader(reader, int64(entry.UncompressedSize64)+1))
		closeWriterErr := writer.Close()
		closeReaderErr := reader.Close()
		if copyErr != nil {
			return copyErr
		}
		if uint64(written) != entry.UncompressedSize64 {
			return fmt.Errorf("archive entry %s size mismatch", entry.Name)
		}
		if closeWriterErr != nil {
			return closeWriterErr
		}
		if closeReaderErr != nil {
			return closeReaderErr
		}
	}
	return nil
}

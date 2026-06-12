package biloba

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
	"golang.org/x/net/context"
)

// inlineImagesSupported reports whether the current terminal can render iTerm2
// inline image sequences.  The decision order is:
//
//  1. BILOBA_NO_IMGCAT=true → force off (returns false).
//  2. BILOBA_IMGCAT=true    → force on  (returns true).
//  3. TERM_PROGRAM=iTerm.app → on (iTerm2 detected).
//  4. Otherwise             → off.
func inlineImagesSupported() bool {
	if os.Getenv("BILOBA_NO_IMGCAT") == "true" {
		return false
	}
	if os.Getenv("BILOBA_IMGCAT") == "true" {
		return true
	}
	return os.Getenv("TERM_PROGRAM") == "iTerm.app"
}

/*
CaptureScreenshot() returns a full screenshot of the current tab as a []byte array (you can decode it with the image package)
*/
func (b *Biloba) CaptureScreenshot() []byte {
	var img []byte
	err := chromedp.Run(b.Context, chromedp.FullScreenshot(&img, 100))
	if err != nil {
		b.gt.Fatalf("Failed to capture screenshot:\n%s", err.Error())
	}
	return img
}

/*
CaptureImgCatScreenshot() returns a full screenshot of the current tab as an iTerm2 imgcat-compatible string.  Simply print it out to see images on your terminal.
*/
func (b *Biloba) CaptureImgcatScreenshot() string {
	return b.asImgCat(b.CaptureScreenshot())
}

/*
CaptureScreenshotToFile writes a full screenshot of the current tab as a PNG file to the given path and returns its absolute path.
The directory is created if it does not already exist.
The absolute path is printed to the test output so it appears in failure output and is readable by tools that can render PNG files.

Read https://onsi.github.io/biloba/#capturing-screenshots for details.
*/
func (b *Biloba) CaptureScreenshotToFile(path string) string {
	b.gt.Helper()
	img := b.CaptureScreenshot()
	absPath, err := filepath.Abs(path)
	if err != nil {
		b.gt.Fatalf("Failed to resolve screenshot path %q:\n%s", path, err.Error())
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		b.gt.Fatalf("Failed to create screenshot directory %q:\n%s", filepath.Dir(absPath), err.Error())
		return ""
	}
	if err := os.WriteFile(absPath, img, 0644); err != nil {
		b.gt.Fatalf("Failed to write screenshot to %q:\n%s", absPath, err.Error())
		return ""
	}
	b.gt.Printf("Screenshot written to: %s\n", absPath)
	return absPath
}

func (b *Biloba) asImgCat(img []byte) string {
	buf := &bytes.Buffer{}
	buf.WriteString("\033]1337;File=;inline=1:")
	encoder := base64.NewEncoder(base64.StdEncoding, buf)
	_, err := encoder.Write(img)
	if err != nil {
		b.gt.Fatalf("Failed to capture screenshot:\n%s", err.Error())
	}
	encoder.Close()
	buf.WriteString("\033\\")

	return string(buf.Bytes())
}

type tabScreenshot struct {
	title            string
	imgcatScreenshot string
	filePath         string
	failure          string
}

// sanitizeForFilename replaces any characters that are not alphanumeric, hyphens, underscores, or dots with underscores,
// and collapses runs of underscores.
var nonFilenameRE = regexp.MustCompile(`[^a-zA-Z0-9\-_.]`)
var multiUnderscoreRE = regexp.MustCompile(`_+`)

func sanitizeForFilename(s string) string {
	s = nonFilenameRE.ReplaceAllString(s, "_")
	s = multiUnderscoreRE.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func (b *Biloba) safeAllTabScreenshots(width int, height int) []tabScreenshot {
	out := []tabScreenshot{}
	for idx, tab := range b.AllTabs() {
		ctx, cancel := context.WithTimeout(tab.Context, time.Second)
		defer cancel()

		var originalWidth, originalHeight int
		if width > 0 && height > 0 {
			originalWidth, originalHeight = b.WindowSize()
			err := chromedp.Run(ctx, chromedp.EmulateViewport(int64(width), int64(height)))
			if err != nil {
				out = append(out, tabScreenshot{failure: fmt.Sprintf("failed to set window size: %s", err.Error())})
				continue
			}
		}
		var img []byte
		var title string
		err := chromedp.Run(ctx,
			chromedp.Title(&title),
			chromedp.FullScreenshot(&img, 100),
		)
		if width > 0 && height > 0 {
			err := chromedp.Run(ctx, chromedp.EmulateViewport(int64(originalWidth), int64(originalHeight), chromedp.EmulatePortrait))
			if err != nil {
				out = append(out, tabScreenshot{failure: fmt.Sprintf("failed to reset window size: %s", err.Error())})
				continue
			}
		}
		if ctx.Err() != nil {
			out = append(out, tabScreenshot{failure: "Timed out attempting to fetch screenshot for tab"})
			continue
		} else if err != nil {
			out = append(out, tabScreenshot{failure: fmt.Sprintf("Failed to fetch screenshot for tab: %s", err.Error())})
			continue
		}
		ts := tabScreenshot{
			title: title,
		}
		if b.root.inlineScreenshotsEnabled() {
			ts.imgcatScreenshot = b.asImgCat(img)
		}
		if b.root.screenshotsDir != "" {
			specName := sanitizeForFilename(b.gt.Name())
			tabLabel := sanitizeForFilename(title)
			if tabLabel == "" {
				tabLabel = fmt.Sprintf("tab%d", idx)
			}
			filename := fmt.Sprintf("screenshot-%s-%s.png", specName, tabLabel)
			absPath := filepath.Join(b.root.screenshotsDir, filename)
			if mkErr := os.MkdirAll(b.root.screenshotsDir, 0755); mkErr == nil {
				if writeErr := os.WriteFile(absPath, img, 0644); writeErr == nil {
					ts.filePath = absPath
				}
			}
		}
		out = append(out, ts)
	}
	return out
}

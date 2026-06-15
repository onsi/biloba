package biloba

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color/palette"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BourgeoisBear/rasterm"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// inlineImageProtocol identifies which terminal inline-image escape sequence a
// screenshot should be encoded with.
type inlineImageProtocol int

const (
	inlineImageNone inlineImageProtocol = iota
	inlineImageITerm
	inlineImageKitty
	inlineImageSixel
)

// detectInlineImageProtocol decides which (if any) terminal inline-image protocol
// to use.  The decision order is:
//
//  1. BILOBA_INLINE_SCREENSHOTS=iterm|kitty|sixel → force that protocol; =none → force off.
//  2. Environment-variable terminal detection (iTerm2, VSCode, WezTerm, Ghostty, kitty, Konsole, …).
//  3. BILOBA_PROBE_TERMINAL=true → query the terminal directly (Primary DA) for Sixel support.
//  4. Otherwise → off.
//
// Kitty's graphics protocol is preferred where available (best quality), then the
// broadly-supported iTerm2 OSC 1337 protocol (works in iTerm2, VSCode, WezTerm, …),
// then Sixel as a last-resort fallback for older terminals.
func detectInlineImageProtocol() inlineImageProtocol {
	switch strings.ToLower(os.Getenv("BILOBA_INLINE_SCREENSHOTS")) {
	case "iterm", "iterm2":
		return inlineImageITerm
	case "kitty":
		return inlineImageKitty
	case "sixel":
		return inlineImageSixel
	case "none", "off", "false":
		return inlineImageNone
	}
	// unset, "auto", or an unrecognized value falls through to terminal auto-detection.

	if p := inlineImageProtocolFromEnv(); p != inlineImageNone {
		return p
	}

	// Some Sixel-capable terminals (xterm, foot, mlterm, …) don't announce themselves
	// through environment variables.  Probing requires putting the controlling TTY into
	// raw mode, so it is opt-in to avoid interfering with the test runner's terminal.
	if os.Getenv("BILOBA_PROBE_TERMINAL") == "true" {
		if ok, err := rasterm.IsSixelCapable(); err == nil && ok {
			return inlineImageSixel
		}
	}

	return inlineImageNone
}

// inlineImageProtocolFromEnv maps well-known terminal environment variables to the
// best inline-image protocol that terminal supports.
func inlineImageProtocolFromEnv() inlineImageProtocol {
	termProgram := os.Getenv("TERM_PROGRAM")
	term := os.Getenv("TERM")

	// Kitty graphics protocol — best quality where supported.
	if os.Getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty" || termProgram == "ghostty" {
		return inlineImageKitty
	}

	// VSCode's integrated terminal renders Sixel but NOT the iTerm2 OSC 1337
	// protocol, so prefer Sixel there.
	if termProgram == "vscode" {
		return inlineImageSixel
	}

	// iTerm2 OSC 1337 inline-image protocol — broad reach (iTerm2, WezTerm, …).
	switch termProgram {
	case "iTerm.app", "WezTerm", "rio":
		return inlineImageITerm
	}
	if os.Getenv("LC_TERMINAL") == "iTerm2" { // iTerm2 forwarded over ssh
		return inlineImageITerm
	}
	if os.Getenv("KONSOLE_VERSION") != "" { // Konsole speaks OSC 1337
		return inlineImageITerm
	}
	if term == "mintty" {
		return inlineImageITerm
	}

	return inlineImageNone
}

// inlineImagesSupported reports whether the current terminal can render any inline
// image protocol.  See detectInlineImageProtocol for the decision order.
func inlineImagesSupported() bool {
	return detectInlineImageProtocol() != inlineImageNone
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
	return b.writeScreenshotToFile(b.CaptureScreenshot(), path)
}

// writeScreenshotToFile resolves path to an absolute path, creates any missing intermediate
// directories, writes img there as a PNG, prints the path to the test output (so it surfaces in
// failure output and is readable by tools that render PNGs), and returns the absolute path.  It
// fails the spec on any error.
func (b *Biloba) writeScreenshotToFile(img []byte, path string) string {
	b.gt.Helper()
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

/*
CaptureScreenshotOf(selector) returns a screenshot of the first element matching selector as a []byte array (you can decode it with the image package).  The screenshot is clipped to the element's bounding box and can capture an element below the fold without scrolling.  Same-origin >>>-pierced iframe elements are translated to top-level page coordinates.

Read https://onsi.github.io/biloba/#capturing-screenshots for details.
*/
func (b *Biloba) CaptureScreenshotOf(selector any) []byte {
	b.gt.Helper()
	r := b.runBilobaHandler("boundingBox", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to capture screenshot of element:\n%s", r.Error())
		return nil
	}
	box, ok := r.Result.(map[string]any)
	if !ok {
		b.gt.Fatalf("Failed to capture screenshot of element:\nunexpected bounding box result: %v", r.Result)
		return nil
	}
	clip := &page.Viewport{
		X:      toFloat64(box["x"]),
		Y:      toFloat64(box["y"]),
		Width:  toFloat64(box["width"]),
		Height: toFloat64(box["height"]),
		Scale:  1,
	}
	// TODO: roadmap §7 masking (cover other selectors before capturing) is not yet supported.
	var img []byte
	err := chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
		var captureErr error
		img, captureErr = page.CaptureScreenshot().
			WithClip(clip).
			WithFromSurface(true).
			WithCaptureBeyondViewport(true).
			Do(ctx)
		return captureErr
	}))
	if err != nil {
		b.gt.Fatalf("Failed to capture screenshot of element:\n%s", err.Error())
		return nil
	}
	return img
}

/*
CaptureImgcatScreenshotOf(selector) returns a screenshot of the first element matching selector as an iTerm2 imgcat-compatible string.  Simply print it out to see the image on your terminal.

Read https://onsi.github.io/biloba/#capturing-screenshots for details.
*/
func (b *Biloba) CaptureImgcatScreenshotOf(selector any) string {
	b.gt.Helper()
	return b.asImgCat(b.CaptureScreenshotOf(selector))
}

/*
CaptureScreenshotOfToFile writes a screenshot of the first element matching selector as a PNG file to the given path and returns its absolute path.
The directory is created if it does not already exist.
The absolute path is printed to the test output so it appears in failure output and is readable by tools that can render PNG files.

Read https://onsi.github.io/biloba/#capturing-screenshots for details.
*/
func (b *Biloba) CaptureScreenshotOfToFile(selector any, path string) string {
	b.gt.Helper()
	return b.writeScreenshotToFile(b.CaptureScreenshotOf(selector), path)
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

	return buf.String()
}

// asInlineImage encodes a PNG screenshot into the escape sequence for the given
// terminal inline-image protocol.  Returns "" for inlineImageNone.
func (b *Biloba) asInlineImage(img []byte, proto inlineImageProtocol) string {
	buf := &bytes.Buffer{}
	switch proto {
	case inlineImageITerm:
		return b.asImgCat(img)
	case inlineImageKitty:
		if err := rasterm.KittyCopyPNGInline(buf, bytes.NewReader(img), rasterm.KittyImgOpts{}); err != nil {
			b.gt.Fatalf("Failed to encode kitty screenshot:\n%s", err.Error())
		}
	case inlineImageSixel:
		paletted, err := pngToPaletted(img)
		if err != nil {
			b.gt.Fatalf("Failed to encode sixel screenshot:\n%s", err.Error())
		}
		if err := rasterm.SixelWriteImage(buf, paletted); err != nil {
			b.gt.Fatalf("Failed to encode sixel screenshot:\n%s", err.Error())
		}
	default:
		return ""
	}
	return buf.String()
}

// pngToPaletted decodes a PNG and dithers it down to a 256-color paletted image,
// as required by the Sixel encoder (which is an inherently paletted format).
func pngToPaletted(img []byte) (*image.Paletted, error) {
	src, err := png.Decode(bytes.NewReader(img))
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	out := image.NewPaletted(bounds, palette.Plan9)
	draw.FloydSteinberg.Draw(out, bounds, src, bounds.Min)
	return out, nil
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
			ts.imgcatScreenshot = b.asInlineImage(img, detectInlineImageProtocol())
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

package biloba

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"github.com/onsi/biloba/engine"
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

// screenshotCaptureTimeout bounds a single tab's screenshot capture so a wedged tab can't hang the
// suite, while staying generous enough that a healthy full-page PNG capture doesn't spuriously time
// out under heavy parallel/CI load.
const screenshotCaptureTimeout = 5 * time.Second

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

Like all the screenshot captures it is a waiting command bounded by its own ~5s default deadline; override that with [Biloba.WithTimeout] or abort it with [Biloba.WithContext] (WithPolling and Immediate are not supported).
*/
func (b *Biloba) CaptureScreenshot() []byte {
	b.gt.Helper()
	b.guardConfig("CaptureScreenshot", knobTimeout, knobContext)
	return b.captureScreenshot()
}

// captureScreenshot is the unguarded substrate behind CaptureScreenshot and its imgcat/to-file
// wrappers.  It runs under a bounded context (default screenshotCaptureTimeout) that honors the
// WithTimeout/WithContext knobs a waiting command is allowed.
func (b *Biloba) captureScreenshot() []byte {
	b.gt.Helper()
	ctx, cancel := b.waitingContext(screenshotCaptureTimeout)
	defer cancel()
	var img []byte
	img, err := engine.CapturePageContext(ctx, nil)
	if err != nil {
		b.gt.Fatalf("Failed to capture screenshot:\n%s", err.Error())
	}
	return img
}

// capturePageAction captures the whole document as a PNG, and optionally reports the document's width
// in CSS pixels (which is what the visual-regression path divides by to recover the device scale
// factor).  Both come out of the same round trip, so they describe the same layout.
//
/*
CaptureImgCatScreenshot() returns a full screenshot of the current tab as an iTerm2 imgcat-compatible string.  Simply print it out to see images on your terminal.

It is a waiting command: see [Biloba.CaptureScreenshot] for the WithTimeout/WithContext knobs it honors.
*/
func (b *Biloba) CaptureImgcatScreenshot() string {
	b.gt.Helper()
	b.guardConfig("CaptureImgcatScreenshot", knobTimeout, knobContext)
	return b.asImgCat(b.captureScreenshot())
}

/*
CaptureScreenshotToFile writes a full screenshot of the current tab as a PNG file to the given path and returns its absolute path.
The directory is created if it does not already exist.
The absolute path is printed to the test output so it appears in failure output and is readable by tools that can render PNG files.

It is a waiting command: see [Biloba.CaptureScreenshot] for the WithTimeout/WithContext knobs it honors.

Read https://onsi.github.io/biloba/#capturing-screenshots for details.
*/
func (b *Biloba) CaptureScreenshotToFile(path string) string {
	b.gt.Helper()
	b.guardConfig("CaptureScreenshotToFile", knobTimeout, knobContext)
	return b.writeScreenshotToFile(b.captureScreenshot(), path)
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
CaptureScreenshotOf(selector) returns a screenshot of the first element matching selector as a []byte array (you can decode it with the image package).  The screenshot is clipped to the element's bounding box and can capture an element below the document fold without scrolling.  Same-origin >>>-pierced iframe elements are translated to top-level page coordinates.

A capture can only contain what the browser painted, and the below-the-fold expansion applies to the DOCUMENT scroller: an element scrolled outside an inner overflow:auto container was never painted and comes back as that container's background.  Biloba prints a warning naming the container when it sees this - scroll it in with [Biloba.ScrollIntoView] and [Biloba.WithinScroller] first.  ([Biloba.HaveScreenshot] refuses outright rather than warning, since a blank baseline would pass forever.)

It is a waiting command: see [Biloba.CaptureScreenshot] for the WithTimeout/WithContext knobs it honors.

Read https://onsi.github.io/biloba/#capturing-screenshots for details.
*/
func (b *Biloba) CaptureScreenshotOf(selector any) []byte {
	b.gt.Helper()
	b.guardConfig("CaptureScreenshotOf", knobTimeout, knobContext)
	return b.captureScreenshotOf(selector)
}

// captureScreenshotOf is the unguarded substrate behind CaptureScreenshotOf and its imgcat/to-file
// wrappers.  The element capture runs under a bounded context (default screenshotCaptureTimeout) that
// honors the WithTimeout/WithContext knobs a waiting command is allowed.
func (b *Biloba) captureScreenshotOf(selector any) []byte {
	b.gt.Helper()
	img, _, notes, err := b.elementScreenshot(selector)
	if err != nil {
		b.gt.Fatalf("Failed to capture screenshot of element:\n%s", err.Error())
		return nil
	}
	// A warning rather than a failure: this is the manual capture, reached from a debugging session or
	// an AddReportEntry, and handing back a blank-but-honest PNG with a note beats failing a spec that
	// only wanted a look at the page.  The visual-regression path, where a blank capture would become a
	// golden master, refuses instead - see captureForComparison.
	if notes.clipped != nil {
		b.gt.Printf("Warning: %s\n", notes.clipped.describe(selector))
	}
	if notes.vanished {
		b.gt.Printf("Warning: %s\n", vanishedDuringCaptureMessage(selector))
	}
	return img
}

// vanishedDuringCaptureMessage names what just happened to a page that re-rendered itself out from
// under its own capture.  Without this the spec fails on whatever it does NEXT - polling for an
// element that was there a line ago - and points nowhere near the capture that removed it.
func vanishedDuringCaptureMessage(selector any) string {
	return fmt.Sprintf("the element matching %v was present before this capture and gone after it.\nReaching content outside the viewport requires expanding it, and a responsive page can observe that: a matchMedia flip or a resize handler that re-renders on the breakpoint will unmount the subtree being captured, taking component-local state with it.\nCapture a subject that is already fully in view (b.ScrollIntoView, then gate on b.BeInViewport(b.Fully())) and Biloba will not touch the viewport at all.", selector)
}

// clippedCapture describes an element that an ancestor is clipping out of its own capture: the
// browser never painted the hidden part, so those pixels come back as whatever the clipping ancestor's
// background is.  clipper names that ancestor and visibleFraction is how much of the element survives
// it (0 means the capture is entirely unpainted).  A nil *clippedCapture means nothing is clipping.
type clippedCapture struct {
	clipper         string
	visibleFraction float64
}

// fullyClipped reports the case worth refusing outright: none of the element was painted, so the
// capture is a flat rectangle of the clipping ancestor's background.
func (c *clippedCapture) fullyClipped() bool { return c != nil && c.visibleFraction <= 0 }

// describe renders the note a failure message or a warning prints.  It names the clipping ancestor
// because that is the part the reader cannot see from the call site: the selector they wrote is right
// there in the spec, the container that swallowed it is somewhere up their markup.
func (c *clippedCapture) describe(selector any) string {
	what := "Only part of"
	if c.fullyClipped() {
		what = "NONE of"
	}
	return fmt.Sprintf("%s the element matching %v was painted: it is inside %s, which clips it, and only %.0f%% of the element falls within that clip.\nA screenshot can only contain what the browser actually rendered, so the rest of this capture is whatever %s paints behind it - not the element.\nScroll it into the container's visible band before capturing:\n  b.ScrollIntoView(%#v, b.WithinScroller(%q))",
		what, selector, c.clipper, c.visibleFraction*100, c.clipper, selector, c.clipper)
}

// clippedCaptureError is what the visual-regression path returns for a fully clipped element.  It is
// a distinct type so update mode can recognise it and escalate - see screenshotMatcher.refuseToWrite.
type clippedCaptureError struct {
	clipped  *clippedCapture
	selector any
}

func (e *clippedCaptureError) Error() string { return e.clipped.describe(e.selector) }

// captureNotes carries what a capture noticed about its own subject, alongside the pixels.  Both
// entries are diagnoses of the setup rather than of the page, so callers that poll print them once.
type captureNotes struct {
	// clipped is set when an ancestor cut the subject out of its own capture.
	clipped *clippedCapture
	// vanished is set when the subject was there before the capture and gone after - see the note on
	// expandsViewport for how a capture manages to do that to a page.
	vanished bool
}

// expandsViewport reports whether capturing this box needs Chrome's captureBeyondViewport, and is
// the whole of the fix for a capture that changes the page it is capturing.
//
// captureBeyondViewport is what lets an element capture reach content below the document fold
// without scrolling, and it is not free: Chrome drives the layout viewport down and back to do it,
// and the page OBSERVES that.  matchMedia flips, a resize fires, and a responsive app that renders
// off its breakpoint can unmount and remount the subtree being captured - taking the subject, and any
// component-local state in it, with it.  The spec then polls for an element its own capture destroyed,
// which surfaces as an intermittent timeout pointing at the line AFTER the capture.
//
// A subject already fully in view needs none of that: the clip is interpreted in page coordinates
// either way, so the same pixels come back without touching the viewport.  That is the common case,
// and it is every case for a suite that captures what it can see.
func expandsViewport(box map[string]any) bool {
	inViewport, ok := box["inViewport"].(bool)
	return !(ok && inViewport)
}

// elementScreenshot captures the first element matching selector, clipped to its bounding box, and
// hands back the PNG along with the clip it used.  The clip is what turns a mask rectangle measured in
// document coordinates into image coordinates, which is why the visual-regression path needs it back.
// It also reports what the capture noticed about its subject - see captureNotes.
// Unlike captureScreenshotOf it returns errors rather than failing the spec: a matcher polls it, and
// an error there means "retry".
func (b *Biloba) elementScreenshot(selector any) ([]byte, *page.Viewport, captureNotes, error) {
	notes := captureNotes{}
	r := b.runBilobaHandler("boundingBox", selector)
	if r.Error() != nil {
		return nil, nil, notes, r.Error()
	}
	box, ok := r.Result.(map[string]any)
	if !ok {
		return nil, nil, notes, fmt.Errorf("unexpected bounding box result: %v", r.Result)
	}
	if clipper, ok := box["clipper"].(string); ok && clipper != "" {
		notes.clipped = &clippedCapture{clipper: clipper, visibleFraction: toFloat64(box["visibleFraction"])}
	}
	clip := &page.Viewport{
		X:      toFloat64(box["x"]),
		Y:      toFloat64(box["y"]),
		Width:  toFloat64(box["width"]),
		Height: toFloat64(box["height"]),
		Scale:  1,
	}
	beyondViewport := expandsViewport(box)
	cctx, cancel := b.waitingContext(screenshotCaptureTimeout)
	defer cancel()
	img, err := engine.CaptureClipContext(cctx, clip, beyondViewport)
	if err != nil {
		return nil, clip, notes, err
	}
	// Only worth asking when the viewport was actually perturbed: the subject was demonstrably there a
	// moment ago (we just measured it), so if it is gone now, the capture is what removed it.  Saying
	// that costs one round trip and replaces an investigation - the spec's next assertion will
	// otherwise poll for a missing element and blame the line it is on.
	if beyondViewport {
		if stillThere := b.runBilobaHandler("exists", selector); stillThere.Error() == nil && !stillThere.Success {
			notes.vanished = true
		}
	}
	return img, clip, notes, nil
}

/*
CaptureImgcatScreenshotOf(selector) returns a screenshot of the first element matching selector as an iTerm2 imgcat-compatible string.  Simply print it out to see the image on your terminal.

It is a waiting command: see [Biloba.CaptureScreenshot] for the WithTimeout/WithContext knobs it honors.

Read https://onsi.github.io/biloba/#capturing-screenshots for details.
*/
func (b *Biloba) CaptureImgcatScreenshotOf(selector any) string {
	b.gt.Helper()
	b.guardConfig("CaptureImgcatScreenshotOf", knobTimeout, knobContext)
	return b.asImgCat(b.captureScreenshotOf(selector))
}

/*
CaptureScreenshotOfToFile writes a screenshot of the first element matching selector as a PNG file to the given path and returns its absolute path.
The directory is created if it does not already exist.
The absolute path is printed to the test output so it appears in failure output and is readable by tools that can render PNG files.

It is a waiting command: see [Biloba.CaptureScreenshot] for the WithTimeout/WithContext knobs it honors.

Read https://onsi.github.io/biloba/#capturing-screenshots for details.
*/
func (b *Biloba) CaptureScreenshotOfToFile(selector any, path string) string {
	b.gt.Helper()
	b.guardConfig("CaptureScreenshotOfToFile", knobTimeout, knobContext)
	return b.writeScreenshotToFile(b.captureScreenshotOf(selector), path)
}

func (b *Biloba) asImgCat(img []byte) string {
	return b.asInlineImage(img, inlineImageITerm)
}

// encodeInlineImage encodes a PNG into the escape sequence for the given terminal inline-image
// protocol, returning "" for inlineImageNone.  It reports an encoding error instead of failing the
// test: asInlineImage is the caller that turns the error into a Fatalf, but a failure message being
// rendered (see visual.go) cannot do that - it runs while a spec is already failing, and during a
// progress report it runs on Ginkgo's goroutine.
func encodeInlineImage(img []byte, proto inlineImageProtocol) (string, error) {
	buf := &bytes.Buffer{}
	switch proto {
	case inlineImageITerm:
		buf.WriteString("\033]1337;File=;inline=1:")
		encoder := base64.NewEncoder(base64.StdEncoding, buf)
		if _, err := encoder.Write(img); err != nil {
			return "", err
		}
		encoder.Close()
		buf.WriteString("\033\\")
	case inlineImageKitty:
		if err := rasterm.KittyCopyPNGInline(buf, bytes.NewReader(img), rasterm.KittyImgOpts{}); err != nil {
			return "", err
		}
	case inlineImageSixel:
		paletted, err := pngToPaletted(img)
		if err != nil {
			return "", err
		}
		if err := rasterm.SixelWriteImage(buf, paletted); err != nil {
			return "", err
		}
	default:
		return "", nil
	}
	return buf.String(), nil
}

// asInlineImage encodes a PNG screenshot into the escape sequence for the given
// terminal inline-image protocol.  Returns "" for inlineImageNone.
func (b *Biloba) asInlineImage(img []byte, proto inlineImageProtocol) string {
	encoded, err := encodeInlineImage(img, proto)
	if err != nil {
		b.gt.Fatalf("Failed to encode inline screenshot:\n%s", err.Error())
	}
	return encoded
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
		// Bound the capture so a wedged tab can't hang screenshot collection, but keep it generous:
		// FullScreenshot encodes a PNG of the whole page and, under heavy parallel/CI load, legitimately
		// takes well over a second.  A 1s bound here spuriously timed out healthy captures - surfacing as
		// "Timed out attempting to fetch screenshot" noise and flaking the inline-encoding specs.
		ctx, cancel := context.WithTimeout(tab.Context, screenshotCaptureTimeout)
		defer cancel()

		var originalWidth, originalHeight int
		if width > 0 && height > 0 {
			originalWidth, originalHeight = b.WindowSize()
			err := engine.EmulateViewportContext(ctx, width, height)
			if err != nil {
				out = append(out, tabScreenshot{failure: fmt.Sprintf("failed to set window size: %s", err.Error())})
				continue
			}
		}
		img, title, err := engine.CaptureFullScreenshotContext(ctx, 100)
		if width > 0 && height > 0 {
			err := engine.EmulateViewportContext(ctx, originalWidth, originalHeight, chromedp.EmulatePortrait)
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

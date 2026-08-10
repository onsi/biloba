package biloba

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

// defaultScreenshotBaselinesDir is where committed visual-regression baselines live when neither
// BilobaConfigScreenshotBaselinesDir nor BILOBA_SCREENSHOT_BASELINES_DIR names a directory.  Baselines
// have the opposite lifecycle from failure screenshots: they are few, small, reviewed, and checked in,
// so they get their own directory rather than sharing the gitignored screenshots dir.
const defaultScreenshotBaselinesDir = "./biloba-baselines"

// screenshotSettleStreak, screenshotSettleAttempts and screenshotSettleInterval tune the settle loop
// that update mode runs before it writes a baseline.
//
// Three captures in a row have to agree, not two.  A page that cycles through a handful of renderings
// will show any two captures the same often enough to fool a pair; needing a third makes a single
// coincidence insufficient.
//
// Eight captures is the overall bound.  It leaves six chances to hit a run of three, which is plenty
// for the things that legitimately settle late - a web font swapping in, an image decoding, a
// ResizeObserver re-running its layout - all of which land inside the first second on a page that is
// otherwise done.
//
// The pause before each capture GROWS rather than staying at the base interval: 100ms, 140, 200, 280,
// 380, 500, 640 (see screenshotSettleGap).  A fixed sampling period is the whole problem - sample a
// 160ms animation every 160ms and every pair matches, so the page looks settled while it is plainly
// not.  Unequal gaps mean no single animation period can satisfy every pair, and because the gaps grow
// by a different amount each time (40ms, then 60, then 80...) no period divides two of them in a row
// either, so a run of three cannot be manufactured by any animation slow enough to actually render
// distinct frames.  The base 100ms is longer than a frame, so consecutive captures are never two reads
// of the same paint.
//
// The cost of all this is about 2.2s of waiting plus eight captures in the worst case, against roughly
// half a second for a page that settles immediately.  That is a deliberate trade: update mode is a
// rare, explicit operation, and an untrustworthy baseline is paid for on every run afterwards.
const screenshotSettleStreak = 3
const screenshotSettleAttempts = 8
const screenshotSettleInterval = 100 * time.Millisecond

// screenshotSettleGap is how long to wait before the nth follow-up capture (n counts from 0).  The
// quadratic term is what makes each gap grow by more than the last - see the note above on why every
// gap has to be a different length.
func screenshotSettleGap(n int) time.Duration {
	return screenshotSettleInterval * time.Duration(n*n+3*n+10) / 10
}

// screenshotConfig is the resolved configuration for one HaveScreenshot assertion.
type screenshotConfig struct {
	masks        []any
	tolerance    screenshotTolerance
	animated     bool
	colorSchemes []string
}

/*
ScreenshotOption configures [Biloba.HaveScreenshot].  The options are [Biloba.Mask],
[Biloba.Tolerance], [Biloba.ChannelTolerance], [Biloba.Animated], and [Biloba.InColorSchemes]:

	Eventually(".receipt").Should(b.HaveScreenshot("receipt", b.Mask(".timestamp"), b.Tolerance(0.001)))

Read https://onsi.github.io/biloba/#visual-assertions to learn more about visual assertions
*/
type ScreenshotOption func(*screenshotConfig)

/*
Mask(selectors...) is a [ScreenshotOption] for [Biloba.HaveScreenshot]: every element matching any of
the selectors is painted out with a flat colour before the comparison, so a region that legitimately
changes on every run (a timestamp, an avatar, an ad slot) stops failing the assertion:

	Eventually(b).Should(b.HaveScreenshot("home", b.Mask(".timestamp", b.XPath().WithID("ad"))))

The same paint is applied before a baseline is written and before every comparison, so both sides of
the comparison are always masked identically.  A selector that matches nothing is a no-op - masking an
element that is only sometimes on the page must not fail the assertion.

Read https://onsi.github.io/biloba/#visual-assertions to learn more about visual assertions
*/
func (b *Biloba) Mask(selectors ...any) ScreenshotOption {
	return func(c *screenshotConfig) { c.masks = append(c.masks, selectors...) }
}

/*
Tolerance(fraction) is a [ScreenshotOption] for [Biloba.HaveScreenshot]: at most fraction (0..1) of
the compared pixels may differ before the comparison fails.  The default is 0 - exact.

	Eventually(b).Should(b.HaveScreenshot("home", b.Tolerance(0.002)))  // up to 0.2% of pixels may differ

Prefer setting a suite-wide default with [BilobaConfigScreenshotTolerance] and reaching for this
option only where one assertion genuinely needs different slack: a tolerance number repeated at two
hundred call sites is a suite nobody can reason about.

Read https://onsi.github.io/biloba/#visual-assertions to learn more about visual assertions
*/
func (b *Biloba) Tolerance(fraction float64) ScreenshotOption {
	return func(c *screenshotConfig) { c.tolerance.fraction = fraction }
}

/*
ChannelTolerance(delta) is a [ScreenshotOption] for [Biloba.HaveScreenshot]: a pixel only counts as
differing when one of its R/G/B/A channels differs by MORE than delta.  This is the knob that absorbs
antialiasing and subpixel rendering, which shift a text edge by a few levels without changing what the
page looks like.  The default is 0 - exact.

	Eventually(".prose").Should(b.HaveScreenshot("prose", b.ChannelTolerance(6)))

It is independent of [Biloba.Tolerance]: channel tolerance decides which pixels count as different,
fraction tolerance decides how many of them are acceptable.

Read https://onsi.github.io/biloba/#visual-assertions to learn more about visual assertions
*/
func (b *Biloba) ChannelTolerance(delta int) ScreenshotOption {
	return func(c *screenshotConfig) { c.tolerance.channel = delta }
}

/*
Animated() is a [ScreenshotOption] for [Biloba.HaveScreenshot] that opts OUT of the rendering freeze.

By default Biloba injects a stylesheet that turns CSS animations and transitions off, hides the text
caret, and disables smooth scrolling for the duration of the capture, so the same page always renders
the same pixels.  Pass Animated when the animation is the thing you are asserting on and you have made
it deterministic yourself:

	Eventually(".spinner").Should(b.HaveScreenshot("spinner-mid-spin", b.Animated()))

Read https://onsi.github.io/biloba/#visual-assertions to learn more about visual assertions
*/
func (b *Biloba) Animated() ScreenshotOption {
	return func(c *screenshotConfig) { c.animated = true }
}

/*
InColorSchemes(schemes...) is a [ScreenshotOption] for [Biloba.HaveScreenshot]: the subject is captured
and compared once per scheme, with prefers-color-scheme emulated to that value, and every scheme must
match for the assertion to pass:

	Eventually(b).Should(b.HaveScreenshot("home", b.InColorSchemes("light", "dark")))

Each scheme gets its own baseline, named <name>-<scheme>.png.  Without this option Biloba does not
emulate prefers-color-scheme at all and stores a single <name>.png.

Read https://onsi.github.io/biloba/#visual-assertions to learn more about visual assertions
*/
func (b *Biloba) InColorSchemes(schemes ...string) ScreenshotOption {
	return func(c *screenshotConfig) { c.colorSchemes = append(c.colorSchemes, schemes...) }
}

/*
HaveScreenshot(name, options...) is a Gomega matcher that passes once the subject renders identically
to the committed baseline stored under name.  Apply it to a selector to compare one element, or to the
tab itself to compare the whole page:

	Eventually(".librarian").Should(b.HaveScreenshot("librarian-rail"))
	Eventually(b).Should(b.HaveScreenshot("home-desktop"))

Every attempt captures fresh and compares, so the poll is what waits out fonts loading and layout
settling - the first attempt that matches is by construction the settled one.  Configure the
Eventually/Expect that wraps it, not the matcher (WithTimeout and friends on HaveScreenshot are a
hard error).  It composes:

	Eventually(".card").Should(SatisfyAll(b.BeInViewport(b.Fully()), b.HaveScreenshot("card")))

Baselines live in ./biloba-baselines (see [BilobaConfigScreenshotBaselinesDir]) as <name>.png; name
may contain "/" to organise them into subdirectories.  A MISSING baseline fails loudly rather than
being written and passed - a spec that has never compared anything would otherwise read green in CI -
and says to re-run with BILOBA_UPDATE_SCREENSHOTS=1, which (re)writes baselines from a settled
rendering and reports in words what changed.  A failing comparison writes the captured image and a
rendered diff next to the failure screenshots and says what moved, where, and by how much.

Configure it with [Biloba.Mask], [Biloba.Tolerance], [Biloba.ChannelTolerance], [Biloba.Animated], and
[Biloba.InColorSchemes].

Read https://onsi.github.io/biloba/#visual-assertions to learn more about visual assertions
*/
func (b *Biloba) HaveScreenshot(name string, options ...ScreenshotOption) types.GomegaMatcher {
	b.gt.Helper()
	b.guardBareMatcher("HaveScreenshot")
	cfg := screenshotConfig{tolerance: b.root.screenshotTolerance}
	for _, option := range options {
		option(&cfg)
	}
	if len(cfg.colorSchemes) == 0 {
		// The empty scheme means "don't emulate prefers-color-scheme at all": one capture, one
		// baseline, whatever the page renders on its own.
		cfg.colorSchemes = []string{""}
	}
	m := &screenshotMatcher{b: b, name: name, cfg: cfg}
	m.CustomGomegaMatcher = gcustom.MakeMatcher(m.match)
	return m
}

// screenshotMatcher is the matcher HaveScreenshot returns.  It embeds a gcustom matcher for Match and
// hand-rolls its failure messages because rendering one has a side effect: it runs the diff analysis
// and writes the actual and diff PNGs, and a template can read state but cannot produce it.
//
// That side effect is not confined to a failing assertion.  Gomega registers a matcher's message
// generator as a Ginkgo progress reporter for the duration of every Eventually, so a progress report
// (SIGINFO, --poll-progress-after, a spec timeout) calls FailureMessage from Ginkgo's own goroutine
// while match is still polling.  Two consequences, both handled: the shared comparison state below is
// behind lock, and the PNGs can legitimately be written mid-poll.  The latter is harmless - they are
// gitignored artifacts written to a fixed path, and the real failure overwrites them - so the writes
// are made atomic (rename) rather than designed away.
type screenshotMatcher struct {
	gcustom.CustomGomegaMatcher

	b    *Biloba
	name string
	cfg  screenshotConfig

	// The last failing comparison, kept so FailureMessage can render it.  scheme names which colour
	// scheme it belongs to; actual is the masked PNG that produced it.  lock guards all three: match
	// writes them, FailureMessage reads them, and the two run on different goroutines.
	lock   sync.Mutex
	scheme string
	diff   *screenshotDiff
	actual []byte
}

// match runs one full attempt: every configured colour scheme is captured and compared, and they must
// all match.  A scheme that differs comes back as (false, nil) so the caller's Eventually keeps
// waiting for the page to settle; only a condition that can never come true (a missing baseline, an
// undecodable PNG) aborts the poll with StopTrying.
func (m *screenshotMatcher) match(actual any) (bool, error) {
	tab, selector, err := m.subject(actual)
	if err != nil {
		return false, err
	}
	m.record("", nil, nil)
	for _, scheme := range m.cfg.colorSchemes {
		pass, err := m.matchScheme(tab, selector, scheme)
		if err != nil {
			return false, err
		}
		if !pass {
			return false, nil // the first failing scheme is the one we diagnose
		}
	}
	return true, nil
}

// subject resolves what the assertion was applied to.  The tab itself means a full-page capture
// (selector comes back nil); anything else has to be a selector.
func (m *screenshotMatcher) subject(actual any) (*Biloba, any, error) {
	if tab, ok := actual.(*Biloba); ok {
		return tab, nil, nil
	}
	if _, err := encodeSelector(actual); err != nil {
		// StopTrying, not a plain error: no amount of polling turns an int into a selector, and a
		// wrong-typed subject would otherwise burn the whole Eventually timeout before saying so.
		return nil, nil, gomega.StopTrying(fmt.Sprintf("HaveScreenshot takes either a selector (a CSS string, an XPath, or a Locator) to capture one element, or the tab itself to capture the whole page.  Got:\n%s", format.Object(actual, 1)))
	}
	return m.b, actual, nil
}

func (m *screenshotMatcher) matchScheme(tab *Biloba, selector any, scheme string) (bool, error) {
	label := m.label(scheme)
	baselinePath, err := m.baselinePath(scheme)
	if err != nil {
		return false, gomega.StopTrying(err.Error())
	}

	if m.b.root.updateScreenshots {
		// Update mode has no Eventually behind it, so it has to do its own settling - see
		// captureSettled.
		img, err := m.captureSettled(tab, selector, scheme, label)
		if err != nil {
			return false, err
		}
		baseline, readErr := os.ReadFile(baselinePath)
		return m.updateBaseline(baselinePath, baseline, readErr, img, label)
	}

	img, err := tab.captureForComparison(selector, m.cfg, scheme)
	if err != nil {
		return false, err
	}
	baseline, readErr := os.ReadFile(baselinePath)

	if os.IsNotExist(readErr) {
		// Writing the baseline here and passing would mean a spec that has never compared anything
		// reads green in CI.  So this is a failure - and a StopTrying one, since no amount of polling
		// is going to conjure the file.
		actualPath := writeVisualArtifact(m.artifactPath(scheme, "actual"), img)
		return false, gomega.StopTrying(missingBaselineMessage(label, baselinePath, actualPath))
	}
	if readErr != nil {
		return false, gomega.StopTrying(fmt.Sprintf("Failed to read the screenshot baseline for %s:\n  %s\n%s", label, baselinePath, readErr.Error()))
	}

	diff, err := compareScreenshots(baseline, img, m.cfg.tolerance)
	if err != nil {
		return false, gomega.StopTrying(fmt.Sprintf("Failed to compare %s against its baseline:\n%s", label, err.Error()))
	}
	if diff.Match {
		return true, nil
	}
	m.record(scheme, diff, img)
	return false, nil
}

// record and lastFailure are the only access to the shared comparison state - see the note on
// screenshotMatcher for why FailureMessage can run concurrently with match.
func (m *screenshotMatcher) record(scheme string, diff *screenshotDiff, actual []byte) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.scheme, m.diff, m.actual = scheme, diff, actual
}

func (m *screenshotMatcher) lastFailure() (string, *screenshotDiff, []byte) {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.scheme, m.diff, m.actual
}

// captureSettled captures until screenshotSettleStreak captures in a row compare equal under the
// configured tolerance, and returns the last of them.  Every other path through this matcher is
// settled by the caller's Eventually - the first attempt that matches the baseline is by construction
// a settled one - but update mode passes on the first attempt no matter what it saw, so a baseline
// written while a web font was still loading would bake in that transient frame and every later run
// would poll forever for a state that never comes back.  See the note on screenshotSettleStreak for
// why it takes three captures and why the gaps between them grow.
//
// The comparison uses the configured tolerance so ordinary antialiasing noise cannot keep the page
// from ever counting as settled, and it runs on the images captureForComparison already masked: a
// clock that has been masked out must not be what stops the page from settling.
func (m *screenshotMatcher) captureSettled(tab *Biloba, selector any, scheme string, label string) ([]byte, error) {
	start := time.Now()
	var previous []byte
	streak := 1 // a lone capture is a run of one; it takes screenshotSettleStreak of them to settle
	for attempt := range screenshotSettleAttempts {
		if attempt > 0 {
			time.Sleep(screenshotSettleGap(attempt - 1))
		}
		current, err := tab.captureForComparison(selector, m.cfg, scheme)
		if err != nil {
			return nil, err
		}
		if previous != nil {
			if diff, err := compareScreenshots(previous, current, m.cfg.tolerance); err == nil && diff.Match {
				streak++
				if streak >= screenshotSettleStreak {
					return current, nil
				}
			} else {
				streak = 1
			}
		}
		previous = current
	}
	// Not a failure: the next normal run will fail on its own and that is the right place to enforce
	// this.  What that run cannot tell you is that the baseline it is comparing against was never
	// settled in the first place, so say so here, loudly.
	m.b.gt.Printf("The screenshot for %s never settled: no %d captures in a row matched, across %d captures over %s.\nBiloba wrote the last one, but a baseline captured from a page that is still changing will fail on every later run.\nMask the changing region with b.Mask(...), or track down what is still moving.\n",
		label, screenshotSettleStreak, screenshotSettleAttempts, time.Since(start).Round(100*time.Millisecond))
	return previous, nil
}

// updateBaseline is the BILOBA_UPDATE_SCREENSHOTS path: settle, write, pass.  When a baseline was
// already there and the capture differs from it, the difference is printed to the test output - an
// updated baseline is otherwise a binary blob in a diff that nobody can review.
func (m *screenshotMatcher) updateBaseline(path string, previous []byte, readErr error, img []byte, label string) (bool, error) {
	switch {
	case readErr != nil:
		m.b.gt.Printf("Wrote a new screenshot baseline for %s:\n  %s\n", label, path)
	default:
		if diff, err := compareScreenshots(previous, img, m.cfg.tolerance); err == nil && !diff.Match {
			m.b.gt.Printf("Updated the screenshot baseline for %s:\n  %s\n%s\n", label, path, diff.summary(label))
		}
	}
	if err := writeFileAtomically(path, img); err != nil {
		return false, gomega.StopTrying(fmt.Sprintf("Failed to write the screenshot baseline for %s:\n  %s\n%s", label, path, err.Error()))
	}
	return true, nil
}

func (m *screenshotMatcher) FailureMessage(actual any) string {
	// Snapshot the shared state and let go of the lock: everything below is image analysis and disk
	// I/O, and holding the lock across it would stall the poll that is producing it.
	scheme, diff, img := m.lastFailure()
	if diff == nil {
		return fmt.Sprintf("Expected the screenshot to match the %q baseline.", m.name)
	}
	// This is where the expensive half of the comparison happens - once, on the attempt that reports,
	// rather than on every failing poll attempt.
	diff.analyze()
	label := m.label(scheme)
	paths := screenshotPaths{Actual: writeVisualArtifact(m.artifactPath(scheme, "actual"), img)}
	if baselinePath, err := m.baselinePath(scheme); err == nil {
		paths.Baseline = baselinePath
	}
	if diffPNG, err := diff.encodeDiffPNG(); err == nil && diffPNG != nil {
		paths.Diff = writeVisualArtifact(m.artifactPath(scheme, "diff"), diffPNG)
	}
	return diff.diagnose(label, paths)
}

func (m *screenshotMatcher) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf("Expected the screenshot NOT to match the %q baseline, but it did.", m.name)
}

// label names the comparison the way a failure message should: the baseline name, plus the colour
// scheme when there is more than one thing called by that name.
func (m *screenshotMatcher) label(scheme string) string {
	if scheme == "" {
		return m.name
	}
	return fmt.Sprintf("%s (prefers-color-scheme: %s)", m.name, scheme)
}

func (m *screenshotMatcher) baselinePath(scheme string) (string, error) {
	rel, err := baselineRelativePath(m.name, scheme)
	if err != nil {
		return "", err
	}
	return absOrAsIs(filepath.Join(m.b.visualBaselinesDir(), rel)), nil
}

// artifactPath is where the actual/diff PNG for this comparison goes.  Artifacts are flat files in the
// (gitignored) screenshots directory, so a name with "/" in it collapses to a single sanitized
// filename rather than growing a directory tree nobody committed.
func (m *screenshotMatcher) artifactPath(scheme string, kind string) string {
	name := sanitizeForFilename(m.name)
	if scheme != "" {
		name += "-" + sanitizeForFilename(scheme)
	}
	return absOrAsIs(filepath.Join(m.b.visualArtifactsDir(), fmt.Sprintf("%s.%s.png", name, kind)))
}

// captureForComparison performs one visual capture: emulate the colour scheme, freeze rendering,
// capture, read the mask geometry while the page is still in exactly the state that was captured, and
// paint the masks in.  Everything it turns on is turned off by a defer - a freeze stylesheet or an
// emulated colour scheme left behind on an error path would silently corrupt every later assertion in
// this tab.
func (b *Biloba) captureForComparison(selector any, cfg screenshotConfig, scheme string) ([]byte, error) {
	if scheme != "" {
		if err := b.emulateColorScheme(scheme); err != nil {
			return nil, err
		}
		defer func() {
			if err := b.emulateColorScheme(""); err != nil {
				// A dropped clear used to be silent, and the override outlives Navigate: every later
				// assertion in this tab would have rendered in the emulated scheme with no signal.  Say
				// so, and leave the flag set so the next Prepare() tries again.
				b.gt.Printf("Failed to clear the emulated prefers-color-scheme (%s) after a screenshot capture:\n  %s\nThis tab keeps rendering as %s until the next b.Prepare().\n", scheme, err.Error(), scheme)
			}
		}()
	}
	if !cfg.animated {
		if r := b.runBilobaFunc("freezeRendering"); r.Error() != nil {
			return nil, r.Error()
		}
		defer func() { b.runBilobaFunc("unfreezeRendering") }()
	}

	var img []byte
	var err error
	// originX/originY place the captured image in document coordinates - the space the mask boxes are
	// measured in.  A full-page capture starts at the document origin; an element capture starts at its
	// clip.  cssWidth is the capture's width in CSS pixels, which is how we recover the device scale
	// factor from the decoded image.
	var originX, originY, cssWidth float64
	if selector == nil {
		img, cssWidth, err = b.fullPageScreenshot()
	} else {
		var clip *page.Viewport
		img, clip, err = b.elementScreenshot(selector)
		if clip != nil {
			originX, originY, cssWidth = clip.X, clip.Y, clip.Width
		}
	}
	if err != nil {
		return nil, err
	}

	rects, err := b.maskRects(cfg.masks, img, originX, originY, cssWidth)
	if err != nil {
		return nil, err
	}
	if len(rects) == 0 {
		return img, nil
	}
	return maskPNG(img, rects)
}

// emulateColorScheme overrides prefers-color-scheme for this tab; the empty scheme clears the
// override.  It keeps b.colorSchemeEmulated honest so Prepare() knows whether there is anything to
// clean up: the flag goes up BEFORE the command that sets the override (a command that reports an
// error may still have landed) and only comes down when a clear actually succeeds.
func (b *Biloba) emulateColorScheme(scheme string) error {
	ctx, cancel := b.waitingContext(screenshotCaptureTimeout)
	defer cancel()
	params := emulation.SetEmulatedMedia()
	if scheme != "" {
		params = params.WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: scheme}})
		b.setColorSchemeEmulated(true)
	}
	err := chromedp.Run(ctx, params)
	if scheme == "" && err == nil {
		b.setColorSchemeEmulated(false)
	}
	return err
}

func (b *Biloba) setColorSchemeEmulated(emulated bool) {
	if b.colorSchemeEmulated != nil {
		*b.colorSchemeEmulated = emulated
	}
}

// clearLeakedColorSchemeEmulation is Prepare()'s half of the colour-scheme teardown, called from
// resetBrowsingState.  emulation.SetEmulatedMedia is a TARGET-level override: it survives Navigate,
// so the one capture whose deferred clear did not land (an expired capture timeout, a cancelled
// context) would otherwise leave every later spec in this process rendering in dark mode.  Gated on
// the flag rather than run unconditionally - Prepare runs before every spec, and in the overwhelming
// case there is no override and this costs nothing.
func (b *Biloba) clearLeakedColorSchemeEmulation() {
	if b.colorSchemeEmulated == nil || !*b.colorSchemeEmulated {
		return
	}
	if err := b.emulateColorScheme(""); err != nil {
		b.gt.Printf("Failed to clear a leaked emulated prefers-color-scheme during Prepare():\n  %s\n", err.Error())
	}
}

// fullPageScreenshot captures the whole document and also reports its width in CSS pixels.  The two
// come from the same round trip so they describe the same layout; the width is what maskRects divides
// by to recover the device scale factor.
func (b *Biloba) fullPageScreenshot() ([]byte, float64, error) {
	ctx, cancel := b.waitingContext(screenshotCaptureTimeout)
	defer cancel()
	var img []byte
	var cssWidth float64
	err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, _, _, _, _, cssContentSize, err := page.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
			}
			cssWidth = cssContentSize.Width
			return nil
		}),
		chromedp.FullScreenshot(&img, 100),
	)
	return img, cssWidth, err
}

// maskRects resolves the mask selectors into image-coordinate rectangles for the capture in img.
func (b *Biloba) maskRects(masks []any, img []byte, originX, originY, cssWidth float64) ([]image.Rectangle, error) {
	if len(masks) == 0 {
		return nil, nil
	}
	decoded, err := png.DecodeConfig(bytes.NewReader(img))
	if err != nil {
		return nil, err
	}
	// Recover the device scale factor from the image we actually captured rather than assuming 1: at 2x
	// the PNG is twice as wide as its CSS box and a mask computed in CSS pixels would land on the wrong
	// quarter of the image.
	scale := 1.0
	if cssWidth > 0 {
		scale = float64(decoded.Width) / cssWidth
	}
	// One call for the whole mask list: every box is measured in the same synchronous snippet, so the
	// masks can't come from different frames of a page that is still moving.
	encoded := make([]string, len(masks))
	for i, mask := range masks {
		selector, err := encodeSelector(mask)
		if err != nil {
			return nil, err
		}
		encoded[i] = selector
	}
	r := b.runBilobaFunc("maskBoxes", encoded)
	if r.Error() != nil {
		return nil, r.Error()
	}
	rects := []image.Rectangle{}
	for _, entry := range r.ResultAnySlice() {
		box, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		x, y := toFloat64(box["x"])-originX, toFloat64(box["y"])-originY
		width, height := toFloat64(box["width"]), toFloat64(box["height"])
		// Round outward: a fractional CSS box that got floored on both edges leaves a sliver of the
		// masked-out thing showing, which defeats the point of masking it.
		rects = append(rects, image.Rect(
			int(math.Floor(x*scale)), int(math.Floor(y*scale)),
			int(math.Ceil((x+width)*scale)), int(math.Ceil((y+height)*scale)),
		))
	}
	return rects, nil
}

// runBilobaFunc is runBilobaHandler for the handful of _biloba primitives that take no selector.
func (b *Biloba) runBilobaFunc(name string, args ...any) *bilobaJSResponse {
	b.ensureBiloba()
	result := &bilobaJSResponse{}
	if _, err := b.RunErr(b.JSFunc("_biloba."+name).Invoke(args...), result); err != nil {
		result.Err = err.Error()
	}
	return result
}

// visualBaselinesDir is where committed baselines are read from and written to.  ConnectToChrome
// resolves this; the fallback keeps a hand-built Biloba honest.
func (b *Biloba) visualBaselinesDir() string {
	if b.root.baselinesDir != "" {
		return b.root.baselinesDir
	}
	return defaultScreenshotBaselinesDir
}

// visualArtifactsDir is where the actual/diff PNGs go when a comparison fails.  They are throwaway,
// gitignored output, so they ride along with the failure screenshots rather than getting a knob of
// their own.
func (b *Biloba) visualArtifactsDir() string {
	if b.root.screenshotsDir != "" {
		return b.root.screenshotsDir
	}
	return defaultAutomationScreenshotsDir
}

// baselineRelativePath turns a baseline name into a path relative to the baselines directory.  name
// may contain "/" so a suite can organise baselines into subdirectories, and each segment is sanitized
// the way failure-screenshot filenames are.  An absolute name or a ".." segment is refused rather than
// sanitized: quietly rewriting a path that tries to leave the baselines directory would hide the
// mistake instead of reporting it.
func baselineRelativePath(name string, scheme string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("HaveScreenshot needs a name to look its baseline up by, but was given an empty one")
	}
	if filepath.IsAbs(name) || strings.HasPrefix(name, "/") {
		return "", fmt.Errorf("the screenshot name %q must be relative to the baselines directory", name)
	}
	segments := strings.Split(name, "/")
	for i, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf(`the screenshot name %q may not contain empty, "." or ".." path segments`, name)
		}
		segments[i] = sanitizeForFilename(segment)
		if segments[i] == "" {
			return "", fmt.Errorf("the screenshot name %q has a path segment with no usable filename characters in it", name)
		}
	}
	last := len(segments) - 1
	if scheme != "" {
		segments[last] += "-" + sanitizeForFilename(scheme)
	}
	segments[last] += ".png"
	return filepath.Join(segments...), nil
}

// missingBaselineMessage says what is missing, what Biloba saw instead, and exactly how to create the
// baseline.  It is the first thing anyone adding a visual assertion reads, so it spells out the
// write-and-pass decision rather than leaving it to be discovered.
func missingBaselineMessage(label string, baselinePath string, actualPath string) string {
	out := &strings.Builder{}
	fmt.Fprintf(out, "There is no screenshot baseline for %s.\n\nExpected to find one at:\n  %s\n", label, baselinePath)
	if actualPath != "" {
		fmt.Fprintf(out, "\nThe screenshot Biloba just captured was written to:\n  %s\n", actualPath)
	}
	fmt.Fprintf(out, "\nReview that image, then create the baseline by re-running with:\n  BILOBA_UPDATE_SCREENSHOTS=1\n\nBiloba will not create a missing baseline and pass: a spec that has never compared anything would read green in CI.")
	return out.String()
}

// writeVisualArtifact writes an actual/diff PNG and returns its absolute path, or "" when it could not
// be written.  It is deliberately best-effort and quiet: it runs while a failure message is being
// rendered, and a spec that is already failing should report the comparison, not a disk error.
//
// The write goes through writeFileAtomically because a failure message can be rendered twice at once
// (a Ginkgo progress report lands while the assertion is failing) onto the same path: whichever
// rename wins, the file is a whole PNG rather than two interleaved ones.
func writeVisualArtifact(path string, img []byte) string {
	if len(img) == 0 {
		return ""
	}
	if err := writeFileAtomically(path, img); err != nil {
		return ""
	}
	return path
}

// writeFileAtomically writes data via a temp file in the same directory and a rename.  Biloba suites
// run across several Ginkgo processes and two of them can update the same baseline at once; without
// the rename one could read the half-written PNG the other is still producing.
func writeFileAtomically(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op once the rename below has succeeded
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0644); err != nil { // CreateTemp makes 0600 files
		return err
	}
	return os.Rename(tmp, path)
}

// absOrAsIs absolutizes a path for display and for writing, falling back to the original when the
// working directory can't be resolved.
func absOrAsIs(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// truthyEnv reads a boolean environment knob, case-insensitively: 1/t/true/y/yes/on turn it on,
// 0/f/false/n/no/off and unset leave it off.  Anything ELSE leaves it off and says so - a knob like
// BILOBA_UPDATE_SCREENSHOTS only does its work when it is set, so a spelling Biloba does not
// recognise has to be a visible no-op rather than a silent one.
func (b *Biloba) truthyEnv(name string) bool {
	value, set := os.LookupEnv(name)
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	case "", "0", "f", "false", "n", "no", "off":
		return false
	}
	if set {
		b.gt.Printf("%s is set to %q, which Biloba does not recognise as a boolean - treating it as off.  Use 1/true/yes/on to turn it on.\n", name, value)
	}
	return false
}

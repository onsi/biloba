package biloba

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

// These are plain testing-based units (not Ginkgo specs) because dot-importing Ginkgo into package
// biloba collides with biloba's own GinkgoTInterface - the same reason probe_trajectory_internal_test.go
// is written this way.  They exercise the pure pixel-diff engine: synthetic images in, a diagnosis
// out.  No browser, no filesystem.

var (
	vdWhite = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	vdBlack = color.NRGBA{0x00, 0x00, 0x00, 0xff}
	vdRed   = color.NRGBA{0xff, 0x00, 0x00, 0xff}
	vdGrey  = color.NRGBA{0x64, 0x64, 0x64, 0xff}
)

func vdImage(w, h int, fill color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = fill.R, fill.G, fill.B, fill.A
	}
	return img
}

// vdPattern is a deterministic pseudo-noise plane.  A shift search needs STRUCTURE to latch onto -
// on a flat image every offset scores the same - and the 7x+13y form has no small-offset period, so
// exactly one (dx,dy) in the searched range can ever realign it.
func vdPattern(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			v := uint8((7*x + 13*y) % 251)
			i := y*img.Stride + x*4
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = v, v, v, 0xff
		}
	}
	return img
}

func vdClone(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

func vdFill(img *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			i := y*img.Stride + x*4
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
		}
	}
}

func vdEncode(g *WithT, img image.Image) []byte {
	buf := &bytes.Buffer{}
	g.Expect(png.Encode(buf, img)).To(Succeed())
	return buf.Bytes()
}

func TestCompareScreenshotsIdentical(t *testing.T) {
	g := NewWithT(t)
	img := vdImage(40, 30, vdGrey)
	vdFill(img, image.Rect(5, 5, 20, 12), vdRed)
	data := vdEncode(g, img)

	d, err := compareScreenshots(data, data, screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.Match).To(BeTrue())
	g.Expect(d.DifferingPixels).To(Equal(0))
	g.Expect(d.MaxChannelDelta).To(Equal(0))
	g.Expect(d.TotalPixels).To(Equal(1200))
	g.Expect(d.Fraction).To(Equal(0.0))
	// a passing comparison skips clustering and rendering entirely - nothing consumes them
	g.Expect(d.Regions).To(BeEmpty())
	g.Expect(d.diff).To(BeNil())
}

func TestCompareScreenshotsSingleDifferingPixel(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(10, 10, vdGrey)
	act := vdClone(base)
	vdFill(act, image.Rect(5, 5, 6, 6), vdBlack)
	baseData, actData := vdEncode(g, base), vdEncode(g, act)

	// exact: one pixel is enough
	d, err := compareScreenshots(baseData, actData, screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.Match).To(BeFalse())
	g.Expect(d.DifferingPixels).To(Equal(1))
	g.Expect(d.Fraction).To(Equal(0.01))
	g.Expect(d.MaxChannelDelta).To(Equal(0x64))

	// a fraction tolerance that admits exactly that pixel
	d, err = compareScreenshots(baseData, actData, screenshotTolerance{fraction: 0.01})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.Match).To(BeTrue())
	g.Expect(d.DifferingPixels).To(Equal(1)) // still counted, just tolerated
}

func TestCompareScreenshotsChannelTolerance(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(20, 20, color.NRGBA{100, 100, 100, 255})
	act := vdImage(20, 20, color.NRGBA{105, 100, 100, 255})
	baseData, actData := vdEncode(g, base), vdEncode(g, act)

	// a delta of 5 is absorbed by a channel tolerance of 5 (strictly-greater-than is the rule)...
	d, err := compareScreenshots(baseData, actData, screenshotTolerance{channel: 5})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.Match).To(BeTrue())
	g.Expect(d.DifferingPixels).To(Equal(0))
	// ...but MaxChannelDelta still reports the worst pixel, tolerated or not
	g.Expect(d.MaxChannelDelta).To(Equal(5))

	// ...and one notch tighter it is a real difference
	d, err = compareScreenshots(baseData, actData, screenshotTolerance{channel: 4})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.Match).To(BeFalse())
	g.Expect(d.DifferingPixels).To(Equal(400))
	g.Expect(d.MaxChannelDelta).To(Equal(5))
	d.analyze()
	g.Expect(d.Shifted).To(BeFalse()) // a flat colour change is not a translation
}

func TestCompareScreenshotsDimensionMismatch(t *testing.T) {
	g := NewWithT(t)
	baseData := vdEncode(g, vdImage(80, 60, vdGrey))
	actData := vdEncode(g, vdImage(80, 64, vdGrey))

	d, err := compareScreenshots(baseData, actData, screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.DimensionMismatch).To(BeTrue())
	g.Expect(d.Match).To(BeFalse())
	g.Expect(d.BaselineBounds).To(Equal(image.Rect(0, 0, 80, 60)))
	g.Expect(d.ActualBounds).To(Equal(image.Rect(0, 0, 80, 64)))
	// no per-pixel work happened at all
	g.Expect(d.DifferingPixels).To(Equal(0))
	g.Expect(d.TotalPixels).To(Equal(0))
	g.Expect(d.Regions).To(BeEmpty())
	g.Expect(d.diff).To(BeNil())

	out := d.diagnose("rail", screenshotPaths{Baseline: "base.png"})
	g.Expect(out).To(ContainSubstring(`screenshot "rail" differs from baseline`))
	g.Expect(out).To(ContainSubstring("baseline is 80x60, actual is 80x64 (4px taller)"))
	g.Expect(out).NotTo(ContainSubstring("pixels differ"))

	// both axes, in words
	baseData = vdEncode(g, vdImage(80, 60, vdGrey))
	actData = vdEncode(g, vdImage(100, 50, vdGrey))
	d, err = compareScreenshots(baseData, actData, screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.dimensionLine()).To(Equal("baseline is 80x60, actual is 100x50 (10px shorter and 20px wider)"))
}

func TestCompareScreenshotsOneRegion(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(100, 100, vdWhite)
	act := vdClone(base)
	vdFill(act, image.Rect(20, 4, 50, 14), vdBlack)

	d, err := compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.Match).To(BeFalse())
	g.Expect(d.DifferingPixels).To(Equal(300))
	d.analyze()
	g.Expect(d.RegionCount).To(Equal(1))
	g.Expect(d.Regions).To(HaveLen(1))
	// the reported box is the EXACT pixel bounding box, not the 8px clustering grid
	g.Expect(d.Regions[0].Rect).To(Equal(image.Rect(20, 4, 50, 14)))
	g.Expect(d.Regions[0].Count).To(Equal(300))
	g.Expect(d.Shifted).To(BeFalse())
	g.Expect(d.Scattered).To(BeFalse())

	out := d.diagnose("librarian-rail", screenshotPaths{})
	g.Expect(out).To(ContainSubstring("300 of 10,000 pixels differ (3.00%), max channel delta 255"))
	g.Expect(out).To(ContainSubstring("changed region: one box, (20,4)-(50,14)"))
	g.Expect(out).To(ContainSubstring("30% of the width"))
	g.Expect(out).To(ContainSubstring("10% of the height"))
	g.Expect(out).To(ContainSubstring("at its top edge"))
	g.Expect(out).To(ContainSubstring("unchanged: everything below y=14"))
	g.Expect(out).To(HaveSuffix("\n"))
	g.Expect(out).NotTo(HaveSuffix("\n\n"))
}

func TestCompareScreenshotsSeparateRegions(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(200, 200, vdWhite)
	act := vdClone(base)
	vdFill(act, image.Rect(10, 10, 40, 40), vdBlack)   // 900 px
	vdFill(act, image.Rect(150, 150, 170, 160), vdRed) // 200 px

	d, err := compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	d.analyze()
	g.Expect(d.RegionCount).To(Equal(2))
	// largest-first
	g.Expect(d.Regions[0].Rect).To(Equal(image.Rect(10, 10, 40, 40)))
	g.Expect(d.Regions[1].Rect).To(Equal(image.Rect(150, 150, 170, 160)))

	out := d.diagnose("two", screenshotPaths{})
	g.Expect(out).To(ContainSubstring("changed regions: 2 boxes"))
	g.Expect(out).To(ContainSubstring("(10,10)-(40,40)  900 pixels"))
	g.Expect(out).To(ContainSubstring("(150,150)-(170,160)  200 pixels"))
	// the combined bbox reaches from the top-left to well past centre: nothing is confined
	g.Expect(out).NotTo(ContainSubstring("unchanged:"))
}

func TestCompareScreenshotsUniformShift(t *testing.T) {
	g := NewWithT(t)
	base := vdPattern(200, 200)
	// the whole image slides down one pixel; the exposed top row keeps its original content
	act := image.NewNRGBA(base.Bounds())
	for y := range 200 {
		src := y - 1
		if src < 0 {
			src = 0
		}
		copy(act.Pix[y*act.Stride:(y+1)*act.Stride], base.Pix[src*base.Stride:(src+1)*base.Stride])
	}

	d, err := compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.Match).To(BeFalse())
	g.Expect(d.Fraction).To(BeNumerically(">", 0.9)) // essentially every pixel "differs"
	d.analyze()
	g.Expect(d.Shifted).To(BeTrue())
	g.Expect(d.Shift).To(Equal(image.Pt(0, 1)))

	out := d.diagnose("rail", screenshotPaths{})
	g.Expect(out).To(ContainSubstring("changed region: uniform shift of the whole image, 1px down (dx=0, dy=1)"))
	g.Expect(out).To(ContainSubstring("something above or before it likely grew or moved"))
	// the giant single region that clustering found must not be what gets reported
	g.Expect(out).NotTo(ContainSubstring("one box"))
	g.Expect(out).NotTo(ContainSubstring("unchanged:"))
}

func TestCompareScreenshotsSmallImagesAreNeverAShift(t *testing.T) {
	g := NewWithT(t)
	// A capture only a few pixels thick has no room for a translation search: every candidate offset
	// slides most of it off the edge.  These four differ only in COLOUR - there is no shift to find -
	// and a 2px <hr>, a 3px focus ring or a 4px progress bar is a real thing to capture as an element.
	for _, dim := range [][2]int{{1, 1}, {4, 4}, {6, 6}, {300, 2}} {
		w, h := dim[0], dim[1]
		base := vdEncode(g, vdImage(w, h, color.NRGBA{0x20, 0x40, 0x60, 0xff}))
		act := vdEncode(g, vdImage(w, h, color.NRGBA{0xc0, 0x30, 0x10, 0xff}))

		d, err := compareScreenshots(base, act, screenshotTolerance{})
		g.Expect(err).NotTo(HaveOccurred())
		d.analyze()
		g.Expect(d.Match).To(BeFalse(), "%dx%d", w, h)
		g.Expect(d.Shifted).To(BeFalse(), "%dx%d reported a shift of %v", w, h, d.Shift)
		g.Expect(d.diagnose("thin", screenshotPaths{})).NotTo(ContainSubstring("uniform shift"), "%dx%d", w, h)
	}
}

func TestCompareScreenshotsAnalysisIsDeferred(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(200, 200, vdWhite)
	act := vdClone(base)
	vdFill(act, image.Rect(20, 20, 120, 120), vdBlack)

	d, err := compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	// the matcher's verdict is all compareScreenshots computes: a settling page fails this
	// comparison on every poll attempt, and none of them pay for the diagnosis
	g.Expect(d.Match).To(BeFalse())
	g.Expect(d.DifferingPixels).To(Equal(10000))
	g.Expect(d.Regions).To(BeEmpty())
	g.Expect(d.RegionCount).To(Equal(0))
	g.Expect(d.diff).To(BeNil())

	d.analyze()
	g.Expect(d.RegionCount).To(Equal(1))
	g.Expect(d.Regions[0].Rect).To(Equal(image.Rect(20, 20, 120, 120)))
	g.Expect(d.diff).NotTo(BeNil())
}

func TestCompareScreenshotsScattered(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(400, 400, vdWhite)
	act := vdClone(base)
	// 25 little blocks on a wide grid: every text run rendered a bit differently
	for j := range 5 {
		for i := range 5 {
			x, y := 10+i*90, 10+j*90
			vdFill(act, image.Rect(x, y, x+6, y+6), vdBlack)
		}
	}

	d, err := compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	d.analyze()
	g.Expect(d.RegionCount).To(Equal(25))
	g.Expect(d.Regions).To(HaveLen(maxReportedRegions)) // truncated for reporting, counted in full
	g.Expect(d.Scattered).To(BeTrue())
	g.Expect(d.Shifted).To(BeFalse())

	out := d.diagnose("rail", screenshotPaths{})
	g.Expect(out).To(ContainSubstring("changed region: scattered — 25 regions spread across the image"))
	g.Expect(out).To(ContainSubstring("a font likely failed to load or rendered differently"))
}

func TestCompareScreenshotsFewRegionsAreNotScattered(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(400, 400, vdWhite)
	act := vdClone(base)
	// widely spread, small - but only 4 of them.  Two components changing is not a font failure.
	for _, p := range [][2]int{{10, 10}, {380, 10}, {10, 380}, {380, 380}} {
		vdFill(act, image.Rect(p[0], p[1], p[0]+6, p[1]+6), vdBlack)
	}

	d, err := compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	d.analyze()
	g.Expect(d.RegionCount).To(Equal(4))
	g.Expect(d.Scattered).To(BeFalse())
}

func TestCompareScreenshotsLocalisedChangeIsNotAShift(t *testing.T) {
	g := NewWithT(t)
	// a big, structured image with one solid block dropped into the middle of it.  11% of the image
	// differs - well past the shift-search floor - but no translation explains it.
	base := vdPattern(600, 600)
	act := vdClone(base)
	vdFill(act, image.Rect(100, 100, 300, 300), vdBlack)

	d, err := compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	d.analyze()
	g.Expect(d.Fraction).To(BeNumerically(">", shiftMinFraction))
	g.Expect(d.Shifted).To(BeFalse())
	g.Expect(d.RegionCount).To(Equal(1))
	g.Expect(d.Regions[0].Rect).To(Equal(image.Rect(100, 100, 300, 300)))

	// and the same holds for a change too small to even trigger the search
	base = vdPattern(600, 600)
	act = vdClone(base)
	vdFill(act, image.Rect(10, 10, 30, 30), vdBlack)
	d, err = compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	d.analyze()
	g.Expect(d.Fraction).To(BeNumerically("<", shiftMinFraction))
	g.Expect(d.Shifted).To(BeFalse())
}

// This one is about the matcher rather than the diff engine, but it needs package-internal access
// and it must not be a Ginkgo spec, so it lives alongside the units.  Gomega registers a matcher's
// message generator as a Ginkgo progress reporter for the length of every Eventually, so Ginkgo
// calls FailureMessage from its own goroutine while match is still polling - which is exactly what
// this reproduces.  Run it under -race.
func TestScreenshotMatcherFailureMessageRacesMatch(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(60, 40, vdGrey)
	act := vdClone(base)
	vdFill(act, image.Rect(5, 5, 40, 30), vdRed)
	baseData, actData := vdEncode(g, base), vdEncode(g, act)

	tab := &Biloba{screenshotsDir: t.TempDir(), baselinesDir: t.TempDir()}
	tab.root = tab
	m := &screenshotMatcher{b: tab, name: "racy"}

	done := make(chan struct{})
	go func() { // stands in for the polling match
		defer close(done)
		for range 200 {
			d, err := compareScreenshots(baseData, actData, screenshotTolerance{})
			g.Expect(err).NotTo(HaveOccurred())
			m.record("", nil, nil)
			m.record("dark", d, actData)
		}
	}()
	for range 200 { // stands in for the progress reporter
		g.Expect(m.FailureMessage(nil)).NotTo(BeEmpty())
	}
	<-done
}

func TestMaskPNG(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(60, 40, vdGrey)
	act := vdClone(base)
	masked := image.Rect(10, 10, 30, 30)
	vdFill(act, masked, vdRed)
	baseData, actData := vdEncode(g, base), vdEncode(g, act)

	// unmasked, the two images differ
	d, err := compareScreenshots(baseData, actData, screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.Match).To(BeFalse())
	g.Expect(d.DifferingPixels).To(Equal(400))

	// masked identically on both sides, they are equal
	maskedBase, err := maskPNG(baseData, []image.Rectangle{masked})
	g.Expect(err).NotTo(HaveOccurred())
	maskedAct, err := maskPNG(actData, []image.Rectangle{masked})
	g.Expect(err).NotTo(HaveOccurred())

	d, err = compareScreenshots(maskedBase, maskedAct, screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.Match).To(BeTrue())
	g.Expect(d.DifferingPixels).To(Equal(0))
	// and the dimensions survive the round trip exactly
	g.Expect(d.ActualBounds).To(Equal(image.Rect(0, 0, 60, 40)))

	// the mask really is maskFillColor, not just "something equal on both sides"
	decoded, err := png.Decode(bytes.NewReader(maskedAct))
	g.Expect(err).NotTo(HaveOccurred())
	r, gr, b, a := decoded.At(15, 15).RGBA()
	g.Expect([4]uint8{uint8(r >> 8), uint8(gr >> 8), uint8(b >> 8), uint8(a >> 8)}).To(Equal(maskFillColor))

	// an out-of-bounds rect is a no-op rather than an error, and a straddling one is clipped
	off, err := maskPNG(baseData, []image.Rectangle{image.Rect(100, 100, 120, 120)})
	g.Expect(err).NotTo(HaveOccurred())
	d, err = compareScreenshots(baseData, off, screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.Match).To(BeTrue())

	clipped, err := maskPNG(baseData, []image.Rectangle{image.Rect(50, 30, 200, 200)})
	g.Expect(err).NotTo(HaveOccurred())
	d, err = compareScreenshots(baseData, clipped, screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.DimensionMismatch).To(BeFalse())
	g.Expect(d.ActualBounds).To(Equal(image.Rect(0, 0, 60, 40)))
	g.Expect(d.DifferingPixels).To(Equal(100)) // only the 10x10 that fell inside

	// nothing to mask
	same, err := maskPNG(baseData, nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(same).To(Equal(baseData))

	_, err = maskPNG([]byte("not a png"), []image.Rectangle{masked})
	g.Expect(err).To(HaveOccurred())
}

func TestEncodeDiffPNG(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(100, 100, vdWhite)
	act := vdClone(base)
	vdFill(act, image.Rect(20, 4, 50, 14), vdBlack)

	d, err := compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	data, err := d.encodeDiffPNG()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(data).NotTo(BeEmpty())

	decoded, err := png.Decode(bytes.NewReader(data))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(decoded.Bounds()).To(Equal(image.Rect(0, 0, 100, 100)))
	// changed pixels are magenta; untouched ones are washed out toward white
	r, gr, b, _ := decoded.At(25, 8).RGBA()
	g.Expect([3]uint8{uint8(r >> 8), uint8(gr >> 8), uint8(b >> 8)}).To(Equal([3]uint8{0xff, 0x00, 0xff}))
	r, gr, b, _ = decoded.At(0, 0).RGBA()
	g.Expect([3]uint8{uint8(r >> 8), uint8(gr >> 8), uint8(b >> 8)}).To(Equal([3]uint8{0xff, 0xff, 0xff}))

	// nothing to render on a dimension mismatch...
	d, err = compareScreenshots(vdEncode(g, vdImage(10, 10, vdGrey)), vdEncode(g, vdImage(10, 12, vdGrey)), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	data, err = d.encodeDiffPNG()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(data).To(BeNil())

	// ...or on a pass
	same := vdEncode(g, vdImage(10, 10, vdGrey))
	d, err = compareScreenshots(same, same, screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	data, err = d.encodeDiffPNG()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(data).To(BeNil())
}

func TestCompareScreenshotsDecodeErrors(t *testing.T) {
	g := NewWithT(t)
	valid := vdEncode(g, vdImage(4, 4, vdGrey))

	_, err := compareScreenshots([]byte("nope"), valid, screenshotTolerance{})
	g.Expect(err).To(MatchError(ContainSubstring("baseline")))

	_, err = compareScreenshots(valid, []byte("nope"), screenshotTolerance{})
	g.Expect(err).To(MatchError(ContainSubstring("actual")))
}

func TestDiagnosePaths(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(100, 100, vdWhite)
	act := vdClone(base)
	vdFill(act, image.Rect(20, 4, 50, 14), vdBlack)
	d, err := compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())

	out := d.diagnose("librarian-rail", screenshotPaths{
		Baseline: "e2e/screenshots/librarian-rail.png",
		Actual:   ".biloba-artifacts/librarian-rail.actual.png",
		Diff:     ".biloba-artifacts/librarian-rail.diff.png",
	})
	g.Expect(out).To(ContainSubstring("  baseline: e2e/screenshots/librarian-rail.png\n"))
	g.Expect(out).To(ContainSubstring("  actual:   .biloba-artifacts/librarian-rail.actual.png\n"))
	g.Expect(out).To(ContainSubstring("  diff:     .biloba-artifacts/librarian-rail.diff.png\n"))

	// an empty path drops its line entirely
	out = d.diagnose("librarian-rail", screenshotPaths{Baseline: "b.png", Diff: "d.png"})
	g.Expect(out).To(ContainSubstring("baseline: b.png"))
	g.Expect(out).To(ContainSubstring("diff:     d.png"))
	g.Expect(out).NotTo(ContainSubstring("actual:"))

	out = d.diagnose("librarian-rail", screenshotPaths{})
	g.Expect(out).NotTo(ContainSubstring("baseline:"))
	g.Expect(out).NotTo(ContainSubstring(".png"))
}

func TestScreenshotDiffSummary(t *testing.T) {
	g := NewWithT(t)

	// a brand new baseline: nothing was compared
	fresh := &screenshotDiff{ActualBounds: image.Rect(0, 0, 1200, 340)}
	g.Expect(fresh.summary("librarian-rail")).To(Equal(`screenshot "librarian-rail" written (new baseline, 1200x340)`))
	var missing *screenshotDiff
	g.Expect(missing.summary("librarian-rail")).To(Equal(`screenshot "librarian-rail" written (new baseline)`))

	base := vdImage(100, 100, vdWhite)
	act := vdClone(base)
	vdFill(act, image.Rect(20, 4, 50, 14), vdBlack)
	d, err := compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	line := d.summary("librarian-rail")
	g.Expect(line).To(Equal(`screenshot "librarian-rail" updated — 300 of 10,000 pixels changed (3.00%), one box at (20,4)-(50,14)`))
	g.Expect(line).NotTo(ContainSubstring("\n"))

	d, err = compareScreenshots(vdEncode(g, vdImage(80, 60, vdGrey)), vdEncode(g, vdImage(80, 64, vdGrey)), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.summary("rail")).To(Equal(`screenshot "rail" updated — resized from 80x60 to 80x64 (4px taller)`))

	same := vdEncode(g, vdImage(10, 10, vdGrey))
	d, err = compareScreenshots(same, same, screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(d.summary("rail")).To(Equal(`screenshot "rail" unchanged`))
}

func TestEdgeDescription(t *testing.T) {
	g := NewWithT(t)
	g.Expect(edgeDescription(image.Rect(10, 0, 90, 20), 100, 100)).To(Equal("top edge"))
	g.Expect(edgeDescription(image.Rect(10, 80, 90, 100), 100, 100)).To(Equal("bottom edge"))
	g.Expect(edgeDescription(image.Rect(0, 40, 30, 60), 100, 100)).To(Equal("left edge"))
	g.Expect(edgeDescription(image.Rect(70, 40, 100, 60), 100, 100)).To(Equal("right edge"))
	g.Expect(edgeDescription(image.Rect(0, 0, 20, 20), 100, 100)).To(Equal("top-left corner"))
	g.Expect(edgeDescription(image.Rect(80, 80, 100, 100), 100, 100)).To(Equal("bottom-right corner"))
	g.Expect(edgeDescription(image.Rect(30, 30, 70, 70), 100, 100)).To(Equal("centre"))
}

func TestShiftWords(t *testing.T) {
	g := NewWithT(t)
	g.Expect(shiftWords(image.Pt(0, 1))).To(Equal("1px down"))
	g.Expect(shiftWords(image.Pt(0, -3))).To(Equal("3px up"))
	g.Expect(shiftWords(image.Pt(2, 0))).To(Equal("2px right"))
	g.Expect(shiftWords(image.Pt(-2, 1))).To(Equal("2px left and 1px down"))
	g.Expect(shiftWords(image.Pt(2, 1))).To(Equal("2px right and 1px down"))
}

func TestWithThousands(t *testing.T) {
	g := NewWithT(t)
	g.Expect(withThousands(0)).To(Equal("0"))
	g.Expect(withThousands(7)).To(Equal("7"))
	g.Expect(withThousands(999)).To(Equal("999"))
	g.Expect(withThousands(1000)).To(Equal("1,000"))
	g.Expect(withThousands(1284)).To(Equal("1,284"))
	g.Expect(withThousands(342000)).To(Equal("342,000"))
	g.Expect(withThousands(1000000)).To(Equal("1,000,000"))
	g.Expect(withThousands(-1284)).To(Equal("-1,284"))
}

func TestDiagnoseTruncatesRegionList(t *testing.T) {
	g := NewWithT(t)
	base := vdImage(400, 100, vdWhite)
	act := vdClone(base)
	// eight separate boxes, all confined to the top band
	for i := range 8 {
		x := 10 + i*45
		vdFill(act, image.Rect(x, 5, x+20, 5+(8-i)*2), vdBlack)
	}
	d, err := compareScreenshots(vdEncode(g, base), vdEncode(g, act), screenshotTolerance{})
	g.Expect(err).NotTo(HaveOccurred())
	d.analyze()
	g.Expect(d.RegionCount).To(Equal(8))
	g.Expect(d.Regions).To(HaveLen(maxReportedRegions))

	out := d.diagnose("rail", screenshotPaths{})
	g.Expect(out).To(ContainSubstring("changed regions: 8 boxes"))
	g.Expect(out).To(ContainSubstring("… and 3 more"))
	// the region list was truncated, so the bbox is only a partial truth - stay quiet about it
	g.Expect(out).NotTo(ContainSubstring("unchanged:"))
	g.Expect(strings.Count(out, "pixels\n")).To(Equal(maxReportedRegions))
}

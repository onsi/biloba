package biloba

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// These specs live in package biloba (not biloba_test) because they exercise the pure pixel-diff
// engine's unexported types directly - synthetic images in, a diagnosis out.  No browser, no
// filesystem.  Ginkgo and Gomega are imported under their package names rather than dot-imported,
// matching tab_state_internal_test.go; see the note at the top of that file for why that's all it
// takes to avoid colliding with biloba's own exported names.

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

func vdEncode(img image.Image) []byte {
	buf := &bytes.Buffer{}
	gomega.Expect(png.Encode(buf, img)).To(gomega.Succeed())
	return buf.Bytes()
}

var _ = ginkgo.Describe("the visual regression diff engine", ginkgo.Label("no-browser"), func() {
	ginkgo.Describe("compareScreenshots", func() {
		ginkgo.It("reports a match with zero differing pixels when the images are identical", func() {
			img := vdImage(40, 30, vdGrey)
			vdFill(img, image.Rect(5, 5, 20, 12), vdRed)
			data := vdEncode(img)

			d, err := compareScreenshots(data, data, screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.Match).To(gomega.BeTrue())
			gomega.Expect(d.DifferingPixels).To(gomega.Equal(0))
			gomega.Expect(d.MaxChannelDelta).To(gomega.Equal(0))
			gomega.Expect(d.TotalPixels).To(gomega.Equal(1200))
			gomega.Expect(d.Fraction).To(gomega.Equal(0.0))
			// a passing comparison skips clustering and rendering entirely - nothing consumes them
			gomega.Expect(d.Regions).To(gomega.BeEmpty())
			gomega.Expect(d.diff).To(gomega.BeNil())
		})

		ginkgo.It("flags a single differing pixel exactly", func() {
			base := vdImage(10, 10, vdGrey)
			act := vdClone(base)
			vdFill(act, image.Rect(5, 5, 6, 6), vdBlack)
			baseData, actData := vdEncode(base), vdEncode(act)

			d, err := compareScreenshots(baseData, actData, screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.Match).To(gomega.BeFalse())
			gomega.Expect(d.DifferingPixels).To(gomega.Equal(1))
			gomega.Expect(d.Fraction).To(gomega.Equal(0.01))
			gomega.Expect(d.MaxChannelDelta).To(gomega.Equal(0x64))
		})

		ginkgo.It("tolerates a single differing pixel under a fraction tolerance that admits it", func() {
			base := vdImage(10, 10, vdGrey)
			act := vdClone(base)
			vdFill(act, image.Rect(5, 5, 6, 6), vdBlack)
			baseData, actData := vdEncode(base), vdEncode(act)

			d, err := compareScreenshots(baseData, actData, screenshotTolerance{fraction: 0.01})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.Match).To(gomega.BeTrue())
			gomega.Expect(d.DifferingPixels).To(gomega.Equal(1)) // still counted, just tolerated
		})

		ginkgo.It("absorbs a channel delta at or below the channel tolerance, and flags one above it", func() {
			base := vdImage(20, 20, color.NRGBA{100, 100, 100, 255})
			act := vdImage(20, 20, color.NRGBA{105, 100, 100, 255})
			baseData, actData := vdEncode(base), vdEncode(act)

			// a delta of 5 is absorbed by a channel tolerance of 5 (strictly-greater-than is the rule)...
			d, err := compareScreenshots(baseData, actData, screenshotTolerance{channel: 5})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.Match).To(gomega.BeTrue())
			gomega.Expect(d.DifferingPixels).To(gomega.Equal(0))
			// ...but MaxChannelDelta still reports the worst pixel, tolerated or not
			gomega.Expect(d.MaxChannelDelta).To(gomega.Equal(5))

			// ...and one notch tighter it is a real difference
			d, err = compareScreenshots(baseData, actData, screenshotTolerance{channel: 4})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.Match).To(gomega.BeFalse())
			gomega.Expect(d.DifferingPixels).To(gomega.Equal(400))
			gomega.Expect(d.MaxChannelDelta).To(gomega.Equal(5))
			d.analyze()
			gomega.Expect(d.Shifted).To(gomega.BeFalse()) // a flat colour change is not a translation
		})

		ginkgo.It("reports a dimension mismatch without doing any per-pixel work", func() {
			baseData := vdEncode(vdImage(80, 60, vdGrey))
			actData := vdEncode(vdImage(80, 64, vdGrey))

			d, err := compareScreenshots(baseData, actData, screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.DimensionMismatch).To(gomega.BeTrue())
			gomega.Expect(d.Match).To(gomega.BeFalse())
			gomega.Expect(d.BaselineBounds).To(gomega.Equal(image.Rect(0, 0, 80, 60)))
			gomega.Expect(d.ActualBounds).To(gomega.Equal(image.Rect(0, 0, 80, 64)))
			// no per-pixel work happened at all
			gomega.Expect(d.DifferingPixels).To(gomega.Equal(0))
			gomega.Expect(d.TotalPixels).To(gomega.Equal(0))
			gomega.Expect(d.Regions).To(gomega.BeEmpty())
			gomega.Expect(d.diff).To(gomega.BeNil())

			out := d.diagnose("rail", screenshotPaths{Baseline: "base.png"})
			gomega.Expect(out).To(gomega.ContainSubstring(`screenshot "rail" differs from baseline`))
			gomega.Expect(out).To(gomega.ContainSubstring("baseline is 80x60, actual is 80x64 (4px taller)"))
			gomega.Expect(out).NotTo(gomega.ContainSubstring("pixels differ"))

			// both axes, in words
			baseData = vdEncode(vdImage(80, 60, vdGrey))
			actData = vdEncode(vdImage(100, 50, vdGrey))
			d, err = compareScreenshots(baseData, actData, screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.dimensionLine()).To(gomega.Equal("baseline is 80x60, actual is 100x50 (10px shorter and 20px wider)"))
		})

		ginkgo.It("reports one changed region with its exact pixel bounding box", func() {
			base := vdImage(100, 100, vdWhite)
			act := vdClone(base)
			vdFill(act, image.Rect(20, 4, 50, 14), vdBlack)

			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.Match).To(gomega.BeFalse())
			gomega.Expect(d.DifferingPixels).To(gomega.Equal(300))
			d.analyze()
			gomega.Expect(d.RegionCount).To(gomega.Equal(1))
			gomega.Expect(d.Regions).To(gomega.HaveLen(1))
			// the reported box is the EXACT pixel bounding box, not the 8px clustering grid
			gomega.Expect(d.Regions[0].Rect).To(gomega.Equal(image.Rect(20, 4, 50, 14)))
			gomega.Expect(d.Regions[0].Count).To(gomega.Equal(300))
			gomega.Expect(d.Shifted).To(gomega.BeFalse())
			gomega.Expect(d.Scattered).To(gomega.BeFalse())

			out := d.diagnose("librarian-rail", screenshotPaths{})
			gomega.Expect(out).To(gomega.ContainSubstring("300 of 10,000 pixels differ (3.00%), max channel delta 255"))
			gomega.Expect(out).To(gomega.ContainSubstring("changed region: one box, (20,4)-(50,14)"))
			gomega.Expect(out).To(gomega.ContainSubstring("30% of the width"))
			gomega.Expect(out).To(gomega.ContainSubstring("10% of the height"))
			gomega.Expect(out).To(gomega.ContainSubstring("at its top edge"))
			gomega.Expect(out).To(gomega.ContainSubstring("unchanged: everything below y=14"))
			gomega.Expect(out).To(gomega.HaveSuffix("\n"))
			gomega.Expect(out).NotTo(gomega.HaveSuffix("\n\n"))
		})

		ginkgo.It("reports separate regions largest-first, without an unchanged-area claim", func() {
			base := vdImage(200, 200, vdWhite)
			act := vdClone(base)
			vdFill(act, image.Rect(10, 10, 40, 40), vdBlack)   // 900 px
			vdFill(act, image.Rect(150, 150, 170, 160), vdRed) // 200 px

			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			d.analyze()
			gomega.Expect(d.RegionCount).To(gomega.Equal(2))
			// largest-first
			gomega.Expect(d.Regions[0].Rect).To(gomega.Equal(image.Rect(10, 10, 40, 40)))
			gomega.Expect(d.Regions[1].Rect).To(gomega.Equal(image.Rect(150, 150, 170, 160)))

			out := d.diagnose("two", screenshotPaths{})
			gomega.Expect(out).To(gomega.ContainSubstring("changed regions: 2 boxes"))
			gomega.Expect(out).To(gomega.ContainSubstring("(10,10)-(40,40)  900 pixels"))
			gomega.Expect(out).To(gomega.ContainSubstring("(150,150)-(170,160)  200 pixels"))
			// the combined bbox reaches from the top-left to well past centre: nothing is confined
			gomega.Expect(out).NotTo(gomega.ContainSubstring("unchanged:"))
		})

		ginkgo.It("recognises a whole-image translation as a uniform shift", func() {
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

			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.Match).To(gomega.BeFalse())
			gomega.Expect(d.Fraction).To(gomega.BeNumerically(">", 0.9)) // essentially every pixel "differs"
			d.analyze()
			gomega.Expect(d.Shifted).To(gomega.BeTrue())
			gomega.Expect(d.Shift).To(gomega.Equal(image.Pt(0, 1)))

			out := d.diagnose("rail", screenshotPaths{})
			gomega.Expect(out).To(gomega.ContainSubstring("changed region: uniform shift of the whole image, 1px down (dx=0, dy=1)"))
			gomega.Expect(out).To(gomega.ContainSubstring("something above or before it likely grew or moved"))
			// the giant single region that clustering found must not be what gets reported
			gomega.Expect(out).NotTo(gomega.ContainSubstring("one box"))
			gomega.Expect(out).NotTo(gomega.ContainSubstring("unchanged:"))
		})

		ginkgo.DescribeTable("never calls a colour-only difference on a tiny image a shift",
			// A capture only a few pixels thick has no room for a translation search: every candidate
			// offset slides most of it off the edge.  These four differ only in COLOUR - there is no
			// shift to find - and a 2px <hr>, a 3px focus ring or a 4px progress bar is a real thing to
			// capture as an element.
			func(w, h int) {
				base := vdEncode(vdImage(w, h, color.NRGBA{0x20, 0x40, 0x60, 0xff}))
				act := vdEncode(vdImage(w, h, color.NRGBA{0xc0, 0x30, 0x10, 0xff}))

				d, err := compareScreenshots(base, act, screenshotTolerance{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				d.analyze()
				gomega.Expect(d.Match).To(gomega.BeFalse())
				gomega.Expect(d.Shifted).To(gomega.BeFalse(), "reported a shift of %v", d.Shift)
				gomega.Expect(d.diagnose("thin", screenshotPaths{})).NotTo(gomega.ContainSubstring("uniform shift"))
			},
			ginkgo.Entry("1x1", 1, 1),
			ginkgo.Entry("4x4", 4, 4),
			ginkgo.Entry("6x6", 6, 6),
			ginkgo.Entry("300x2", 300, 2),
		)

		ginkgo.It("defers region clustering and diff rendering until analyze is called", func() {
			base := vdImage(200, 200, vdWhite)
			act := vdClone(base)
			vdFill(act, image.Rect(20, 20, 120, 120), vdBlack)

			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			// the matcher's verdict is all compareScreenshots computes: a settling page fails this
			// comparison on every poll attempt, and none of them pay for the diagnosis
			gomega.Expect(d.Match).To(gomega.BeFalse())
			gomega.Expect(d.DifferingPixels).To(gomega.Equal(10000))
			gomega.Expect(d.Regions).To(gomega.BeEmpty())
			gomega.Expect(d.RegionCount).To(gomega.Equal(0))
			gomega.Expect(d.diff).To(gomega.BeNil())

			d.analyze()
			gomega.Expect(d.RegionCount).To(gomega.Equal(1))
			gomega.Expect(d.Regions[0].Rect).To(gomega.Equal(image.Rect(20, 20, 120, 120)))
			gomega.Expect(d.diff).NotTo(gomega.BeNil())
		})

		ginkgo.It("recognises many small scattered regions as a likely font-rendering difference", func() {
			base := vdImage(400, 400, vdWhite)
			act := vdClone(base)
			// 25 little blocks on a wide grid: every text run rendered a bit differently
			for j := range 5 {
				for i := range 5 {
					x, y := 10+i*90, 10+j*90
					vdFill(act, image.Rect(x, y, x+6, y+6), vdBlack)
				}
			}

			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			d.analyze()
			gomega.Expect(d.RegionCount).To(gomega.Equal(25))
			gomega.Expect(d.Regions).To(gomega.HaveLen(maxReportedRegions)) // truncated for reporting, counted in full
			gomega.Expect(d.Scattered).To(gomega.BeTrue())
			gomega.Expect(d.Shifted).To(gomega.BeFalse())

			out := d.diagnose("rail", screenshotPaths{})
			gomega.Expect(out).To(gomega.ContainSubstring("changed region: scattered — 25 regions spread across the image"))
			gomega.Expect(out).To(gomega.ContainSubstring("a font likely failed to load or rendered differently"))
		})

		ginkgo.It("does not call a handful of widely-spread regions scattered", func() {
			base := vdImage(400, 400, vdWhite)
			act := vdClone(base)
			// widely spread, small - but only 4 of them.  Two components changing is not a font failure.
			for _, p := range [][2]int{{10, 10}, {380, 10}, {10, 380}, {380, 380}} {
				vdFill(act, image.Rect(p[0], p[1], p[0]+6, p[1]+6), vdBlack)
			}

			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			d.analyze()
			gomega.Expect(d.RegionCount).To(gomega.Equal(4))
			gomega.Expect(d.Scattered).To(gomega.BeFalse())
		})

		ginkgo.It("does not call a large localised change a shift, however much of the image it covers", func() {
			// a big, structured image with one solid block dropped into the middle of it.  11% of the
			// image differs - well past the shift-search floor - but no translation explains it.
			base := vdPattern(600, 600)
			act := vdClone(base)
			vdFill(act, image.Rect(100, 100, 300, 300), vdBlack)

			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			d.analyze()
			gomega.Expect(d.Fraction).To(gomega.BeNumerically(">", shiftMinFraction))
			gomega.Expect(d.Shifted).To(gomega.BeFalse())
			gomega.Expect(d.RegionCount).To(gomega.Equal(1))
			gomega.Expect(d.Regions[0].Rect).To(gomega.Equal(image.Rect(100, 100, 300, 300)))

			// and the same holds for a change too small to even trigger the search
			base = vdPattern(600, 600)
			act = vdClone(base)
			vdFill(act, image.Rect(10, 10, 30, 30), vdBlack)
			d, err = compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			d.analyze()
			gomega.Expect(d.Fraction).To(gomega.BeNumerically("<", shiftMinFraction))
			gomega.Expect(d.Shifted).To(gomega.BeFalse())
		})

		ginkgo.It("errors when either image fails to decode, naming which side", func() {
			valid := vdEncode(vdImage(4, 4, vdGrey))

			_, err := compareScreenshots([]byte("nope"), valid, screenshotTolerance{})
			gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("baseline")))

			_, err = compareScreenshots(valid, []byte("nope"), screenshotTolerance{})
			gomega.Expect(err).To(gomega.MatchError(gomega.ContainSubstring("actual")))
		})
	})

	// This one is about the matcher rather than the diff engine, but it needs package-internal access
	// and it must run under -race, which is why it lives alongside the diff-engine units rather than as
	// its own file.  Gomega registers a matcher's message generator as a Ginkgo progress reporter for
	// the length of every Eventually, so Ginkgo calls FailureMessage from its own goroutine while match
	// is still polling - which is exactly what this reproduces.
	ginkgo.It("lets FailureMessage race a concurrent match without a data race", func() {
		base := vdImage(60, 40, vdGrey)
		act := vdClone(base)
		vdFill(act, image.Rect(5, 5, 40, 30), vdRed)
		baseData, actData := vdEncode(base), vdEncode(act)

		tab := &Biloba{screenshotsDir: ginkgo.GinkgoT().TempDir(), baselinesDir: ginkgo.GinkgoT().TempDir()}
		tab.root = tab
		m := &screenshotMatcher{b: tab, name: "racy"}

		done := make(chan struct{})
		go func() { // stands in for the polling match
			defer close(done)
			for range 200 {
				d, err := compareScreenshots(baseData, actData, screenshotTolerance{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				m.record("", nil, nil)
				m.record("dark", d, actData)
			}
		}()
		for range 200 { // stands in for the progress reporter
			gomega.Expect(m.FailureMessage(nil)).NotTo(gomega.BeEmpty())
		}
		<-done
	})

	ginkgo.Describe("maskPNG", func() {
		ginkgo.It("makes a masked region compare equal even when the unmasked images differ", func() {
			base := vdImage(60, 40, vdGrey)
			act := vdClone(base)
			masked := image.Rect(10, 10, 30, 30)
			vdFill(act, masked, vdRed)
			baseData, actData := vdEncode(base), vdEncode(act)

			// unmasked, the two images differ
			d, err := compareScreenshots(baseData, actData, screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.Match).To(gomega.BeFalse())
			gomega.Expect(d.DifferingPixels).To(gomega.Equal(400))

			// masked identically on both sides, they are equal
			maskedBase, err := maskPNG(baseData, []image.Rectangle{masked})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			maskedAct, err := maskPNG(actData, []image.Rectangle{masked})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			d, err = compareScreenshots(maskedBase, maskedAct, screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.Match).To(gomega.BeTrue())
			gomega.Expect(d.DifferingPixels).To(gomega.Equal(0))
			// and the dimensions survive the round trip exactly
			gomega.Expect(d.ActualBounds).To(gomega.Equal(image.Rect(0, 0, 60, 40)))

			// the mask really is opaque mid-grey, not just "something equal on both sides".  The
			// literal is spelled out rather than compared against maskFillColor: both sides would
			// move together, so changing the constant - to a transparent colour, say - would leave
			// this passing while claiming to have checked exactly that.
			decoded, err := png.Decode(bytes.NewReader(maskedAct))
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			r, gr, b, a := decoded.At(15, 15).RGBA()
			gomega.Expect([4]uint8{uint8(r >> 8), uint8(gr >> 8), uint8(b >> 8), uint8(a >> 8)}).To(gomega.Equal([4]uint8{0x80, 0x80, 0x80, 0xff}))
		})

		ginkgo.It("treats an out-of-bounds mask rect as a no-op and clips a straddling one", func() {
			baseData := vdEncode(vdImage(60, 40, vdGrey))

			off, err := maskPNG(baseData, []image.Rectangle{image.Rect(100, 100, 120, 120)})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			d, err := compareScreenshots(baseData, off, screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.Match).To(gomega.BeTrue())

			clipped, err := maskPNG(baseData, []image.Rectangle{image.Rect(50, 30, 200, 200)})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			d, err = compareScreenshots(baseData, clipped, screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.DimensionMismatch).To(gomega.BeFalse())
			gomega.Expect(d.ActualBounds).To(gomega.Equal(image.Rect(0, 0, 60, 40)))
			gomega.Expect(d.DifferingPixels).To(gomega.Equal(100)) // only the 10x10 that fell inside
		})

		ginkgo.It("passes the image through unchanged when there is nothing to mask, and errors on bad PNG data", func() {
			baseData := vdEncode(vdImage(60, 40, vdGrey))

			same, err := maskPNG(baseData, nil)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(same).To(gomega.Equal(baseData))

			_, err = maskPNG([]byte("not a png"), []image.Rectangle{image.Rect(10, 10, 30, 30)})
			gomega.Expect(err).To(gomega.HaveOccurred())
		})
	})

	ginkgo.Describe("encodeDiffPNG", func() {
		ginkgo.It("colours changed pixels magenta and washes out untouched ones toward white", func() {
			base := vdImage(100, 100, vdWhite)
			act := vdClone(base)
			vdFill(act, image.Rect(20, 4, 50, 14), vdBlack)

			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			data, err := d.encodeDiffPNG()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(data).NotTo(gomega.BeEmpty())

			decoded, err := png.Decode(bytes.NewReader(data))
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(decoded.Bounds()).To(gomega.Equal(image.Rect(0, 0, 100, 100)))
			// changed pixels are magenta; untouched ones are washed out toward white
			r, gr, b, _ := decoded.At(25, 8).RGBA()
			gomega.Expect([3]uint8{uint8(r >> 8), uint8(gr >> 8), uint8(b >> 8)}).To(gomega.Equal([3]uint8{0xff, 0x00, 0xff}))
			r, gr, b, _ = decoded.At(0, 0).RGBA()
			gomega.Expect([3]uint8{uint8(r >> 8), uint8(gr >> 8), uint8(b >> 8)}).To(gomega.Equal([3]uint8{0xff, 0xff, 0xff}))
		})

		ginkgo.It("renders nothing on a dimension mismatch or a pass", func() {
			// nothing to render on a dimension mismatch...
			d, err := compareScreenshots(vdEncode(vdImage(10, 10, vdGrey)), vdEncode(vdImage(10, 12, vdGrey)), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			data, err := d.encodeDiffPNG()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(data).To(gomega.BeNil())

			// ...or on a pass
			same := vdEncode(vdImage(10, 10, vdGrey))
			d, err = compareScreenshots(same, same, screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			data, err = d.encodeDiffPNG()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(data).To(gomega.BeNil())
		})
	})

	ginkgo.Describe("diagnose", func() {
		ginkgo.It("prints only the artifact paths that were actually given", func() {
			base := vdImage(100, 100, vdWhite)
			act := vdClone(base)
			vdFill(act, image.Rect(20, 4, 50, 14), vdBlack)
			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			out := d.diagnose("librarian-rail", screenshotPaths{
				Baseline: "e2e/screenshots/librarian-rail.png",
				Actual:   ".biloba-artifacts/librarian-rail.actual.png",
				Diff:     ".biloba-artifacts/librarian-rail.diff.png",
			})
			gomega.Expect(out).To(gomega.ContainSubstring("  baseline: e2e/screenshots/librarian-rail.png\n"))
			gomega.Expect(out).To(gomega.ContainSubstring("  actual:   .biloba-artifacts/librarian-rail.actual.png\n"))
			gomega.Expect(out).To(gomega.ContainSubstring("  diff:     .biloba-artifacts/librarian-rail.diff.png\n"))

			// an empty path drops its line entirely
			out = d.diagnose("librarian-rail", screenshotPaths{Baseline: "b.png", Diff: "d.png"})
			gomega.Expect(out).To(gomega.ContainSubstring("baseline: b.png"))
			gomega.Expect(out).To(gomega.ContainSubstring("diff:     d.png"))
			gomega.Expect(out).NotTo(gomega.ContainSubstring("actual:"))

			out = d.diagnose("librarian-rail", screenshotPaths{})
			gomega.Expect(out).NotTo(gomega.ContainSubstring("baseline:"))
			gomega.Expect(out).NotTo(gomega.ContainSubstring(".png"))
		})

		ginkgo.It("truncates a long region list for reporting while still counting all of them", func() {
			base := vdImage(400, 100, vdWhite)
			act := vdClone(base)
			// eight separate boxes, all confined to the top band
			for i := range 8 {
				x := 10 + i*45
				vdFill(act, image.Rect(x, 5, x+20, 5+(8-i)*2), vdBlack)
			}
			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			d.analyze()
			gomega.Expect(d.RegionCount).To(gomega.Equal(8))
			gomega.Expect(d.Regions).To(gomega.HaveLen(maxReportedRegions))

			out := d.diagnose("rail", screenshotPaths{})
			gomega.Expect(out).To(gomega.ContainSubstring("changed regions: 8 boxes"))
			gomega.Expect(out).To(gomega.ContainSubstring("… and 3 more"))
			// the region list was truncated, so the bbox is only a partial truth - stay quiet about it
			gomega.Expect(out).NotTo(gomega.ContainSubstring("unchanged:"))
			gomega.Expect(strings.Count(out, "pixels\n")).To(gomega.Equal(maxReportedRegions))
		})

		// A max channel delta in the low single digits is a verdict, not a statistic: it says no pixel
		// changed meaningfully, so the cause is the renderer rather than the page.  Getting that wrong
		// costs an hour of hunting for an element that never moved.
		ginkgo.It("calls out a low max-channel-delta as rasterisation rather than content change", func() {
			// a soft band that differs by 3 on every channel - a shadow compositing into a clipped capture
			base := vdImage(200, 100, vdGrey)
			act := vdClone(base)
			vdFill(act, image.Rect(0, 90, 200, 96), color.NRGBA{0x67, 0x67, 0x67, 0xff})
			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.MaxChannelDelta).To(gomega.Equal(3))

			out := d.diagnose("choropleth", screenshotPaths{})
			gomega.Expect(out).To(gomega.ContainSubstring("every differing pixel differs by <= 3 — a rasterisation or compositing difference, not a content change"))
			gomega.Expect(out).To(gomega.ContainSubstring("b.ChannelTolerance(3)"))
			// the geometry is still reported: the verdict narrows the cause, it does not replace the location
			gomega.Expect(out).To(gomega.ContainSubstring("changed region: one box"))
		})

		ginkgo.It("does not call a real content change a rasterisation difference", func() {
			base := vdImage(200, 100, vdGrey)
			act := vdClone(base)
			vdFill(act, image.Rect(0, 90, 200, 96), vdBlack)
			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.diagnose("choropleth", screenshotPaths{})).NotTo(gomega.ContainSubstring("rasterisation"))
		})

		ginkgo.DescribeTable("draws the rasterisation-verdict boundary as a boundary, not a gradient",
			func(delta int, expectRasterisation bool) {
				base := vdImage(200, 100, vdGrey)
				act := vdClone(base)
				shade := uint8(int(vdGrey.R) + delta)
				vdFill(act, image.Rect(0, 90, 200, 96), color.NRGBA{shade, shade, shade, 0xff})
				d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				gomega.Expect(strings.Contains(d.diagnose("x", screenshotPaths{}), "rasterisation")).To(gomega.Equal(expectRasterisation))
			},
			ginkgo.Entry("at the rasterisation threshold", rasterizationChannelDelta, true),
			ginkgo.Entry("one past the rasterisation threshold", rasterizationChannelDelta+1, false),
		)
	})

	ginkgo.Describe("summary", func() {
		ginkgo.It("describes a brand new baseline, with or without known bounds", func() {
			fresh := &screenshotDiff{ActualBounds: image.Rect(0, 0, 1200, 340)}
			gomega.Expect(fresh.summary("librarian-rail")).To(gomega.Equal(`screenshot "librarian-rail" written (new baseline, 1200x340)`))
			var missing *screenshotDiff
			gomega.Expect(missing.summary("librarian-rail")).To(gomega.Equal(`screenshot "librarian-rail" written (new baseline)`))
		})

		ginkgo.It("summarizes an updated screenshot's pixel change on one line", func() {
			base := vdImage(100, 100, vdWhite)
			act := vdClone(base)
			vdFill(act, image.Rect(20, 4, 50, 14), vdBlack)
			d, err := compareScreenshots(vdEncode(base), vdEncode(act), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			line := d.summary("librarian-rail")
			gomega.Expect(line).To(gomega.Equal(`screenshot "librarian-rail" updated — 300 of 10,000 pixels changed (3.00%), one box at (20,4)-(50,14)`))
			gomega.Expect(line).NotTo(gomega.ContainSubstring("\n"))
		})

		ginkgo.It("summarizes a resize", func() {
			d, err := compareScreenshots(vdEncode(vdImage(80, 60, vdGrey)), vdEncode(vdImage(80, 64, vdGrey)), screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.summary("rail")).To(gomega.Equal(`screenshot "rail" updated — resized from 80x60 to 80x64 (4px taller)`))
		})

		ginkgo.It("summarizes an unchanged screenshot", func() {
			same := vdEncode(vdImage(10, 10, vdGrey))
			d, err := compareScreenshots(same, same, screenshotTolerance{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(d.summary("rail")).To(gomega.Equal(`screenshot "rail" unchanged`))
		})
	})

	ginkgo.DescribeTable("edgeDescription",
		func(rect image.Rectangle, w, h int, expected string) {
			gomega.Expect(edgeDescription(rect, w, h)).To(gomega.Equal(expected))
		},
		ginkgo.Entry("top edge", image.Rect(10, 0, 90, 20), 100, 100, "top edge"),
		ginkgo.Entry("bottom edge", image.Rect(10, 80, 90, 100), 100, 100, "bottom edge"),
		ginkgo.Entry("left edge", image.Rect(0, 40, 30, 60), 100, 100, "left edge"),
		ginkgo.Entry("right edge", image.Rect(70, 40, 100, 60), 100, 100, "right edge"),
		ginkgo.Entry("top-left corner", image.Rect(0, 0, 20, 20), 100, 100, "top-left corner"),
		ginkgo.Entry("bottom-right corner", image.Rect(80, 80, 100, 100), 100, 100, "bottom-right corner"),
		ginkgo.Entry("centre", image.Rect(30, 30, 70, 70), 100, 100, "centre"),
	)

	ginkgo.DescribeTable("shiftWords",
		func(pt image.Point, expected string) {
			gomega.Expect(shiftWords(pt)).To(gomega.Equal(expected))
		},
		ginkgo.Entry("down", image.Pt(0, 1), "1px down"),
		ginkgo.Entry("up", image.Pt(0, -3), "3px up"),
		ginkgo.Entry("right", image.Pt(2, 0), "2px right"),
		ginkgo.Entry("left and down", image.Pt(-2, 1), "2px left and 1px down"),
		ginkgo.Entry("right and down", image.Pt(2, 1), "2px right and 1px down"),
	)

	ginkgo.DescribeTable("withThousands",
		func(n int, expected string) {
			gomega.Expect(withThousands(n)).To(gomega.Equal(expected))
		},
		ginkgo.Entry("zero", 0, "0"),
		ginkgo.Entry("single digit", 7, "7"),
		ginkgo.Entry("just under a thousand", 999, "999"),
		ginkgo.Entry("exactly a thousand", 1000, "1,000"),
		ginkgo.Entry("four digits", 1284, "1,284"),
		ginkgo.Entry("six digits", 342000, "342,000"),
		ginkgo.Entry("seven digits", 1000000, "1,000,000"),
		ginkgo.Entry("negative", -1284, "-1,284"),
	)
})

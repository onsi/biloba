package biloba

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"sort"
	"strings"
	"sync"
)

// maskFillColor is the flat colour painted over a masked region.  It is opaque and mid-grey so a
// mask is obvious to a human reading the artifact, and - because the SAME colour is painted into
// both the baseline and the actual - the region always compares equal.
var maskFillColor = [4]uint8{0x80, 0x80, 0x80, 0xff}

// The tunables behind the diagnosis.  Every one of them exists to keep a verdict CONSERVATIVE: a
// wrong verdict ("a font failed to load") sends someone down the wrong path, which is strictly worse
// than the plain box list they would otherwise have read for themselves.
const (
	// maxReportedRegions bounds how many changed boxes the diagnosis prints.  RegionCount always
	// carries the true total; the list is only ever an illustration of it.
	maxReportedRegions = 5

	// diffGridCell is the coarse clustering grid, in pixels.  Clustering on 8x8 cells rather than on
	// pixels keeps union-find work at 1/64th of the image while still separating boxes that are more
	// than a cell apart - which is every gap that matters at UI scale.
	diffGridCell = 8

	// maxShiftOffset bounds the translation search to +/- 4px on each axis.  A whole-image shift
	// caused by something above growing is a handful of pixels; a bigger displacement is a layout
	// change, not a shift, and calling it one would be the wrong diagnosis.
	maxShiftOffset = 4

	// minShiftDimensionMultiple is how many times maxShiftOffset each axis has to exceed before a
	// translation search says anything about the page.  On an image only a few pixels thick every
	// candidate offset slides most of it off the edge, so the winner describes the search space and
	// not the rendering: a 2px <hr>, a 3px focus ring or a 4px progress bar captured as an element
	// would otherwise get a confident whole-image-shift verdict computed from a handful of pixels.
	minShiftDimensionMultiple = 4

	// shiftSearchWindow caps the centred crop the offset search runs over.  81 candidate offsets over
	// a full-page screenshot would be far more work than the diff itself; a 400x400 window is plenty
	// to pick the right offset, and the winner is then verified at full resolution anyway.
	shiftSearchWindow = 400

	// shiftMinFraction is the floor below which a shift is not what happened.  If only 1% of the
	// image moved, it was a component, not the page - so the search is skipped entirely (it is also
	// the expensive path, and this keeps it off the common "one small box changed" case).
	shiftMinFraction = 0.02

	// shiftResidualRatio is how much of the difference the shift has to explain: applying the winning
	// offset must leave under 20% of the original differing pixels.  A shift that only explains half
	// the change is a shift PLUS something else, and the something else is the interesting part.
	shiftResidualRatio = 0.2

	// The scattered ("a font failed to load") signature: many regions, each small, spread wide.  All
	// three have to hold.  Many small regions clumped together is a spinner; a few wide-apart regions
	// is two components changing.  Only the conjunction means "every text run moved a little".
	scatteredMinRegions    = 12
	scatteredMaxRegionArea = 0.01 // of the image, per reported region
	scatteredMinSpan       = 0.5  // of the image's width AND height, across all regions

	// confinedUntouchedFraction is how much of the image has to be clean on one side before the
	// complementary "unchanged: everything below y=N" line is worth printing.  The line is derived
	// from the bounding box so it is always true; the threshold is only about whether it is useful.
	confinedUntouchedFraction = 0.5
)

// screenshotTolerance is the resolved tolerance for one comparison.  The two axes are independent
// and both must be satisfied for a match:
//
//   - channel: a pixel counts as differing only if some R/G/B/A channel differs by MORE than this.
//     This is the antialiasing/subpixel-rendering absorber.  0 means exact.
//   - fraction: at most this fraction (0..1) of the compared pixels may differ.  0 means exact.
type screenshotTolerance struct {
	channel  int
	fraction float64
}

// screenshotRegion is one clustered run of changed pixels, in image coordinates.  Bounds are
// inclusive of Min and exclusive of Max, matching image.Rectangle.
type screenshotRegion struct {
	Rect  image.Rectangle
	Count int // differing pixels inside Rect
}

// screenshotDiff is the full result of comparing a baseline against a freshly captured actual.  It
// carries everything the §17 diagnosis needs; nothing about it touches the filesystem.
type screenshotDiff struct {
	Match bool // did the comparison pass under the supplied tolerance?

	BaselineBounds image.Rectangle
	ActualBounds   image.Rectangle
	// DimensionMismatch short-circuits everything else: when the two images are different sizes
	// there is no meaningful per-pixel comparison, so DifferingPixels et al are left zero and the
	// diagnosis reports the size change alone.
	DimensionMismatch bool

	TotalPixels     int
	DifferingPixels int
	Fraction        float64
	MaxChannelDelta int

	// Regions are the clustered changed areas, largest-first.  Truncated to maxReportedRegions;
	// RegionCount is the true total before truncation.
	Regions     []screenshotRegion
	RegionCount int

	// Shift is the (dx, dy) whole-image translation that best explains the difference, and Shifted
	// reports whether one was found.  "Everything moved down one pixel" is a completely different
	// bug from "one box changed", and it is the bounding boxes alone that cannot tell them apart.
	Shifted bool
	Shift   image.Point

	// Scattered reports the many-small-regions-spread-widely signature: a font that failed to load
	// or rendered differently, rather than a localised layout or colour change.
	Scattered bool

	// diff is the rendered diff image: the actual, desaturated and dimmed, with every differing
	// pixel painted in a high-contrast highlight.  nil until analyze() runs, and on a dimension
	// mismatch.
	diff image.Image

	// Everything below Regions is produced by analyze(), not by compareScreenshots: base, act, mask
	// and tolChannel are the inputs it needs, retained by a failing comparison so the expensive
	// diagnosis can be deferred to the one attempt that actually reports.
	base, act   *image.NRGBA
	mask        *pixelMask
	tolChannel  int
	analyzeOnce sync.Once
}

// compareScreenshots decodes two PNGs and diffs them under tol.  It returns an error only when a
// PNG fails to decode; a legitimate mismatch comes back as a *screenshotDiff with Match false.
//
// It computes only what the matcher needs to decide pass/fail - the dimensions, the differing count
// and fraction, the worst channel delta - and retains the decoded images and the differing-pixel
// mask so [screenshotDiff.analyze] can produce the diagnosis later.  That split matters because a
// failing comparison is the COMMON case while a page settles: this runs on every poll attempt, and
// the clustering, shift search and diff render only run on the attempt that reports.
func compareScreenshots(baseline []byte, actual []byte, tol screenshotTolerance) (*screenshotDiff, error) {
	base, err := decodeToNRGBA(baseline)
	if err != nil {
		return nil, fmt.Errorf("could not decode the baseline screenshot: %w", err)
	}
	act, err := decodeToNRGBA(actual)
	if err != nil {
		return nil, fmt.Errorf("could not decode the actual screenshot: %w", err)
	}

	d := &screenshotDiff{BaselineBounds: base.Bounds(), ActualBounds: act.Bounds()}
	w, h := base.Bounds().Dx(), base.Bounds().Dy()
	if w != act.Bounds().Dx() || h != act.Bounds().Dy() {
		// Different sizes: every per-pixel number below would be meaningless (and expensive), and
		// the size change IS the diagnosis.  Stop here.
		d.DimensionMismatch = true
		return d, nil
	}
	d.TotalPixels = w * h

	// The per-pixel pass.  Both images are normalised to *image.NRGBA above precisely so this is a
	// slice index rather than an At()/RGBA() interface call per pixel - full-page screenshots run to
	// several megapixels and this pass happens on every poll attempt, passing or failing.
	mask := newPixelMask(w * h)
	maxDelta, differing := 0, 0
	for y := range h {
		brow, arow := y*base.Stride, y*act.Stride
		for x := range w {
			i, j := brow+x*4, arow+x*4
			delta := 0
			for c := range 4 {
				dv := int(base.Pix[i+c]) - int(act.Pix[j+c])
				if dv < 0 {
					dv = -dv
				}
				if dv > delta {
					delta = dv
				}
			}
			if delta > maxDelta {
				maxDelta = delta
			}
			if delta > tol.channel {
				mask.set(y*w + x)
				differing++
			}
		}
	}

	d.DifferingPixels = differing
	// MaxChannelDelta is deliberately the max over ALL pixels, including the ones the channel
	// tolerance absorbed: it answers "how bad is the worst pixel", which is how you tell "the
	// tolerance is doing its job" from "the tolerance is one notch away from hiding a real change".
	d.MaxChannelDelta = maxDelta
	if d.TotalPixels > 0 {
		d.Fraction = float64(differing) / float64(d.TotalPixels)
	}
	d.Match = differing == 0 || d.Fraction <= tol.fraction
	if d.Match {
		// Nothing downstream consumes regions, shifts or the rendered diff on a pass, and this is the
		// path every successful poll attempt takes.  Keep nothing alive.
		return d, nil
	}
	d.base, d.act, d.mask, d.tolChannel = base, act, mask, tol.channel
	return d, nil
}

// analyze produces the diagnosis: the clustered regions, the shift and scattered verdicts, and the
// rendered diff image.  It is separate from compareScreenshots because it is the expensive half -
// on a full-page capture it roughly doubles the cost of a failing comparison - and it is only ever
// needed by the ONE attempt that reports, not by the dozens a settling page fails.
//
// Idempotent and safe to call concurrently: FailureMessage runs on Ginkgo's progress-reporter
// goroutine as well as on the failing assertion's, so a second caller waits for the first rather
// than racing it.
func (d *screenshotDiff) analyze() {
	if d == nil {
		return
	}
	d.analyzeOnce.Do(func() {
		if d.Match || d.DimensionMismatch || d.mask == nil {
			return
		}
		w, h := d.base.Bounds().Dx(), d.base.Bounds().Dy()
		regions, count, combined := clusterRegions(d.mask, w, h)
		d.Regions, d.RegionCount = regions, count
		d.Scattered = isScattered(regions, count, combined, w, h)
		if shift, ok := detectShift(d.base, d.act, d.Fraction, d.DifferingPixels, d.tolChannel); ok {
			d.Shifted, d.Shift = true, shift
		}
		d.diff = renderDiffImage(d.act, d.mask)
	})
}

// decodeToNRGBA decodes a PNG and normalises it to a zero-origin *image.NRGBA with a tightly packed
// stride, so every caller below can index Pix directly.  image/png hands back *image.RGBA for
// truecolour PNGs and *image.NRGBA for truecolour-with-alpha, among others; converting once here
// costs one copy and saves a per-pixel colour-model conversion in three separate passes.
func decodeToNRGBA(data []byte) (*image.NRGBA, error) {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := src.Bounds()
	if n, ok := src.(*image.NRGBA); ok && n.Rect.Min == (image.Point{}) && n.Stride == 4*bounds.Dx() {
		return n, nil
	}
	dst := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
	return dst, nil
}

// pixelMask is a one-bit-per-pixel record of which pixels differ.  A []bool would cost 8x the memory
// for the same information and this is allocated on every comparison, including the passing ones.
type pixelMask struct{ bits []uint64 }

func newPixelMask(n int) *pixelMask { return &pixelMask{bits: make([]uint64, (n+63)/64)} }
func (m *pixelMask) set(i int)      { m.bits[i>>6] |= 1 << uint(i&63) }
func (m *pixelMask) get(i int) bool { return m.bits[i>>6]&(1<<uint(i&63)) != 0 }

// clusterRegions groups the differing pixels into connected changed areas and returns them
// largest-first, along with the true component count and the bounding box that encloses all of them.
//
// The grid is the whole trick.  Union-find over 8x8 cells rather than over pixels cuts the work by
// 64x, and the coarseness is a feature rather than an approximation: at UI scale, two changed things
// less than 8px apart are one changed thing.  Each component's reported Rect is then refined back
// down to the EXACT bounding box of the differing pixels inside it, so the coordinates a human reads
// are real pixel coordinates and not snapped to the grid.
func clusterRegions(mask *pixelMask, w, h int) ([]screenshotRegion, int, image.Rectangle) {
	cols, rows := (w+diffGridCell-1)/diffGridCell, (h+diffGridCell-1)/diffGridCell
	marked := make([]bool, cols*rows)
	for y := range h {
		for x := range w {
			if mask.get(y*w + x) {
				marked[(y/diffGridCell)*cols+(x/diffGridCell)] = true
			}
		}
	}

	parent := make([]int, cols*rows)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]] // path halving
			i = parent[i]
		}
		return i
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	// 8-connectivity, expressed as four forward neighbours so each edge is visited once.  Diagonal
	// connectivity matters here: a one-pixel-wide diagonal change (an underline, a border radius)
	// would otherwise shatter into a dozen "regions".
	for cy := range rows {
		for cx := range cols {
			if !marked[cy*cols+cx] {
				continue
			}
			for _, n := range [4][2]int{{cx + 1, cy}, {cx - 1, cy + 1}, {cx, cy + 1}, {cx + 1, cy + 1}} {
				nx, ny := n[0], n[1]
				if nx < 0 || nx >= cols || ny >= rows {
					continue
				}
				if marked[ny*cols+nx] {
					union(cy*cols+cx, ny*cols+nx)
				}
			}
		}
	}

	type accum struct {
		rect  image.Rectangle
		count int
	}
	byRoot := map[int]*accum{}
	for y := range h {
		for x := range w {
			if !mask.get(y*w + x) {
				continue
			}
			root := find((y/diffGridCell)*cols + (x / diffGridCell))
			a := byRoot[root]
			if a == nil {
				a = &accum{rect: image.Rect(x, y, x+1, y+1)}
				byRoot[root] = a
			} else {
				a.rect = a.rect.Union(image.Rect(x, y, x+1, y+1))
			}
			a.count++
		}
	}

	regions := make([]screenshotRegion, 0, len(byRoot))
	combined := image.Rectangle{}
	for _, a := range byRoot {
		regions = append(regions, screenshotRegion{Rect: a.rect, Count: a.count})
		combined = combined.Union(a.rect)
	}
	// Largest-first, with a stable tie-break on position so the same diff always renders the same
	// list (map iteration order is not).
	sort.Slice(regions, func(i, j int) bool {
		if regions[i].Count != regions[j].Count {
			return regions[i].Count > regions[j].Count
		}
		if regions[i].Rect.Min.Y != regions[j].Rect.Min.Y {
			return regions[i].Rect.Min.Y < regions[j].Rect.Min.Y
		}
		return regions[i].Rect.Min.X < regions[j].Rect.Min.X
	})
	count := len(regions)
	if count > maxReportedRegions {
		regions = regions[:maxReportedRegions]
	}
	return regions, count, combined
}

// isScattered reads the "a font failed to load" signature off the clustering: lots of regions, none
// of them big, spread over most of the image in both axes.  Every text run moved a little.
func isScattered(regions []screenshotRegion, count int, combined image.Rectangle, w, h int) bool {
	if count < scatteredMinRegions || len(regions) == 0 || w == 0 || h == 0 {
		return false
	}
	// regions is sorted largest-first and truncated, so checking the reported ones checks the
	// biggest ones - if none of those is big, none of the rest is either.
	limit := float64(w*h) * scatteredMaxRegionArea
	for _, r := range regions {
		if float64(r.Rect.Dx()*r.Rect.Dy()) > limit {
			return false
		}
	}
	return float64(combined.Dx()) > scatteredMinSpan*float64(w) && float64(combined.Dy()) > scatteredMinSpan*float64(h)
}

// detectShift looks for the whole-image translation that best explains the difference.  "Everything
// moved down 1px because a banner above it grew" otherwise reports as one enormous region covering
// the entire element, which is true and completely unhelpful.
//
// Two-stage on purpose: 80 candidate offsets are searched over a centred crop using a precomputed
// luma plane (cheap, and luma is enough to align structure), and only the winner is re-counted at
// full resolution on all four channels - the same metric DifferingPixels was computed with, so the
// ratio test below compares like with like.
func detectShift(base, act *image.NRGBA, fraction float64, differing int, tolChannel int) (image.Point, bool) {
	if fraction < shiftMinFraction || differing == 0 {
		return image.Point{}, false
	}
	w, h := base.Bounds().Dx(), base.Bounds().Dy()
	// An image too thin for the search to mean anything gets no verdict at all - see
	// minShiftDimensionMultiple.
	if w <= minShiftDimensionMultiple*maxShiftOffset || h <= minShiftDimensionMultiple*maxShiftOffset {
		return image.Point{}, false
	}
	bl, al := lumaPlane(base), lumaPlane(act)

	cw, ch := min(w, shiftSearchWindow), min(h, shiftSearchWindow)
	x0, y0 := (w-cw)/2, (h-ch)/2
	best, bestPt := -1, image.Point{}
	for dy := -maxShiftOffset; dy <= maxShiftOffset; dy++ {
		for dx := -maxShiftOffset; dx <= maxShiftOffset; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			c := countLumaDiff(bl, al, w, h, x0, y0, cw, ch, dx, dy, tolChannel)
			if best < 0 || c < best {
				best, bestPt = c, image.Pt(dx, dy)
			}
		}
	}
	if best < 0 {
		return image.Point{}, false
	}

	full := countChannelDiff(base, act, w, h, bestPt.X, bestPt.Y, tolChannel)
	if float64(full) >= shiftResidualRatio*float64(differing) {
		// The offset does not explain the change.  A tiny localised edit must never be dressed up as
		// a shift, and a shift that leaves most of the difference behind is not the story either.
		return image.Point{}, false
	}
	return bestPt, true
}

func luma(r, g, b uint8) uint8 {
	return uint8((299*int(r) + 587*int(g) + 114*int(b)) / 1000)
}

func lumaPlane(img *image.NRGBA) []uint8 {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	out := make([]uint8, w*h)
	for y := range h {
		row := y * img.Stride
		for x := range w {
			i := row + x*4
			out[y*w+x] = luma(img.Pix[i], img.Pix[i+1], img.Pix[i+2])
		}
	}
	return out
}

// countLumaDiff counts differing pixels in the (x0,y0,cw,ch) window when the actual is read at an
// offset of (dx,dy).  Source pixels whose offset target falls OUTSIDE the image count as differing:
// the shift does not explain them, and skipping them would let an offset that slides most of the
// image off the edge score zero - which is exactly how a 2px-tall capture used to come back with a
// confident 4px-diagonal verdict.  On an image big enough to search, the penalty is the at-most
// maxShiftOffset-wide exposed band, which is far too small to unseat a genuine offset.
func countLumaDiff(bl, al []uint8, w, h, x0, y0, cw, ch, dx, dy, tolChannel int) int {
	count := 0
	for y := y0; y < y0+ch; y++ {
		ty := y + dy
		if ty < 0 || ty >= h {
			count += cw
			continue
		}
		for x := x0; x < x0+cw; x++ {
			tx := x + dx
			if tx < 0 || tx >= w {
				count++
				continue
			}
			d := int(bl[y*w+x]) - int(al[ty*w+tx])
			if d < 0 {
				d = -d
			}
			if d > tolChannel {
				count++
			}
		}
	}
	return count
}

// countChannelDiff is countLumaDiff's full-resolution, all-four-channels twin: the verification pass
// for a candidate shift, using the same metric as the original comparison.  Out-of-bounds targets
// count as differing here for the same reason they do there - they are pixels the shift leaves
// unexplained, and the residual test below is only honest if it says so.
func countChannelDiff(base, act *image.NRGBA, w, h, dx, dy, tolChannel int) int {
	count := 0
	for y := range h {
		ty := y + dy
		if ty < 0 || ty >= h {
			count += w
			continue
		}
		brow, arow := y*base.Stride, ty*act.Stride
		for x := range w {
			tx := x + dx
			if tx < 0 || tx >= w {
				count++
				continue
			}
			i, j := brow+x*4, arow+tx*4
			delta := 0
			for c := range 4 {
				dv := int(base.Pix[i+c]) - int(act.Pix[j+c])
				if dv < 0 {
					dv = -dv
				}
				if dv > delta {
					delta = dv
				}
			}
			if delta > tolChannel {
				count++
			}
		}
	}
	return count
}

// renderDiffImage draws the actual image desaturated and washed out toward white, with every
// differing pixel painted opaque magenta.  Keeping the actual underneath (rather than rendering the
// mask alone) is what makes the artifact readable - you can see WHAT changed, in place.  Magenta
// because it essentially never occurs in real UI, so it reads as annotation rather than content.
func renderDiffImage(act *image.NRGBA, mask *pixelMask) image.Image {
	w, h := act.Bounds().Dx(), act.Bounds().Dy()
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		orow, arow := y*out.Stride, y*act.Stride
		for x := range w {
			o := orow + x*4
			if mask.get(y*w + x) {
				out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = 0xff, 0x00, 0xff, 0xff
				continue
			}
			i := arow + x*4
			v := uint8(255 - (255-int(luma(act.Pix[i], act.Pix[i+1], act.Pix[i+2])))/4)
			out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = v, v, v, 0xff
		}
	}
	return out
}

// maskPNG paints rects over a PNG in maskFillColor and re-encodes it.  Rects are clipped to the
// image bounds and out-of-bounds rects are skipped, so a mask selector that matches something off
// the edge of an element capture is a no-op rather than an error.  Masking happens BEFORE a
// baseline is written and BEFORE every comparison, so the stored baseline is the masked image and
// the two sides are always masked identically.
func maskPNG(img []byte, rects []image.Rectangle) ([]byte, error) {
	if len(rects) == 0 {
		return img, nil // nothing to paint: don't pay for a decode/re-encode round trip
	}
	src, err := decodeToNRGBA(img)
	if err != nil {
		return nil, fmt.Errorf("could not decode the screenshot to mask: %w", err)
	}
	bounds := src.Bounds()
	for _, rect := range rects {
		r := rect.Intersect(bounds)
		if r.Empty() {
			continue
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			row := y * src.Stride
			for x := r.Min.X; x < r.Max.X; x++ {
				copy(src.Pix[row+x*4:row+x*4+4], maskFillColor[:])
			}
		}
	}
	buf := &bytes.Buffer{}
	if err := png.Encode(buf, src); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeDiffPNG encodes the rendered diff image.  Returns nil, nil when there is nothing to render
// (a dimension mismatch, or a passing comparison).
func (d *screenshotDiff) encodeDiffPNG() ([]byte, error) {
	d.analyze()
	if d == nil || d.DimensionMismatch || d.diff == nil {
		return nil, nil
	}
	buf := &bytes.Buffer{}
	if err := png.Encode(buf, d.diff); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// screenshotPaths carries the three resolved artifact locations the diagnosis prints.  Any may be
// empty, in which case its line is omitted.
type screenshotPaths struct {
	Baseline string
	Actual   string
	Diff     string
}

// diagnose renders the human- and agent-readable diagnosis of a failed comparison.  This is the
// point of the whole feature: alongside the images, say IN WORDS what changed and where, the way
// the poll trajectory says in words what shape a failing poll had.  See
// https://onsi.github.io/biloba/#visual-assertions
func (d *screenshotDiff) diagnose(name string, paths screenshotPaths) string {
	d.analyze() // idempotent: the caller has usually already asked for the diff image
	out := &strings.Builder{}
	fmt.Fprintf(out, "screenshot %q differs from baseline\n", name)
	if d.DimensionMismatch {
		// The size change is the whole diagnosis - there is no per-pixel story to tell on top of it.
		fmt.Fprintf(out, "  %s\n", d.dimensionLine())
	} else {
		fmt.Fprintf(out, "  %s of %s pixels differ (%.2f%%), max channel delta %d\n",
			withThousands(d.DifferingPixels), withThousands(d.TotalPixels), d.Fraction*100, d.MaxChannelDelta)
		if line := d.amplitudeLine(); line != "" {
			fmt.Fprintf(out, "  %s\n", line)
		}
		for _, line := range d.shapeLines() {
			fmt.Fprintf(out, "  %s\n", line)
		}
	}
	for _, p := range [][2]string{{"baseline:", paths.Baseline}, {"actual:", paths.Actual}, {"diff:", paths.Diff}} {
		if p[1] == "" {
			continue
		}
		fmt.Fprintf(out, "  %-9s %s\n", p[0], p[1])
	}
	return out.String()
}

// rasterizationChannelDelta is the largest per-channel difference that still reads as "the renderer
// drew the same thing slightly differently."  Subpixel antialiasing, a composited shadow landing on a
// clipped capture, a gradient dithered a shade over: all of these move a channel by a handful of
// levels.  A change to what is ON the page moves one by dozens or hundreds.  Deliberately low - the
// cost of missing a real content change that happens to be this faint is far higher than the cost of
// staying quiet about a rasterisation difference of 9.
const rasterizationChannelDelta = 8

// amplitudeLine turns max channel delta from a statistic into a verdict.  It is the number that
// decides the whole response to a failure - tolerate this, or go find what moved - and on its own it
// reads as telemetry: a reader who has already located the changed region from the box list walks
// straight past "max channel delta 3" and starts hunting for an element that is not there.  Saying it
// in a sentence is the same instruction as the shape lines, applied to amplitude instead of geometry.
func (d *screenshotDiff) amplitudeLine() string {
	if d.DifferingPixels == 0 || d.MaxChannelDelta > rasterizationChannelDelta {
		return ""
	}
	return fmt.Sprintf("every differing pixel differs by <= %d — a rasterisation or compositing difference, not a content change (absorb it with b.ChannelTolerance(%d), or look for a shadow or gradient bleeding into the capture)",
		d.MaxChannelDelta, d.MaxChannelDelta)
}

// shapeLines is the verdict: given the same pixel counts, WHICH of the three bugs the feedback named
// is this?  The order is a precedence, not a set of independent checks - a uniform shift also
// produces one huge region, and a scattered change also produces many boxes, so the more specific
// reading has to win before the generic one gets a chance to mislead.
func (d *screenshotDiff) shapeLines() []string {
	w, h := d.ActualBounds.Dx(), d.ActualBounds.Dy()
	lines := []string{}
	switch {
	case d.Shifted:
		lines = append(lines, fmt.Sprintf("changed region: uniform shift of the whole image, %s (dx=%d, dy=%d) — something above or before it likely grew or moved",
			shiftWords(d.Shift), d.Shift.X, d.Shift.Y))
	case d.Scattered:
		lines = append(lines, fmt.Sprintf("changed region: scattered — %d regions spread across the image — a font likely failed to load or rendered differently", d.RegionCount))
	case d.RegionCount == 1 && len(d.Regions) == 1:
		r := d.Regions[0].Rect
		lines = append(lines, fmt.Sprintf("changed region: one box, (%d,%d)-(%d,%d)  [%d%% of the width, %d%% of the height, at its %s]",
			r.Min.X, r.Min.Y, r.Max.X, r.Max.Y, percentOf(r.Dx(), w), percentOf(r.Dy(), h), edgeDescription(r, w, h)))
	default:
		lines = append(lines, fmt.Sprintf("changed regions: %d boxes", d.RegionCount))
		for _, r := range d.Regions {
			lines = append(lines, fmt.Sprintf("  (%d,%d)-(%d,%d)  %s pixels",
				r.Rect.Min.X, r.Rect.Min.Y, r.Rect.Max.X, r.Rect.Max.Y, withThousands(r.Count)))
		}
		if d.RegionCount > len(d.Regions) {
			lines = append(lines, fmt.Sprintf("  … and %d more", d.RegionCount-len(d.Regions)))
		}
	}
	if u := d.unchangedLine(); u != "" {
		lines = append(lines, u)
	}
	return lines
}

// unchangedLine states the complement: which side of the image is provably untouched.  It is often
// the faster read of the two - "everything below y=44 is unchanged" rules out the whole body of the
// page in one line - but it is only worth saying when a clear majority really is clean, so the
// largest untouched margin has to clear confinedUntouchedFraction to be printed.
//
// It stays quiet whenever the bounding box would be a partial truth: on a shift or a scattered
// change (where the box is not the story), and whenever the region list was truncated (where the
// union of the reported boxes is not the union of all of them).
func (d *screenshotDiff) unchangedLine() string {
	if d.Shifted || d.Scattered || len(d.Regions) == 0 || d.RegionCount != len(d.Regions) {
		return ""
	}
	w, h := d.ActualBounds.Dx(), d.ActualBounds.Dy()
	if w == 0 || h == 0 {
		return ""
	}
	bbox := d.Regions[0].Rect
	for _, r := range d.Regions[1:] {
		bbox = bbox.Union(r.Rect)
	}
	sides := []struct {
		fraction float64
		text     string
	}{
		{float64(h-bbox.Max.Y) / float64(h), fmt.Sprintf("unchanged: everything below y=%d", bbox.Max.Y)},
		{float64(bbox.Min.Y) / float64(h), fmt.Sprintf("unchanged: everything above y=%d", bbox.Min.Y)},
		{float64(w-bbox.Max.X) / float64(w), fmt.Sprintf("unchanged: everything right of x=%d", bbox.Max.X)},
		{float64(bbox.Min.X) / float64(w), fmt.Sprintf("unchanged: everything left of x=%d", bbox.Min.X)},
	}
	best := sides[0]
	for _, s := range sides[1:] {
		if s.fraction > best.fraction {
			best = s
		}
	}
	if best.fraction < confinedUntouchedFraction {
		return ""
	}
	return best.text
}

// summary is the one-line form used when a comparison is REPORTED rather than failed - by
// BILOBA_UPDATE_SCREENSHOTS, which prints what it changed so the update is reviewable in words and
// not only as a binary blob in a diff.
//
// A nil diff, or one with no baseline bounds, is the "there was nothing to compare against" case:
// a brand new baseline was written.
func (d *screenshotDiff) summary(name string) string {
	if d == nil {
		return fmt.Sprintf("screenshot %q written (new baseline)", name)
	}
	d.analyze() // shapePhrase below reads the clustering, which a bare comparison does not compute
	if d.BaselineBounds.Empty() {
		return fmt.Sprintf("screenshot %q written (new baseline, %dx%d)", name, d.ActualBounds.Dx(), d.ActualBounds.Dy())
	}
	if d.DimensionMismatch {
		line := fmt.Sprintf("screenshot %q updated — resized from %dx%d to %dx%d", name,
			d.BaselineBounds.Dx(), d.BaselineBounds.Dy(), d.ActualBounds.Dx(), d.ActualBounds.Dy())
		if delta := d.sizeDeltaWords(); delta != "" {
			line += " (" + delta + ")"
		}
		return line
	}
	if d.Match {
		return fmt.Sprintf("screenshot %q unchanged", name)
	}
	return fmt.Sprintf("screenshot %q updated — %s of %s pixels changed (%.2f%%), %s", name,
		withThousands(d.DifferingPixels), withThousands(d.TotalPixels), d.Fraction*100, d.shapePhrase())
}

// shapePhrase is shapeLines' one-clause form, for summary.
func (d *screenshotDiff) shapePhrase() string {
	switch {
	case d.Shifted:
		return fmt.Sprintf("a uniform shift %s", shiftWords(d.Shift))
	case d.Scattered:
		return fmt.Sprintf("scattered across %d regions", d.RegionCount)
	case d.RegionCount == 1 && len(d.Regions) == 1:
		r := d.Regions[0].Rect
		return fmt.Sprintf("one box at (%d,%d)-(%d,%d)", r.Min.X, r.Min.Y, r.Max.X, r.Max.Y)
	case d.RegionCount == 0:
		return "no distinct regions"
	default:
		return fmt.Sprintf("%d boxes", d.RegionCount)
	}
}

// dimensionLine is the whole body of a dimension-mismatch diagnosis: the two sizes, plus the delta
// spelled out.  "800x600 vs 800x640" makes you do arithmetic; "40px taller" does not.
func (d *screenshotDiff) dimensionLine() string {
	line := fmt.Sprintf("baseline is %dx%d, actual is %dx%d",
		d.BaselineBounds.Dx(), d.BaselineBounds.Dy(), d.ActualBounds.Dx(), d.ActualBounds.Dy())
	if delta := d.sizeDeltaWords(); delta != "" {
		line += " (" + delta + ")"
	}
	return line
}

func (d *screenshotDiff) sizeDeltaWords() string {
	dw := d.ActualBounds.Dx() - d.BaselineBounds.Dx()
	dh := d.ActualBounds.Dy() - d.BaselineBounds.Dy()
	parts := []string{}
	if dh != 0 {
		parts = append(parts, fmt.Sprintf("%dpx %s", abs(dh), pickWord(dh > 0, "taller", "shorter")))
	}
	if dw != 0 {
		parts = append(parts, fmt.Sprintf("%dpx %s", abs(dw), pickWord(dw > 0, "wider", "narrower")))
	}
	return strings.Join(parts, " and ")
}

// shiftWords renders a translation the way a person would say it: "1px down", "2px right and 1px up".
func shiftWords(p image.Point) string {
	parts := []string{}
	if p.X != 0 {
		parts = append(parts, fmt.Sprintf("%dpx %s", abs(p.X), pickWord(p.X > 0, "right", "left")))
	}
	if p.Y != 0 {
		parts = append(parts, fmt.Sprintf("%dpx %s", abs(p.Y), pickWord(p.Y > 0, "down", "up")))
	}
	if len(parts) == 0 {
		return "0px"
	}
	return strings.Join(parts, " and ")
}

// edgeDescription says where in the image a box sits, in the words someone would use to point at it.
// This is the line that turns "(938,14)-(1272,44)" into "it is the header": a box in the top third is
// at the top edge, one that is neither near a horizontal nor a vertical extreme is in the centre.
func edgeDescription(r image.Rectangle, w, h int) string {
	if w == 0 || h == 0 {
		return "centre"
	}
	vertical, horizontal := "", ""
	switch {
	case r.Max.Y*3 <= h:
		vertical = "top"
	case r.Min.Y*3 >= 2*h:
		vertical = "bottom"
	}
	switch {
	case r.Max.X*3 <= w:
		horizontal = "left"
	case r.Min.X*3 >= 2*w:
		horizontal = "right"
	}
	switch {
	case vertical != "" && horizontal != "":
		return vertical + "-" + horizontal + " corner"
	case vertical != "":
		return vertical + " edge"
	case horizontal != "":
		return horizontal + " edge"
	default:
		return "centre"
	}
}

func percentOf(part, whole int) int {
	if whole == 0 {
		return 0
	}
	return int(math.Round(float64(part) / float64(whole) * 100))
}

// withThousands renders an int with comma group separators.  A dependency for this would be absurd.
func withThousands(n int) string {
	s := fmt.Sprintf("%d", n)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	out := &strings.Builder{}
	lead := len(s) % 3
	if lead > 0 {
		out.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if out.Len() > 0 {
			out.WriteByte(',')
		}
		out.WriteString(s[i : i+3])
	}
	return sign + out.String()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func pickWord(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

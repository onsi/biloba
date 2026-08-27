package biloba

import "image"

/*
A VisualComparison is what one [Biloba.HaveScreenshot] comparison measured.  Get them with
[Biloba.VisualComparisons].

Biloba renders a comparison as prose, in the failure message - which is the right form for the
person reading a failing suite, and the wrong form for a reporter that wants to put "4% of pixels
changed, in three regions" on a dashboard, or for a spec that wants to assert on HOW an image
changed rather than merely that it did.  This is the same information as data.

The fields divide into two kinds, and the distinction is worth knowing before you assert on one:

  - The MEASUREMENTS - the sizes, TotalPixels, DifferingPixels, Fraction, MaxChannelDelta and the
    tolerances - are definitions.  Given two PNGs and a tolerance they have exactly one right
    answer, and Biloba will not quietly change what they mean.
  - The VERDICTS - Regions, RegionCount, Shifted, Shift and Scattered - are tuned heuristics that
    answer "what does this change look like?".  Their shape is stable; the thresholds behind them
    are not, and Biloba retunes them as the diagnosis improves.  Assert on them the way you would
    assert on any heuristic: "there is one region" is a fair assertion, "there are exactly 7" is a
    spec that will break on an improvement.

Read https://onsi.github.io/biloba/#collecting-the-files-biloba-wrote to learn more
*/
type VisualComparison struct {
	// Name is the baseline name passed to [Biloba.HaveScreenshot].  Label is how the diagnosis names
	// it - Name, plus the colour scheme when [Biloba.InColorSchemes] made more than one comparison
	// under that name.  ColorScheme is "light"/"dark", or empty when the assertion did not emulate one.
	Name        string
	Label       string
	ColorScheme string

	// Match reports whether this comparison passed under the tolerances below.  Every comparison a
	// spec makes is recorded, passing ones included, so an empty [Biloba.VisualComparisons] means
	// "this spec asserted nothing visually" rather than "everything passed".
	Match bool

	// BaselinePath, ActualPath and DiffPath are the files on disk, and are the same paths
	// [Biloba.Artifacts] reports.  ActualPath and DiffPath are written only when a comparison fails,
	// so they are empty on a passing one; DiffPath is also empty on a dimension mismatch, where there
	// is no meaningful per-pixel diff to draw.
	BaselinePath string
	ActualPath   string
	DiffPath     string

	// BaselineSize and ActualSize are the two images' dimensions in pixels.  DimensionMismatch
	// short-circuits everything else: when the sizes differ there is no per-pixel comparison to make,
	// so DifferingPixels and the verdicts below are all zero and the size change is the whole story.
	BaselineSize      image.Point
	ActualSize        image.Point
	DimensionMismatch bool

	// Tolerance and ChannelTolerance are what the comparison actually ran under - the defaults, or
	// whatever [Biloba.Tolerance] and [Biloba.ChannelTolerance] set.  A number without the tolerance
	// that produced it cannot be compared against anything.
	Tolerance        float64
	ChannelTolerance int

	// TotalPixels is the image area; DifferingPixels is how many pixels differ by more than
	// ChannelTolerance on their worst channel; Fraction is the ratio of the two.  MaxChannelDelta is
	// the largest single-channel difference anywhere in the image, which is what separates "everything
	// shifted by one shade" from "a box turned red".
	TotalPixels     int
	DifferingPixels int
	Fraction        float64
	MaxChannelDelta int

	// Regions are the clustered changed areas, ordered by DIFFERING-PIXEL COUNT (not by area), largest
	// first.  RegionCount is the true total before Regions was truncated for reporting, so
	// len(Regions) < RegionCount means truncation, not a lost region.  Shift/Shifted describe a
	// whole-image translation that explains the difference - "everything moved down a pixel" is a
	// completely different bug from "one box changed", and bounding boxes alone cannot tell them
	// apart.  Shifted is never true with a zero Shift: the search skips the (0,0) offset, so
	// "shifted by nothing" is not a state this can report.  Scattered reports the
	// many-small-regions-spread-widely signature of a font that rendered differently.  All five are
	// heuristics: see the note on VisualComparison.
	Regions     []VisualRegion
	RegionCount int
	Shifted     bool
	Shift       image.Point
	Scattered   bool
}

/*
A VisualRegion is one clustered area of change inside a [VisualComparison].

Read https://onsi.github.io/biloba/#collecting-the-files-biloba-wrote to learn more
*/
type VisualRegion struct {
	// Bounds is the region in image coordinates, Min-inclusive and Max-exclusive like any
	// image.Rectangle - a single changed pixel at (10,20) is Rect(10,20,11,21), so the region's width
	// is Max.X-Min.X.
	Bounds image.Rectangle
	// DifferingPixels is how many pixels inside Bounds actually differ.  A region whose count is far
	// below its area is a sparse change (a glyph, a border) rather than a solid repaint.
	DifferingPixels int
}

/*
VisualComparisons returns what every [Biloba.HaveScreenshot] assertion in the current spec measured, in the order the comparisons reported - passing ones included.  It is the failure message's prose diagnosis as data, for a reporter that wants to record the numbers rather than print the sentences, or for a spec that wants to assert on how an image changed.

Only the comparison that DECIDED an assertion is recorded.  HaveScreenshot polls, so a page that settles compares many times; the losing attempts are not what the assertion is about and would swamp the list.  One assertion under [Biloba.InColorSchemes] records one comparison per scheme.

The list is cleared by [Biloba.Prepare], so it always describes the current spec.  A failing comparison is recorded when the failure is reported, which in Ginkgo means the numbers are available from a ReportAfterEach alongside [Biloba.Artifacts]:

	ReportAfterEach(func(report SpecReport) {
		for _, comparison := range b.VisualComparisons() {
			recordVisualRegression(comparison.Label, comparison.Fraction, comparison.DiffPath)
		}
	})

VisualComparisons is a snapshot: it does not poll, and it rejects the poll-config knobs.

Read https://onsi.github.io/biloba/#collecting-the-files-biloba-wrote to learn more
*/
func (b *Biloba) VisualComparisons() []VisualComparison {
	b.guardConfig("VisualComparisons")
	root := b.root
	root.artifactLock.Lock()
	defer root.artifactLock.Unlock()
	out := make([]VisualComparison, len(root.visualComparisons))
	copy(out, root.visualComparisons)
	return out
}

// recordVisualComparison appends to the root tab's per-spec comparison list.  It shares artifactLock
// with the artifact list because the two are written from the same places, at the same moments, and
// are read together.
func (b *Biloba) recordVisualComparison(comparison VisualComparison) {
	root := b.root
	root.artifactLock.Lock()
	defer root.artifactLock.Unlock()
	root.visualComparisons = append(root.visualComparisons, comparison)
}

// resetVisualComparisons drops the previous spec's list.  Called by Prepare, alongside
// resetArtifacts and for the same reason - see the note there.
func (b *Biloba) resetVisualComparisons() {
	root := b.root
	root.artifactLock.Lock()
	defer root.artifactLock.Unlock()
	root.visualComparisons = nil
}

// comparison assembles the VisualComparison for one decided comparison.  diff is nil only on the
// paths that never got as far as comparing (a missing or unreadable baseline), which record nothing.
func (m *screenshotMatcher) comparison(scheme string, diff *screenshotDiff, paths screenshotPaths) VisualComparison {
	out := VisualComparison{
		Name:              m.name,
		Label:             m.label(scheme),
		ColorScheme:       scheme,
		Match:             diff.Match,
		BaselinePath:      paths.Baseline,
		ActualPath:        paths.Actual,
		DiffPath:          paths.Diff,
		BaselineSize:      image.Point{X: diff.BaselineBounds.Dx(), Y: diff.BaselineBounds.Dy()},
		ActualSize:        image.Point{X: diff.ActualBounds.Dx(), Y: diff.ActualBounds.Dy()},
		DimensionMismatch: diff.DimensionMismatch,
		Tolerance:         m.cfg.tolerance.fraction,
		ChannelTolerance:  m.cfg.tolerance.channel,
		TotalPixels:       diff.TotalPixels,
		DifferingPixels:   diff.DifferingPixels,
		Fraction:          diff.Fraction,
		MaxChannelDelta:   diff.MaxChannelDelta,
		RegionCount:       diff.RegionCount,
		Shifted:           diff.Shifted,
		Shift:             diff.Shift,
		Scattered:         diff.Scattered,
	}
	for _, region := range diff.Regions {
		out.Regions = append(out.Regions, VisualRegion{Bounds: region.Rect, DifferingPixels: region.Count})
	}
	return out
}

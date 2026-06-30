package biloba

import (
	"fmt"

	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

/*
Box is the viewport-relative layout rectangle of an element, returned by [Biloba.GetBoundingBox].  All
fields are CSS pixels measured from the top-left of the viewport (so Top/Left already account for page
scroll, exactly like getBoundingClientRect).  CenterX/CenterY are the box's center point.

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
type Box struct {
	Top     float64
	Left    float64
	Width   float64
	Height  float64
	Bottom  float64
	Right   float64
	CenterX float64
	CenterY float64
}

/*
ScrollOffset is the scroll position of a scroll container, returned by [Biloba.GetScrollOffset].  Top/Left
are the container's current scrollTop/scrollLeft; MaxTop/MaxLeft are the largest values those can reach
(scrollHeight-clientHeight / scrollWidth-clientWidth), so Top == MaxTop means "scrolled to the bottom".

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
type ScrollOffset struct {
	Top     float64
	Left    float64
	MaxTop  float64
	MaxLeft float64
}

func (box Box) String() string {
	return fmt.Sprintf("Box{Top:%g Left:%g Width:%g Height:%g Bottom:%g Right:%g CenterX:%g CenterY:%g}",
		box.Top, box.Left, box.Width, box.Height, box.Bottom, box.Right, box.CenterX, box.CenterY)
}

func newBox(input any) Box {
	m := input.(map[string]any)
	return Box{
		Top:     toFloat64(m["top"]),
		Left:    toFloat64(m["left"]),
		Width:   toFloat64(m["width"]),
		Height:  toFloat64(m["height"]),
		Bottom:  toFloat64(m["bottom"]),
		Right:   toFloat64(m["right"]),
		CenterX: toFloat64(m["centerX"]),
		CenterY: toFloat64(m["centerY"]),
	}
}

func newScrollOffset(input any) ScrollOffset {
	m := input.(map[string]any)
	return ScrollOffset{
		Top:     toFloat64(m["top"]),
		Left:    toFloat64(m["left"]),
		MaxTop:  toFloat64(m["maxTop"]),
		MaxLeft: toFloat64(m["maxLeft"]),
	}
}

/*
GetBoundingBox(selector) returns the viewport-relative layout [Box] of the first element matching selector.

GetBoundingBox polls by default: it waits until an element matching selector is present AND has a
non-degenerate layout box (width and height > 0 - i.e. actually laid out, not merely in the DOM), then
returns its rectangle.  This is the idiomatic replacement for hand-rolling getBoundingClientRect()
through [Biloba.Run]: readiness is folded in, so you never read a zero box mid-layout.

To assert on geometry that settles asynchronously, prefer the matcher form [Biloba.HaveBoundingBox] so
Gomega does the polling:

	Eventually(".hero .sec").Should(b.HaveBoundingBox(HaveField("Top", BeNumerically("<", 120))))

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) GetBoundingBox(selector any) Box {
	b.gt.Helper()
	var result Box
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		r := b.runBilobaHandler("boundingBoxP", sel)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		result = newBox(r.Result)
		b.recordProbe(probeKey("GetBoundingBox", sel), result)
		return true, nil
	}).WithMessage("be present and laid out (have a non-degenerate bounding box)")
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
HaveBoundingBox(matcher) is the Gomega matcher counterpart of [Biloba.GetBoundingBox]: it passes once the
first element matching selector is present and laid out AND its [Box] satisfies the provided matcher.
The matcher receives the [Box] value, so compose it with Gomega's HaveField:

	Eventually(".hero .sec").Should(b.HaveBoundingBox(HaveField("Top", BeNumerically("<", 120))))

Because it returns a matcher you poll, configure the Eventually/Expect that wraps it - knobs on the
Biloba view (WithTimeout/Immediate/...) are not honored here.

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) HaveBoundingBox(matcher types.GomegaMatcher) types.GomegaMatcher {
	data := map[string]any{"Matcher": matcher}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("boundingBoxP", selector)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		box := newBox(r.Result)
		data["Result"] = box
		b.recordProbe(probeKey("HaveBoundingBox", selector), box)
		return matcher.Match(box)
	}).WithTemplate("HaveBoundingBox for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
GetScrollOffset(selector) returns the [ScrollOffset] of the first element matching selector (treated as a
scroll container).

GetScrollOffset polls by default: it waits until an element matching selector is present, then reports its
scrollTop/scrollLeft and the maximum scrollable offsets.  Use it instead of reading scrollTop through
[Biloba.Run].  For assertions that settle asynchronously, prefer the matcher form [Biloba.HaveScrollOffset].

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) GetScrollOffset(selector any) ScrollOffset {
	b.gt.Helper()
	var result ScrollOffset
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		r := b.runBilobaHandler("scrollOffsetP", sel)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		result = newScrollOffset(r.Result)
		b.recordProbe(probeKey("GetScrollOffset", sel), result)
		return true, nil
	}).WithMessage("be present (so its scroll offset can be read)")
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
HaveScrollOffset(matcher) is the Gomega matcher counterpart of [Biloba.GetScrollOffset]: it passes once
the first element matching selector is present AND its [ScrollOffset] satisfies the provided matcher,
which receives the [ScrollOffset] value:

	Eventually(".scroller").Should(b.HaveScrollOffset(HaveField("Top", BeNumerically("==", 0))))

Because it returns a matcher you poll, configure the Eventually/Expect that wraps it.

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) HaveScrollOffset(matcher types.GomegaMatcher) types.GomegaMatcher {
	data := map[string]any{"Matcher": matcher}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("scrollOffsetP", selector)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		offset := newScrollOffset(r.Result)
		data["Result"] = offset
		b.recordProbe(probeKey("HaveScrollOffset", selector), offset)
		return matcher.Match(offset)
	}).WithTemplate("HaveScrollOffset for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

// offsetWithin is the shared substrate behind GetOffsetTopWithin/GetOffsetLeftWithin: it polls until both
// selector and container are present and selector has a non-degenerate box, then returns the named axis
// of selector's viewport offset relative to container's top-left corner.
func (b *Biloba) offsetWithin(selector, container any, axis string) float64 {
	b.gt.Helper()
	encodedContainer, encErr := encodeSelector(container)
	var result float64
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		if encErr != nil {
			return false, encErr
		}
		r := b.runBilobaHandler("offsetWithinP", sel, encodedContainer)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		result = toFloat64(r.Result.(map[string]any)[axis])
		b.recordProbe(probeKey("GetOffsetWithin:"+axis, sel), result)
		return true, nil
	}).WithMessage("be present and laid out within its container")
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
GetOffsetTopWithin(selector, container) returns how far the top of the first element matching selector sits
below the top of container - i.e. (element.top - container.top) in viewport coordinates.

This is the measurement a "scrolled near the top of the pane" spec actually wants.  It polls by default
until both elements are present and the element has a non-degenerate box.  To assert on a threshold that
settles asynchronously, prefer the matcher form [Biloba.HaveOffsetTopWithin]:

	Eventually(".hero .sec").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("<", 120)))

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) GetOffsetTopWithin(selector, container any) float64 {
	b.gt.Helper()
	return b.offsetWithin(selector, container, "top")
}

/*
GetOffsetLeftWithin(selector, container) is the horizontal sibling of [Biloba.GetOffsetTopWithin]: it returns
(element.left - container.left) in viewport coordinates.

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) GetOffsetLeftWithin(selector, container any) float64 {
	b.gt.Helper()
	return b.offsetWithin(selector, container, "left")
}

// haveOffsetWithin is the shared substrate behind HaveOffsetTopWithin/HaveOffsetLeftWithin.
func (b *Biloba) haveOffsetWithin(name, axis string, container any, expected ...any) types.GomegaMatcher {
	encodedContainer, encErr := encodeSelector(container)
	matcher := matcherOrEqual(firstOrNil(expected))
	data := map[string]any{"Name": name, "Matcher": matcher}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		if encErr != nil {
			return false, encErr
		}
		r := b.runBilobaHandler("offsetWithinP", selector, encodedContainer)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		value := toFloat64(r.Result.(map[string]any)[axis])
		data["Result"] = value
		b.recordProbe(probeKey(name, selector), value)
		return matcher.Match(value)
	}).WithTemplate("{{.Data.Name}} for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
HaveOffsetTopWithin(container, expected) is the Gomega matcher counterpart of [Biloba.OffsetTopWithin]:
it passes once the first element matching selector is laid out within container AND its top offset
(element.top - container.top) satisfies expected.  expected may be a Gomega matcher or a plain value
(compared with Equal):

	Eventually(".hero .sec").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("<", 120)))

Because it returns a matcher you poll, configure the Eventually/Expect that wraps it.

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) HaveOffsetTopWithin(container any, expected ...any) types.GomegaMatcher {
	return b.haveOffsetWithin("HaveOffsetTopWithin", "top", container, expected...)
}

/*
HaveOffsetLeftWithin(container, expected) is the horizontal sibling of [Biloba.HaveOffsetTopWithin],
asserting on (element.left - container.left).

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) HaveOffsetLeftWithin(container any, expected ...any) types.GomegaMatcher {
	return b.haveOffsetWithin("HaveOffsetLeftWithin", "left", container, expected...)
}

func firstOrNil(expected []any) any {
	if len(expected) == 0 {
		return nil
	}
	return expected[0]
}

/*
BoxDelta is the per-field difference between two element boxes - the subject's field minus the other's -
returned by [Biloba.GetGapBetween].  Positive Top means the subject sits lower than the other; CenterX
near 0 means the two boxes share a vertical center line; Width/Height near 0 means they're the same size.
These are the comparisons a "does A line up with / center within B" spec actually wants.

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
type BoxDelta struct {
	Top     float64
	Left    float64
	Bottom  float64
	Right   float64
	Width   float64
	Height  float64
	CenterX float64
	CenterY float64
}

func newBoxDelta(a, other Box) BoxDelta {
	return BoxDelta{
		Top:     a.Top - other.Top,
		Left:    a.Left - other.Left,
		Bottom:  a.Bottom - other.Bottom,
		Right:   a.Right - other.Right,
		Width:   a.Width - other.Width,
		Height:  a.Height - other.Height,
		CenterX: a.CenterX - other.CenterX,
		CenterY: a.CenterY - other.CenterY,
	}
}

func (d BoxDelta) String() string {
	return fmt.Sprintf("BoxDelta{Top:%g Left:%g Bottom:%g Right:%g Width:%g Height:%g CenterX:%g CenterY:%g}",
		d.Top, d.Left, d.Bottom, d.Right, d.Width, d.Height, d.CenterX, d.CenterY)
}

// relativeBoxes is the shared substrate behind all the pairwise-geometry methods: it runs the atomic
// two-box probe and invokes do(a, other) with both boxes read at a single layout instant.  ok is false
// (no error) while either element is absent or not yet laid out, so a polling caller keeps waiting; a
// genuine JS error surfaces as err.
func (b *Biloba) relativeBoxes(selector, other any, do func(a, other Box)) (ok bool, err error) {
	encodedOther, encErr := encodeSelector(other)
	if encErr != nil {
		return false, encErr
	}
	r := b.runBilobaHandler("relativeBoxesP", selector, encodedOther)
	if r.Error() != nil {
		return false, r.Error()
	}
	if !r.Success {
		return false, nil
	}
	m := r.Result.(map[string]any)
	do(newBox(m["a"]), newBox(m["b"]))
	return true, nil
}

// relationalMatcher builds a boolean pairwise-geometry matcher (BeAbove/Encloses/...): it reads both
// boxes atomically and passes when rel(subject, other) holds.  name keys the poll trajectory (recorded
// as the BoxDelta so the converging gap is visible on failure); verb is the phrase used in the message.
func (b *Biloba) relationalMatcher(name, verb string, other any, rel func(a, other Box) bool) types.GomegaMatcher {
	data := map[string]any{"Other": fmt.Sprintf("%v", other)}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		pass := false
		ok, err := b.relativeBoxes(selector, other, func(a, o Box) {
			data["A"], data["B"] = a, o
			b.recordProbe(probeKey(name, selector), newBoxDelta(a, o))
			pass = rel(a, o)
		})
		if !ok {
			return false, err
		}
		return pass, nil
	}).WithTemplate("Expected {{.Actual}} to {{if .Failure}}{{else}}NOT {{end}}"+verb+" {{.Data.Other}}\n  subject box: {{.Data.A}}\n  other box:   {{.Data.B}}", data)
}

/*
BeAbove(otherSelector) is a Gomega matcher that passes once the subject's box sits entirely above the
other element's box (subject.Bottom <= other.Top).  Both boxes are read in one atomic probe, so the
relation is judged at a single layout instant:

	Eventually(tabSel).Should(b.BeAbove(tileSel))

Because it returns a matcher you poll, configure the Eventually/Expect that wraps it.

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) BeAbove(otherSelector any) types.GomegaMatcher {
	return b.relationalMatcher("BeAbove", "be above", otherSelector, func(a, o Box) bool { return a.Bottom <= o.Top })
}

/*
BeBelow(otherSelector) is the vertical mirror of [Biloba.BeAbove]: it passes once the subject's box sits
entirely below the other element's box (subject.Top >= other.Bottom).

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) BeBelow(otherSelector any) types.GomegaMatcher {
	return b.relationalMatcher("BeBelow", "be below", otherSelector, func(a, o Box) bool { return a.Top >= o.Bottom })
}

/*
BeLeftOf(otherSelector) passes once the subject's box sits entirely to the left of the other element's
box (subject.Right <= other.Left).

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) BeLeftOf(otherSelector any) types.GomegaMatcher {
	return b.relationalMatcher("BeLeftOf", "be left of", otherSelector, func(a, o Box) bool { return a.Right <= o.Left })
}

/*
BeRightOf(otherSelector) passes once the subject's box sits entirely to the right of the other element's
box (subject.Left >= other.Right).

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) BeRightOf(otherSelector any) types.GomegaMatcher {
	return b.relationalMatcher("BeRightOf", "be right of", otherSelector, func(a, o Box) bool { return a.Left >= o.Right })
}

/*
Encloses(otherSelector) passes once the subject's box fully contains the other element's box on all four
edges (subject.Top <= other.Top, subject.Left <= other.Left, subject.Bottom >= other.Bottom,
subject.Right >= other.Right):

	Eventually(frameSel).Should(b.Encloses(tabSel))

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) Encloses(otherSelector any) types.GomegaMatcher {
	return b.relationalMatcher("Encloses", "enclose", otherSelector, func(a, o Box) bool {
		return a.Top <= o.Top && a.Left <= o.Left && a.Bottom >= o.Bottom && a.Right >= o.Right
	})
}

/*
Overlaps(otherSelector) passes once the subject's box intersects the other element's box (the two
rectangles share any area):

	Eventually(iconSel).Should(b.Overlaps(buttonSel))

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) Overlaps(otherSelector any) types.GomegaMatcher {
	return b.relationalMatcher("Overlaps", "overlap", otherSelector, func(a, o Box) bool {
		return a.Left < o.Right && a.Right > o.Left && a.Top < o.Bottom && a.Bottom > o.Top
	})
}

/*
GetGapBetween(selector, otherSelector) returns the [BoxDelta] between the first element matching selector
and the first element matching otherSelector - the subject's box fields minus the other's - read in one
atomic probe.  Reach for it when a relation is numeric rather than boolean: "these two share a center
line" (CenterX ~ 0), "this column is the same width as that one" (Width ~ 0), "the footer sits 12px below
the tools" (Top ~ 12).

GetGapBetween polls by default until both elements are present and laid out.  To assert on a delta that
settles asynchronously, prefer the matcher form [Biloba.HaveGapBetween].

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) GetGapBetween(selector, otherSelector any) BoxDelta {
	b.gt.Helper()
	var result BoxDelta
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		ok, err := b.relativeBoxes(sel, otherSelector, func(a, o Box) {
			result = newBoxDelta(a, o)
			b.recordProbe(probeKey("GetGapBetween", sel), result)
		})
		return ok, err
	}).WithMessage("be present and laid out alongside the other element")
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
HaveGapBetween(otherSelector, expected) is the Gomega matcher counterpart of [Biloba.GetGapBetween]: it
passes once both elements are laid out AND the [BoxDelta] between them satisfies expected, which may be a
Gomega matcher or a plain value (compared with Equal):

	Eventually(spanSel).Should(b.HaveGapBetween(cardSel, HaveField("CenterX", BeNumerically("~", 0, 1))))

Because it returns a matcher you poll, configure the Eventually/Expect that wraps it.

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) HaveGapBetween(otherSelector any, expected ...any) types.GomegaMatcher {
	matcher := matcherOrEqual(firstOrNil(expected))
	data := map[string]any{"Other": fmt.Sprintf("%v", otherSelector), "Matcher": matcher}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		pass := false
		var matchErr error
		ok, err := b.relativeBoxes(selector, otherSelector, func(a, o Box) {
			delta := newBoxDelta(a, o)
			data["Result"] = delta
			b.recordProbe(probeKey("HaveGapBetween", selector), delta)
			pass, matchErr = matcher.Match(delta)
		})
		if !ok {
			return false, err
		}
		return pass, matchErr
	}).WithTemplate("HaveGapBetween {{.Data.Other}} for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
BeInViewport() is a Gomega matcher that passes once the subject is laid out AND its box intersects the
visible layout viewport - i.e. the element is actually on screen, not merely rendered somewhere off in a
scrolled-away region.  This is the assertion a "after the scroll the target is visible" spec wants, and
is distinct from [Biloba.BeVisible], which only checks the element is rendered at all:

	Eventually(noteSel).Should(b.BeInViewport())

Partial visibility counts: any overlap with the viewport passes.  Because it returns a matcher you poll,
configure the Eventually/Expect that wraps it.

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) BeInViewport() types.GomegaMatcher {
	data := map[string]any{}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("inViewportP", selector)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		m := r.Result.(map[string]any)
		top, left := toFloat64(m["top"]), toFloat64(m["left"])
		bottom, right := toFloat64(m["bottom"]), toFloat64(m["right"])
		vw, vh := toFloat64(m["vw"]), toFloat64(m["vh"])
		data["Top"], data["Left"], data["Bottom"], data["Right"], data["VW"], data["VH"] = top, left, bottom, right, vw, vh
		b.recordProbe(probeKey("BeInViewport", selector), Box{Top: top, Left: left, Bottom: bottom, Right: right})
		onScreen := left < vw && right > 0 && top < vh && bottom > 0
		return onScreen, nil
	}).WithTemplate("Expected {{.Actual}} to {{if .Failure}}{{else}}NOT {{end}}be within the viewport.\n  element: top={{.Data.Top}} left={{.Data.Left}} bottom={{.Data.Bottom}} right={{.Data.Right}}\n  viewport: {{.Data.VW}}x{{.Data.VH}}", data)
}

// documentOrder reads the compareDocumentPosition bitmask of otherSelector relative to selector.  ok is
// false (no error) until both elements are present, so a polling caller keeps waiting.
func (b *Biloba) documentOrder(selector, other any, do func(mask int)) (ok bool, err error) {
	encodedOther, encErr := encodeSelector(other)
	if encErr != nil {
		return false, encErr
	}
	r := b.runBilobaHandler("documentOrderP", selector, encodedOther)
	if r.Error() != nil {
		return false, r.Error()
	}
	if !r.Success {
		return false, nil
	}
	do(int(toFloat64(r.Result)))
	return true, nil
}

// DOM Node.compareDocumentPosition bitmask constants (the only two we test).
const (
	documentPositionPreceding = 0x02
	documentPositionFollowing = 0x04
)

// documentOrderMatcher builds BePrecededBy/BeFollowedBy: it reads compareDocumentPosition once both
// elements are present and passes when the named bit is set.
func (b *Biloba) documentOrderMatcher(name, verb string, other any, bit int) types.GomegaMatcher {
	data := map[string]any{"Other": fmt.Sprintf("%v", other)}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		pass := false
		ok, err := b.documentOrder(selector, other, func(mask int) {
			b.recordProbe(probeKey(name, selector), mask)
			pass = mask&bit != 0
		})
		if !ok {
			return false, err
		}
		return pass, nil
	}).WithTemplate("Expected {{.Actual}} to {{if .Failure}}{{else}}NOT {{end}}"+verb+" {{.Data.Other}} in document order", data)
}

/*
BePrecededBy(otherSelector) is a Gomega matcher that passes once the other element precedes the subject
in document order (compareDocumentPosition reports PRECEDING).  Use it to assert structural ordering of
dynamically-inserted nodes:

	Eventually(noteSel).Should(b.BePrecededBy(sectionSel))

Because it returns a matcher you poll, configure the Eventually/Expect that wraps it.

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) BePrecededBy(otherSelector any) types.GomegaMatcher {
	return b.documentOrderMatcher("BePrecededBy", "be preceded by", otherSelector, documentPositionPreceding)
}

/*
BeFollowedBy(otherSelector) is the mirror of [Biloba.BePrecededBy]: it passes once the other element
follows the subject in document order (compareDocumentPosition reports FOLLOWING):

	Eventually(quizSel).Should(b.BeFollowedBy(noteSel))

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) BeFollowedBy(otherSelector any) types.GomegaMatcher {
	return b.documentOrderMatcher("BeFollowedBy", "be followed by", otherSelector, documentPositionFollowing)
}

/*
GetComputedStyle(selector, property) returns the resolved computed CSS value of property on the first
element matching selector.  Unlike the matcher [Biloba.HaveComputedStyle], it hands you the value as a
string so you can do Go-side math on it (relative luminance, hex->RGB, custom-property resolution).  It
resolves CSS custom properties too, so a design-token read works:

	hex := b.GetComputedStyle(".rail", "--stage")   // -> "rgb(220, 228, 225)" or "#DCE4E1"
	z := b.GetComputedStyle(sel, "z-index")

Property names follow getPropertyValue semantics (kebab-case, e.g. "z-index"; custom properties as
"--name").  GetComputedStyle polls by default until the element is present.

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) GetComputedStyle(selector any, property string) string {
	b.gt.Helper()
	var result string
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		r := b.runBilobaHandler("getComputedStyleP", sel, property)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		result, _ = r.Result.(string)
		b.recordProbe(probeKey("GetComputedStyle:"+property, sel), result)
		return true, nil
	}).WithMessage("be present (so its computed style can be read)")
	b.pollOrImmediate(selector, matcher)
	return result
}

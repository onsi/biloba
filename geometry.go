package biloba

import (
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

/*
Box is the viewport-relative layout rectangle of an element, returned by [Biloba.BoundingBox].  All
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
ScrollOffset is the scroll position of a scroll container, returned by [Biloba.ScrollOffset].  Top/Left
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
BoundingBox(selector) returns the viewport-relative layout [Box] of the first element matching selector.

BoundingBox polls by default: it waits until an element matching selector is present AND has a
non-degenerate layout box (width and height > 0 - i.e. actually laid out, not merely in the DOM), then
returns its rectangle.  This is the idiomatic replacement for hand-rolling getBoundingClientRect()
through [Biloba.Run]: readiness is folded in, so you never read a zero box mid-layout.

To assert on geometry that settles asynchronously, prefer the matcher form [Biloba.HaveBoundingBox] so
Gomega does the polling:

	Eventually(".hero .sec").Should(b.HaveBoundingBox(HaveField("Top", BeNumerically("<", 120))))

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) BoundingBox(selector any) Box {
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
		b.recordProbe(probeKey("BoundingBox", sel), result)
		return true, nil
	}).WithMessage("be present and laid out (have a non-degenerate bounding box)")
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
HaveBoundingBox(matcher) is the Gomega matcher counterpart of [Biloba.BoundingBox]: it passes once the
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
ScrollOffset(selector) returns the [ScrollOffset] of the first element matching selector (treated as a
scroll container).

ScrollOffset polls by default: it waits until an element matching selector is present, then reports its
scrollTop/scrollLeft and the maximum scrollable offsets.  Use it instead of reading scrollTop through
[Biloba.Run].  For assertions that settle asynchronously, prefer the matcher form [Biloba.HaveScrollOffset].

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) ScrollOffset(selector any) ScrollOffset {
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
		b.recordProbe(probeKey("ScrollOffset", sel), result)
		return true, nil
	}).WithMessage("be present (so its scroll offset can be read)")
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
HaveScrollOffset(matcher) is the Gomega matcher counterpart of [Biloba.ScrollOffset]: it passes once
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

// offsetWithin is the shared substrate behind OffsetTopWithin/OffsetLeftWithin: it polls until both
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
		b.recordProbe(probeKey("OffsetWithin:"+axis, sel), result)
		return true, nil
	}).WithMessage("be present and laid out within its container")
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
OffsetTopWithin(selector, container) returns how far the top of the first element matching selector sits
below the top of container - i.e. (element.top - container.top) in viewport coordinates.

This is the measurement a "scrolled near the top of the pane" spec actually wants.  It polls by default
until both elements are present and the element has a non-degenerate box.  To assert on a threshold that
settles asynchronously, prefer the matcher form [Biloba.HaveOffsetTopWithin]:

	Eventually(".hero .sec").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("<", 120)))

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) OffsetTopWithin(selector, container any) float64 {
	b.gt.Helper()
	return b.offsetWithin(selector, container, "top")
}

/*
OffsetLeftWithin(selector, container) is the horizontal sibling of [Biloba.OffsetTopWithin]: it returns
(element.left - container.left) in viewport coordinates.

Read https://onsi.github.io/biloba/#geometry to learn more about geometry getters
*/
func (b *Biloba) OffsetLeftWithin(selector, container any) float64 {
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

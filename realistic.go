package biloba

import (
	"fmt"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

/*
Realistic() returns a lightweight view of this tab whose DOM interactions are performed with *real* Chrome DevTools Protocol input instead of Biloba's fast atomic JavaScript simulations.  It is meant for the handful of specs that need to guard the realism Biloba otherwise trades away for speed (CLAUDE.md principle 2) - occlusion, scroll-into-view, and genuine CSS :hover.

Use it per-spec:

	rb := b.Realistic()
	rb.Click("#submit")                           // scrolls into view, refuses to click through an overlay, dispatches a real mouse click
	Eventually(".menu").Should(rb.Hover())        // moves the real mouse, activating CSS :hover

The returned *Biloba shares this tab's Chrome connection and state - it is the same tab, just with Click and Hover routed through CDP.  The default (non-realistic) tab is unchanged, so the bulk of your suite keeps Biloba's fast, atomic behavior.

What realistic mode does differently:
  - Click: scrolls the element to the viewport center, verifies it is enabled and is the topmost element at its centroid (so an occluding overlay or an off-screen element does NOT click through - it polls/fails like a real interaction), then dispatches a real mousePressed/mouseReleased at that point.
  - Hover: scrolls into view and moves the real mouse to the centroid, which activates genuine CSS :hover (Biloba's synthetic Hover does not).

What it does NOT change (yet): Type and SendKeys already use real CDP key events; SetValue keeps its value-set semantics (use Type for realistic text entry).  Realistic interactions cost real CDP round-trips and can reintroduce the timing sensitivity Biloba's atomic model avoids - that is the deliberate cost, which is why it is opt-in per spec.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Realistic() *Biloba {
	rb := *b
	rb.realistic = true
	return &rb
}

// clickPoint is the centroid + actionability snapshot returned by the scrollToAndPoint primitive.
type clickPoint struct {
	x, y       float64
	inViewport bool
	hittable   bool
	enabled    bool
}

// scrollToAndPoint scrolls the element matching selector to the viewport center and returns its
// centroid plus whether a real mouse event there would land on it.  A missing or hidden element
// surfaces as an error (so matcher callers keep polling); a present-but-unactionable element
// returns ok=true with the relevant clickPoint flags false.
func (b *Biloba) scrollToAndPoint(selector any) (clickPoint, error) {
	r := b.runBilobaHandler("scrollToAndPoint", selector)
	if r.Error() != nil {
		return clickPoint{}, r.Error()
	}
	m, ok := r.Result.(map[string]any)
	if !ok {
		return clickPoint{}, fmt.Errorf("unexpected scrollToAndPoint result: %v", r.Result)
	}
	return clickPoint{
		x:          toFloat64(m["x"]),
		y:          toFloat64(m["y"]),
		inViewport: m["inViewport"] == true,
		hittable:   m["hittable"] == true,
		enabled:    m["enabled"] == true,
	}, nil
}

// realisticClick implements Click for realistic mode.  It returns (true, nil) on a real click,
// (false, nil) when the element is present but not yet clickable (disabled, off-screen, or
// obscured - so matcher callers poll), and (false, err) on a hard error (missing/hidden element).
func (b *Biloba) realisticClick(selector any) (bool, error) {
	pt, err := b.scrollToAndPoint(selector)
	if err != nil {
		return false, err
	}
	if !pt.enabled || !pt.inViewport || !pt.hittable {
		return false, nil
	}
	// Move the real pointer to the element before pressing - so pointerover/pointermove/mousemove
	// fire and hover state is set - then click.  This matches how a real user arrives at and clicks
	// an element (Playwright does move->down->up) and makes hover-gated clicks behave correctly.
	if err := chromedp.Run(b.Context,
		chromedp.MouseEvent(input.MouseMoved, pt.x, pt.y),
		chromedp.MouseClickXY(pt.x, pt.y),
	); err != nil {
		return false, err
	}
	return true, nil
}

// realisticHover implements Hover for realistic mode: it scrolls into view and moves the real
// mouse to the element's centroid, activating genuine CSS :hover.
func (b *Biloba) realisticHover(selector any) (bool, error) {
	pt, err := b.scrollToAndPoint(selector)
	if err != nil {
		return false, err
	}
	if !pt.inViewport {
		return false, nil
	}
	if err := chromedp.Run(b.Context, chromedp.MouseEvent(input.MouseMoved, pt.x, pt.y)); err != nil {
		return false, err
	}
	return true, nil
}

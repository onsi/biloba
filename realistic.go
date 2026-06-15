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

The returned *Biloba shares this tab's Chrome connection and state - it is the same tab, just with its interactions routed through CDP.  The default (non-realistic) tab is unchanged, so the bulk of your suite keeps Biloba's fast, atomic behavior.

What realistic mode does differently:
  - Click: scrolls the element to the viewport center, waits for its box to stop moving, verifies it is enabled and is the topmost element at its centroid (so an occluding overlay or an off-screen element does NOT click through - it polls/fails like a real interaction), moves the real pointer to it, then dispatches a real mousePressed/mouseReleased.  Coordinates inside same-origin >>> iframes are translated to the top-level viewport.
  - ClickEach: clicks every matching element with real input (scrolling+re-measuring each in turn), skipping any that are hidden/disabled/off-screen/obscured.
  - Hover: scrolls into view and moves the real mouse to the centroid, which activates genuine CSS :hover (Biloba's synthetic Hover does not).
  - SetValue: text inputs are focused with a real click, cleared, and typed with real key events (then blurred to fire change); checkboxes are toggled with a real click. Native pickers - radio groups, <select>, multi-selects - fall back to the fast JS path, since they can't be driven by a real pointer (Playwright's selectOption sets them programmatically too).
  - Type/SendKeys: already use real CDP key events; realistic mode additionally scrolls the element into view before typing.

Realistic interactions cost real CDP round-trips and can reintroduce the timing sensitivity Biloba's atomic model avoids - that is the deliberate cost, which is why it is opt-in per spec.  (Focus stays a plain JS focus, matching how real engines focus elements without a side-effecting click.)

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Realistic() *Biloba {
	rb := *b
	rb.realistic = true
	return &rb
}

// clickPoint is the centroid + actionability snapshot returned by the measurePoint primitive.
type clickPoint struct {
	x, y       float64
	inViewport bool
	hittable   bool
	enabled    bool
}

// pointFromResult decodes a measurePoint object returned by the browser into a clickPoint.
func pointFromResult(result any) (clickPoint, bool) {
	m, ok := result.(map[string]any)
	if !ok {
		return clickPoint{}, false
	}
	return clickPoint{
		x:          toFloat64(m["x"]),
		y:          toFloat64(m["y"]),
		inViewport: m["inViewport"] == true,
		hittable:   m["hittable"] == true,
		enabled:    m["enabled"] == true,
	}, true
}

// scrollToStablePoint scrolls the element matching selector to the viewport center, waits for its
// box to stop moving, and returns its centroid plus whether a real mouse event there would land on
// it.  A missing or hidden element surfaces as an error (so matcher callers keep polling).
func (b *Biloba) scrollToStablePoint(selector any) (clickPoint, error) {
	r := b.runBilobaHandlerAsync("scrollToStablePoint", selector)
	if r.Error() != nil {
		return clickPoint{}, r.Error()
	}
	pt, ok := pointFromResult(r.Result)
	if !ok {
		return clickPoint{}, fmt.Errorf("unexpected scrollToStablePoint result: %v", r.Result)
	}
	return pt, nil
}

// realisticClickEach clicks every matching element with real CDP input, scrolling and re-measuring
// each one in turn (positions shift as earlier clicks mutate the page).  Elements that are missing,
// hidden, disabled, off-screen, or obscured are skipped - mirroring fast ClickEach, which clicks
// only the visible+enabled matches.
func (b *Biloba) realisticClickEach(selector any) error {
	count := b.runBilobaHandler("count", selector)
	if count.Error() != nil {
		return count.Error()
	}
	for i := 0; i < count.ResultInt(); i++ {
		r := b.runBilobaHandler("scrollToAndPointAt", selector, i)
		if r.Error() != nil {
			return r.Error()
		}
		pt, ok := pointFromResult(r.Result) // nil result (missing/hidden) => skip
		if !ok || !pt.enabled || !pt.inViewport || !pt.hittable {
			continue
		}
		if err := chromedp.Run(b.Context,
			chromedp.MouseEvent(input.MouseMoved, pt.x, pt.y),
			chromedp.MouseClickXY(pt.x, pt.y),
		); err != nil {
			return err
		}
	}
	return nil
}

// realisticClick implements Click for realistic mode.  It returns (true, nil) on a real click,
// (false, nil) when the element is present but not yet clickable (disabled, off-screen, or
// obscured - so matcher callers poll), and (false, err) on a hard error (missing/hidden element).
func (b *Biloba) realisticClick(selector any) (bool, error) {
	pt, err := b.scrollToStablePoint(selector)
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

// realisticDblClick implements DblClick for realistic mode.  Like realisticClick it scrolls the
// element to a stable point and checks actionability, then dispatches two real click sequences with
// an incrementing clickCount so Chrome fires a genuine dblclick (a single clickCount:2 release is
// what the renderer keys the dblclick off of).
func (b *Biloba) realisticDblClick(selector any) (bool, error) {
	pt, err := b.scrollToStablePoint(selector)
	if err != nil {
		return false, err
	}
	if !pt.enabled || !pt.inViewport || !pt.hittable {
		return false, nil
	}
	if err := chromedp.Run(b.Context,
		chromedp.MouseEvent(input.MouseMoved, pt.x, pt.y),
		chromedp.MouseClickXY(pt.x, pt.y, chromedp.ClickCount(1)),
		chromedp.MouseClickXY(pt.x, pt.y, chromedp.ClickCount(2)),
	); err != nil {
		return false, err
	}
	return true, nil
}

// realisticRightClick implements RightClick for realistic mode: a real right-button mouse click at
// the element's stable centroid, which makes Chrome fire a genuine contextmenu event.
func (b *Biloba) realisticRightClick(selector any) (bool, error) {
	pt, err := b.scrollToStablePoint(selector)
	if err != nil {
		return false, err
	}
	if !pt.enabled || !pt.inViewport || !pt.hittable {
		return false, nil
	}
	if err := chromedp.Run(b.Context,
		chromedp.MouseEvent(input.MouseMoved, pt.x, pt.y),
		chromedp.MouseClickXY(pt.x, pt.y, chromedp.ButtonType(input.Right)),
	); err != nil {
		return false, err
	}
	return true, nil
}

// realisticDragTo implements DragTo for realistic mode.  It measures stable, actionable points for
// both source and target (scroll-into-view + stability + occlusion, same as realisticClick), then
// drives a real CDP pointer drag: press at the source, several interpolated moves toward the target,
// and a release at the target.  This drives pointer-based drag-and-drop libraries; it does not drive
// native HTML5 draggable (use chromedp via b.Context for that).  Returns an error if either element
// is missing/hidden or is not actionable (off-screen, disabled, or obscured).
func (b *Biloba) realisticDragTo(source any, target any) error {
	src, err := b.scrollToStablePoint(source)
	if err != nil {
		return err
	}
	if !src.enabled || !src.inViewport || !src.hittable {
		return fmt.Errorf("source element is not actionable (it is disabled, off-screen, or obscured by another element)")
	}
	tgt, err := b.scrollToStablePoint(target)
	if err != nil {
		return err
	}
	if !tgt.enabled || !tgt.inViewport || !tgt.hittable {
		return fmt.Errorf("target element is not actionable (it is disabled, off-screen, or obscured by another element)")
	}
	actions := []chromedp.Action{
		chromedp.MouseEvent(input.MouseMoved, src.x, src.y),
		chromedp.MouseEvent(input.MousePressed, src.x, src.y, chromedp.ButtonType(input.Left), chromedp.ClickCount(1)),
	}
	steps := 5
	for i := 1; i <= steps; i++ {
		x := src.x + (tgt.x-src.x)*float64(i)/float64(steps)
		y := src.y + (tgt.y-src.y)*float64(i)/float64(steps)
		actions = append(actions, chromedp.MouseEvent(input.MouseMoved, x, y, chromedp.ButtonType(input.Left)))
	}
	actions = append(actions,
		chromedp.MouseEvent(input.MouseMoved, tgt.x, tgt.y, chromedp.ButtonType(input.Left)),
		chromedp.MouseEvent(input.MouseReleased, tgt.x, tgt.y, chromedp.ButtonType(input.Left), chromedp.ClickCount(1)),
	)
	return chromedp.Run(b.Context, actions...)
}

// realisticSetValue implements SetValue for realistic mode.  Text inputs are focused with a real
// click, cleared, typed with real CDP key events, then blurred to fire change (matching SetValue's
// value-set contract); checkboxes are toggled with a real click only when not already in the
// desired state.  Radio groups, <select>, and multi-selects fall back to the fast JS path - native
// pickers can't be driven by a real pointer (Playwright's selectOption sets them programmatically
// too).
func (b *Biloba) realisticSetValue(selector any, value any) (bool, error) {
	kind := b.runBilobaHandler("inputKind", selector)
	if kind.Error() != nil {
		return false, kind.Error()
	}
	switch kind.ResultString() {
	case "checkbox":
		desired, ok := value.(bool)
		if !ok {
			return false, fmt.Errorf("checkboxes only accept boolean values")
		}
		cur := b.runBilobaHandler("getValue", selector)
		if cur.Error() != nil {
			return false, cur.Error()
		}
		if cur.ResultBool() == desired {
			return true, nil // already in the desired state - nothing to click
		}
		return b.realisticClick(selector)
	case "text":
		ok, err := b.realisticClick(selector) // real click to focus
		if err != nil || !ok {
			return ok, err
		}
		if r := b.runBilobaHandler("setProperty", selector, "value", ""); r.Error() != nil {
			return false, r.Error()
		}
		if err := chromedp.Run(b.Context, chromedp.KeyEvent(toString(value))); err != nil {
			return false, err
		}
		if r := b.runBilobaHandler("blur", selector); r.Error() != nil {
			return false, r.Error()
		}
		return true, nil
	default: // radio, select, multi-select
		return b.runBilobaHandler("setValue", selector, value).MatcherResult()
	}
}

// realisticHover implements Hover for realistic mode: it scrolls into view and moves the real
// mouse to the element's centroid, activating genuine CSS :hover.
func (b *Biloba) realisticHover(selector any) (bool, error) {
	pt, err := b.scrollToStablePoint(selector)
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

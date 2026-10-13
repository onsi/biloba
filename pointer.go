package biloba

import (
	"context"
	"fmt"

	"github.com/chromedp/cdproto/input"
	"github.com/onsi/biloba/engine"
	"github.com/onsi/gomega/gcustom"
)

/*
Pointer is a handle on a real pointer - a mouse, or a pen with [Biloba.Pen] - that you drive one event at a time.  Get one with [Biloba.Pointer].

It remembers where it is and whether its primary button is down, so a gesture reads as the sequence of events it is:

	pen := b.Pointer(b.Pen())
	pen.MoveTo(100, 200)
	pen.Down()
	for i := 1; i <= 14; i++ {
		time.Sleep(16 * time.Millisecond)
		pen.MoveTo(100+float64(i)*10, 200)
	}
	pen.Up()

Every call dispatches real Chrome DevTools Protocol input immediately: there is no scroll-into-view, no stability wait, and (once the target is known) no polling, because the caller owns the timeline.  Moves while the button is down carry buttons=1, so the page sees a drag.

Read https://onsi.github.io/biloba/#driving-the-pointer-directly to learn more about driving the pointer
*/
type Pointer struct {
	b           *Biloba
	pointerType input.DispatchMouseEventPointerType
	// x, y are in the tab's viewport - where input lands - so Down and Up never re-translate them.
	x, y    float64
	pressed bool
}

/*
PointerHandleOption configures the pointer [Biloba.Pointer] returns.  Build one with [Biloba.Pen].

Read https://onsi.github.io/biloba/#driving-the-pointer-directly to learn more about driving the pointer
*/
type PointerHandleOption func(*Pointer)

/*
Pen() makes [Biloba.Pointer] return a pen: every event it dispatches reaches the page as a PointerEvent with pointerType "pen".  Chrome reports a pressure of 0.5 while the pen touches and 0 while it hovers.

	pen := b.Pointer(b.Pen())

Read https://onsi.github.io/biloba/#driving-the-pointer-directly to learn more about driving the pointer
*/
func (b *Biloba) Pen() PointerHandleOption {
	return func(p *Pointer) { p.pointerType = input.Pen }
}

/*
Pointer() returns a handle on a real pointer for driving custom gestures - drawing, scrubbing a slider - one event at a time.  It is a mouse unless you pass [Biloba.Pen]:

	p := b.Pointer()
	p.MoveToElement("#thumb")
	p.Down()
	p.MoveTo(300, 120)
	p.Up()

The pointer starts at (0, 0) and is not pressed.  On a frame handle its coordinates are in the frame's viewport.  If it is still pressed when the spec ends, Biloba releases it so the next spec does not inherit a held button.

Pointer itself accepts every poll knob: the handle keeps the view it was made from, and [Pointer.MoveToElement] polls with it (b.WithTimeout(d).Pointer()).  [Pointer.MoveTo], [Pointer.Down], and [Pointer.Up] dispatch at once and have nothing to tune.

Read https://onsi.github.io/biloba/#driving-the-pointer-directly to learn more about driving the pointer
*/
func (b *Biloba) Pointer(opts ...PointerHandleOption) *Pointer {
	p := &Pointer{b: b, pointerType: input.Mouse}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

/*
MoveTo moves the pointer to (x, y), in CSS pixels from the top-left of the viewport (the frame's viewport, on a frame handle).  It is one CDP round trip on a tab, and dispatches immediately.

Read https://onsi.github.io/biloba/#driving-the-pointer-directly to learn more about driving the pointer
*/
func (p *Pointer) MoveTo(x, y float64) {
	p.b.gt.Helper()
	x, y, err := p.b.toTabPointInPlace(x, y)
	if err != nil {
		p.b.gt.Fatalf("Pointer.MoveTo failed:\n%s", err.Error())
		return
	}
	p.move("Pointer.MoveTo", x, y)
}

/*
MoveToElement moves the pointer to the center of the first element matching selector, or to a point measured from its top-left corner with [Biloba.At]:

	p.MoveToElement("#track", b.At(10, 5))

It polls until the element exists and is visible (honoring the knobs of the view the pointer was made from), then dispatches at once.  It reads the element where it is now: it does not scroll, and a point outside the viewport fails rather than dispatching off-screen - scroll the element into view first.

Read https://onsi.github.io/biloba/#driving-the-pointer-directly to learn more about driving the pointer
*/
func (p *Pointer) MoveToElement(selector any, opts ...PointerOption) {
	p.b.gt.Helper()
	cfg := pointerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	var x, y float64
	matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := p.b.runBilobaHandler("pointerTarget", selector, cfg.encode())
		if r.Error() != nil {
			return false, r.Error()
		}
		point, ok := r.Result.(map[string]any)
		if !ok {
			return false, fmt.Errorf("unexpected pointerTarget result: %v", r.Result)
		}
		x, y = toFloat64(point["x"]), toFloat64(point["y"])
		return true, nil
	}).WithMessage("be an element the pointer can move to")
	if !p.b.pollOrImmediate(selector, matcher) {
		return
	}
	x, y, err := p.b.toTabPointInPlace(x, y)
	if err != nil {
		p.b.gt.Fatalf("Pointer.MoveToElement failed:\n%s", err.Error())
		return
	}
	p.move("Pointer.MoveToElement", x, y)
}

/*
Down presses the pointer's primary button where the pointer is.  Pressing a pointer that is already pressed fails the spec.

Read https://onsi.github.io/biloba/#driving-the-pointer-directly to learn more about driving the pointer
*/
func (p *Pointer) Down() {
	p.b.gt.Helper()
	if p.pressed {
		p.b.gt.Fatalf("Pointer.Down: the pointer is already pressed - call Up first")
		return
	}
	if err := p.dispatch(p.b, input.MousePressed); err != nil {
		p.b.gt.Fatalf("Pointer.Down failed:\n%s", err.Error())
		return
	}
	p.pressed = true
	p.b.gt.DeferCleanup(p.releaseAtSpecEnd)
}

/*
Up releases the pointer's primary button where the pointer is.  Releasing a pointer that is not pressed fails the spec.

Read https://onsi.github.io/biloba/#driving-the-pointer-directly to learn more about driving the pointer
*/
func (p *Pointer) Up() {
	p.b.gt.Helper()
	if !p.pressed {
		p.b.gt.Fatalf("Pointer.Up: the pointer is not pressed - call Down first")
		return
	}
	p.pressed = false
	if err := p.dispatch(p.b, input.MouseReleased); err != nil {
		p.b.gt.Fatalf("Pointer.Up failed:\n%s", err.Error())
	}
}

func (p *Pointer) move(method string, x, y float64) {
	p.b.gt.Helper()
	p.x, p.y = x, y
	if err := p.dispatch(p.b, input.MouseMoved); err != nil {
		p.b.gt.Fatalf("%s failed:\n%s", method, err.Error())
	}
}

func (p *Pointer) dispatch(on *Biloba, kind input.MouseType) error {
	return on.runEngine("dispatch pointer input", func(ctx context.Context) error {
		return engine.DispatchPointerContext(ctx, kind, p.x, p.y, p.pressed, p.pointerType)
	})
}

// releaseAtSpecEnd lets go of a pointer the spec left pressed.  It is best-effort: the page may be
// gone by now.  A same-process frame's input lands in its tab, so the release goes through the tab and
// still reaches Chrome after the frame's document has gone.
func (p *Pointer) releaseAtSpecEnd() {
	if !p.pressed {
		return
	}
	p.pressed = false
	on := p.b
	if on.frame != nil && !on.frame.outOfProcess {
		on = on.frame.owner
	}
	_ = p.dispatch(on, input.MouseReleased)
}

// toTabPointInPlace maps a point in this frame's viewport into its tab's viewport, through the iframe
// element wherever it is now - unlike toTabPoint it neither scrolls the iframe into view nor hit-tests,
// because the pointer dispatches exactly where it is told.  A tab's points, and an out-of-process
// frame's, pass through.
func (b *Biloba) toTabPointInPlace(x, y float64) (float64, float64, error) {
	if b.frame == nil || b.frame.outOfProcess {
		return x, y, nil
	}
	var point engine.Point
	err := b.runEngine("locate the frame in its tab", func(ctx context.Context) error {
		points, err := engine.TranslateFramePointsInPlaceContext(ctx, b.frame.id, []engine.Point{{X: x, Y: y}})
		if err != nil {
			return err
		}
		point = points[0]
		return nil
	})
	return point.X, point.Y, err
}

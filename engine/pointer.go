package engine

import (
	"context"
	"time"

	"github.com/chromedp/cdproto/input"
)

// PointerType is what a pointer's events report as PointerEvent.pointerType.
type PointerType string

const (
	MousePointer PointerType = "mouse"
	PenPointer   PointerType = "pen"
)

type pointerState struct {
	pointerType PointerType
	// x, y are in the viewport of the target that receives input - the tab's, for a same-process
	// frame - so Down and Up never translate them again.
	x, y    float64
	pressed bool
}

/*
SessionPointer drives one real pointer on a session, one event at a time - the engine side of the Go
client's Pointer handle.  Get one with Session.Pointer.

The session holds the pointer's state (position, whether it is pressed) under the caller's id, so a
handle can be rebuilt from the id alone: the daemon's clients keep only the id.  A pointer starts at
(0, 0), released.  Prepare releases a pointer the tab left pressed and forgets every pointer, and a
frame session releases its pressed pointers when it closes.

MoveTo, Down, and Up dispatch at once - one CDP command on a tab - with no scroll, no stability wait,
and no polling.  MoveToElement makes one attempt; a caller polls it.
*/
type SessionPointer struct {
	session     *Session
	id          uint64
	pointerType PointerType
}

// Pointer returns the pointer with this id on the session, creating its state on first use.
func (s *Session) Pointer(id uint64, pointerType PointerType) (*SessionPointer, error) {
	if pointerType != MousePointer && pointerType != PenPointer {
		return nil, invalidArgument("pointer", `pointer type must be "mouse" or "pen"`)
	}
	return &SessionPointer{session: s, id: id, pointerType: pointerType}, nil
}

// state returns the pointer's state, creating it if need be.  Callers hold s.mu (they run in serial).
func (p *SessionPointer) state() (*pointerState, error) {
	s := p.session
	if s.pointers == nil {
		s.pointers = map[uint64]*pointerState{}
	}
	state, ok := s.pointers[p.id]
	if !ok {
		state = &pointerState{pointerType: p.pointerType}
		s.pointers[p.id] = state
	} else if state.pointerType != p.pointerType {
		return nil, invalidArgument("pointer", "pointer is a "+string(state.pointerType)+", not a "+string(p.pointerType))
	}
	return state, nil
}

// MoveTo moves the pointer to (x, y), in CSS pixels from the top-left of the session's viewport (the
// frame's viewport, on a frame session).
func (p *SessionPointer) MoveTo(ctx context.Context, x, y float64) error {
	return p.session.serial(ctx, "pointer move", func(opCtx context.Context) error {
		return p.move(opCtx, x, y)
	})
}

// MoveToElement moves the pointer to the center of the first element matching selector, or to offset
// from its top-left corner.  It reads the element where it is, without scrolling: an element that is
// missing or hidden, or whose point is outside the viewport, is a retryable failure.
func (p *SessionPointer) MoveToElement(ctx context.Context, selector Selector, offset *Point) error {
	s := p.session
	if err := s.waitForDownloadSlot(ctx); err != nil {
		return contextError("pointer move to element", err)
	}
	return s.serial(ctx, "pointer move to element", func(opCtx context.Context) error {
		response, err := s.runHandler(opCtx, "pointerTarget", selector, pointerPayload(offset, 0))
		if err != nil {
			return err
		}
		point, ok := response.Result.(map[string]any)
		if !ok {
			return malformed("pointer move to element", response.Result)
		}
		x, xOK := number(point["x"])
		y, yOK := number(point["y"])
		if !xOK || !yOK {
			return malformed("pointer move to element", response.Result)
		}
		return p.move(opCtx, x, y)
	})
}

func (p *SessionPointer) move(ctx context.Context, x, y float64) error {
	state, err := p.state()
	if err != nil {
		return err
	}
	x, y, err = p.session.toInputPointInPlace(ctx, x, y)
	if err != nil {
		return err
	}
	state.x, state.y = x, y
	return dispatchPointer(ctx, state, input.MouseMoved)
}

// Down presses the pointer's primary button where it is.  Pressing a pressed pointer is an error.
func (p *SessionPointer) Down(ctx context.Context) error {
	return p.session.serial(ctx, "pointer down", func(opCtx context.Context) error {
		state, err := p.state()
		if err != nil {
			return err
		}
		if state.pressed {
			return invalidArgument("pointer down", "the pointer is already pressed - release it first")
		}
		if err := dispatchPointer(opCtx, state, input.MousePressed); err != nil {
			return err
		}
		state.pressed = true
		return nil
	})
}

// Up releases the pointer's primary button where it is.  Releasing a released pointer is an error.
func (p *SessionPointer) Up(ctx context.Context) error {
	return p.session.serial(ctx, "pointer up", func(opCtx context.Context) error {
		state, err := p.state()
		if err != nil {
			return err
		}
		if !state.pressed {
			return invalidArgument("pointer up", "the pointer is not pressed - press it first")
		}
		state.pressed = false
		return dispatchPointer(opCtx, state, input.MouseReleased)
	})
}

func dispatchPointer(ctx context.Context, state *pointerState, kind input.MouseType) error {
	return DispatchPointerContext(ctx, kind, state.x, state.y, state.pressed, input.DispatchMouseEventPointerType(state.pointerType))
}

// toInputPointInPlace maps a point in a same-process frame's viewport into its tab's, through the
// iframe element wherever it is now - without the scroll and hit test the realistic track does,
// because the pointer dispatches exactly where it is told.  A tab's points, and an out-of-process
// frame's, pass through.
func (s *Session) toInputPointInPlace(ctx context.Context, x, y float64) (float64, float64, error) {
	if s.frameID == "" || s.frameOOPIF {
		return x, y, nil
	}
	points, err := TranslateFramePointsInPlaceContext(ctx, s.frameID, []Point{{X: x, Y: y}})
	if err != nil {
		return 0, 0, err
	}
	return points[0].X, points[0].Y, nil
}

// releasePointers lets go of every pointer left pressed and forgets them all.  It is best-effort - the
// page may be gone - and costs nothing when no pointer was used.  Callers hold s.mu.
func (s *Session) releasePointers(ctx context.Context) {
	for _, state := range s.pointers {
		if state.pressed {
			state.pressed = false
			_ = dispatchPointer(ctx, state, input.MouseReleased)
		}
	}
	s.pointers = nil
}

// releasePointersOnClose releases a frame session's pressed pointers before the handle goes away.  A
// same-process frame's input lands in its tab, so the release still reaches Chrome after the frame's
// document has gone; s.ctx is the tab's target there.
func (s *Session) releasePointersOnClose() {
	if !s.frameTarget || len(s.pointers) == 0 {
		s.pointers = nil
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()
	s.releasePointers(ctx)
}

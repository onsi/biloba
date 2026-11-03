package engine

import (
	"context"
	"path/filepath"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// The browser interactions that cannot be simulated in JavaScript and so have to go over CDP:
// trusted keyboard and pointer input, a file input's file list, and the accessibility tree.  They
// live here rather than in the Ginkgo adapter for the same reason everything else does - the
// daemon needs them too, and two implementations of "dispatch a real click" would drift in exactly
// the ways that are hardest to notice.

// EmulateViewportContext resizes the tab's emulated viewport.  Options are chromedp's, deliberately:
// SetWindowSize takes them in its public signature, and Biloba does not hide chromedp from callers.
func EmulateViewportContext(ctx context.Context, width, height int, opts ...chromedp.EmulateViewportOption) error {
	return chromedp.Do(ctx, chromedp.EmulateViewport(int64(width), int64(height), opts...))
}

// EmulateViewportMatchingScreenContext keeps full headless Chrome's compositor surface aligned
// with its layout viewport, so trusted input is not clipped to Chrome's default 800x600 screen.
func EmulateViewportMatchingScreenContext(ctx context.Context, width, height int) error {
	matchScreen := func(metrics *emulation.SetDeviceMetricsOverrideParams, _ *emulation.SetTouchEmulationEnabledParams) {
		metrics.ScreenWidth = metrics.Width
		metrics.ScreenHeight = metrics.Height
	}
	return EmulateViewportContext(ctx, width, height, matchScreen)
}

// KeyEventContext dispatches keys as real keyboard events, so keydown/keypress/keyup all fire.
//
// A key sent with Ctrl, Alt, or Meta held dispatches keydown and keyup but no char event, so the
// page sees no keypress and no text is typed, which is what a shortcut does in Chrome and what
// chromedp.KeyEvent has done since v0.18.0.  Shift alone still gets its char event.  Alt is treated
// the same on every platform: the key encoding is a fixed US layout and does not model the
// characters Option produces on macOS, so to type one, send the resulting character.
func KeyEventContext(ctx context.Context, keys string, opts ...chromedp.KeyOption) error {
	if keys == "" {
		return nil
	}
	return chromedp.Do(ctx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		for _, r := range keys {
			for _, key := range kb.Encode(r) {
				for _, o := range opts {
					o(key)
				}
				if key.Type == kb.KeyChar && key.Modifiers&^int64(chromedp.ModifierShift) != 0 {
					continue
				}
				if _, err := cdp.Call(ctx, t, input.DispatchKeyEvent, *key); err != nil {
					return err
				}
			}
		}
		return nil
	}))
}

// MouseClickContext dispatches a press/release pair at a point, with modifiers held, repeated for
// each click count so a double click reports detail=1 then detail=2 the way a real one does.
func MouseClickContext(ctx context.Context, x, y float64, button input.MouseButton, clickCount int, modifiers chromedp.Modifier) error {
	actions := []chromedp.Action[chromedp.Void]{chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, x, y)}
	for count := int64(1); count <= int64(clickCount); count++ {
		for _, kind := range []input.DispatchMouseEventType{input.DispatchMouseEventTypeMousePressed, input.DispatchMouseEventTypeMouseReleased} {
			actions = append(actions, dispatchMouse(input.DispatchMouseEventParams{Type: kind, X: x, Y: y, Button: button, ClickCount: count, Modifiers: int64(modifiers)}))
		}
	}
	return chromedp.Do(ctx, actions...)
}

func dispatchMouse(params input.DispatchMouseEventParams) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, input.DispatchMouseEvent, params)
		return err
	})
}

func dispatchTouch(kind input.DispatchTouchEventType, points []*input.TouchPoint) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, input.DispatchTouchEvent, input.DispatchTouchEventParams{Type: kind, TouchPoints: points})
		return err
	})
}

// TapContext dispatches a touch start/end pair at a point.
func TapContext(ctx context.Context, x, y float64) error {
	return chromedp.Do(ctx,
		dispatchTouch(input.DispatchTouchEventTypeTouchStart, []*input.TouchPoint{{X: x, Y: y}}),
		dispatchTouch(input.DispatchTouchEventTypeTouchEnd, []*input.TouchPoint{}),
	)
}

// SetFileInputFilesContext points a file input at paths.  The browser forbids doing this from
// JavaScript, so it is one of the few interactions that must go over CDP: resolve the element to a
// remote object, then hand DOM.setFileInputFiles its object id.  Reports false when the selector
// matched nothing, so a caller can poll rather than fail.
func SetFileInputFilesContext(ctx context.Context, nodeScript string, paths []string) (bool, error) {
	// Chrome accepts a relative path and attaches the file, but the tab's next navigation then stalls
	// until it times out.  Resolve against the working directory ourselves, which is what Chrome
	// would have resolved against.
	absolute := make([]string, len(paths))
	for i, path := range paths {
		var err error
		if absolute[i], err = filepath.Abs(path); err != nil {
			return false, err
		}
	}
	paths = absolute
	found := false
	err := chromedp.Do(ctx, chromedp.Func(func(runCtx context.Context, t *chromedp.Target) error {
		evaluate := scopeEvaluation(ctx, withUserGesture(evaluateParams(nodeScript)))
		evaluated, evaluateErr := cdp.Call(runCtx, t, runtime.Evaluate, evaluate)
		if evaluateErr != nil {
			return evaluateErr
		}
		if evaluated.ExceptionDetails != nil {
			return &chromedp.ExceptionError{ExceptionDetails: evaluated.ExceptionDetails}
		}
		node := evaluated.Result
		if node == nil || node.ObjectID == "" {
			return nil
		}
		defer func() {
			_, _ = cdp.Call(runCtx, t, runtime.ReleaseObject, runtime.ReleaseObjectParams{ObjectID: node.ObjectID})
		}()
		if _, setErr := cdp.Call(runCtx, t, dom.SetFileInputFiles, dom.SetFileInputFilesParams{Files: paths, ObjectID: node.ObjectID}); setErr != nil {
			return setErr
		}
		found = true
		return nil
	}))
	return found, err
}

// AccessibilityTreeContext reads the full accessibility tree for the tab.
func AccessibilityTreeContext(ctx context.Context) ([]*accessibility.Node, error) {
	return accessibilityTreeContext(ctx, "")
}

func accessibilityTreeContext(ctx context.Context, frameID cdp.FrameID) ([]*accessibility.Node, error) {
	tree, err := chromedp.Call(ctx, accessibility.GetFullAXTree, accessibility.GetFullAXTreeParams{FrameID: frameID})
	if err != nil {
		return nil, err
	}
	return tree.Nodes, nil
}

// MouseMoveContext moves the real pointer, which is what activates genuine CSS :hover.
func MouseMoveContext(ctx context.Context, x, y float64) error {
	return chromedp.Do(ctx, chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, x, y))
}

// DispatchPointerContext dispatches one real pointer event - a move, a press, or a release of the
// primary button - at a point, as pointerType ("mouse" or "pen").  held reports whether the primary
// button is down once the event is over: a move while held carries buttons=1 so the page sees a drag,
// a press is always held, and a release never is.  A pen reports pressure 0.5 while it touches and 0
// otherwise, the way a mouse does; Chrome derives a pen's pressure from force, which defaults to 0.
func DispatchPointerContext(ctx context.Context, kind input.DispatchMouseEventType, x, y float64, held bool, pointerType input.DispatchMouseEventPointerType) error {
	held = kind == input.DispatchMouseEventTypeMousePressed || (kind == input.DispatchMouseEventTypeMouseMoved && held)
	event := input.DispatchMouseEventParams{Type: kind, X: x, Y: y, PointerType: pointerType}
	if kind != input.DispatchMouseEventTypeMouseMoved || held {
		event.Button = input.MouseButtonLeft
	}
	if kind != input.DispatchMouseEventTypeMouseMoved {
		event.ClickCount = 1
	}
	if held {
		event.Buttons = 1
		if pointerType == input.DispatchMouseEventPointerTypePen {
			event.Force = 0.5
		}
	}
	return chromedp.Do(ctx, dispatchMouse(event))
}

// ClickXYContext moves to a point and clicks it - the simple case, no modifiers or repeat counts.
func ClickXYContext(ctx context.Context, x, y float64) error {
	return chromedp.Do(ctx,
		chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, x, y),
		chromedp.MouseClickXY(x, y),
	)
}

// TouchTapContext enables touch emulation and dispatches a tap.  Emulation is enabled per call
// because a tap can be the first touch interaction a page ever sees.
func TouchTapContext(ctx context.Context, x, y float64) error {
	return chromedp.Do(ctx,
		chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
			_, err := cdp.Call(ctx, t, emulation.SetTouchEmulationEnabled, emulation.SetTouchEmulationEnabledParams{Enabled: true})
			return err
		}),
		dispatchTouch(input.DispatchTouchEventTypeTouchStart, []*input.TouchPoint{{X: x, Y: y}}),
		dispatchTouch(input.DispatchTouchEventTypeTouchEnd, []*input.TouchPoint{}),
	)
}

// ScrollWheelContext dispatches a real wheel event at a point.
func ScrollWheelContext(ctx context.Context, x, y, deltaX, deltaY float64) error {
	params := wheelEventParams{Type: input.DispatchMouseEventTypeMouseWheel, X: x, Y: y, DeltaX: deltaX, DeltaY: deltaY}
	return chromedp.Do(ctx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		return t.Call(ctx, input.CommandDispatchMouseEvent, params, nil)
	}))
}

// wheelEventParams is Input.dispatchMouseEvent for a wheel event with both deltas always on the wire.
// Chrome rejects a mouseWheel that lacks either one, and cdproto leaves a zero delta out, so a purely
// vertical scroll (deltaX 0) would never reach the page.
type wheelEventParams struct {
	Type   input.DispatchMouseEventType `json:"type"`
	X      float64                      `json:"x"`
	Y      float64                      `json:"y"`
	DeltaX float64                      `json:"deltaX"`
	DeltaY float64                      `json:"deltaY"`
}

// DragContext presses at src, moves to tgt in steps, and releases - the intermediate moves matter,
// because a drag implementation watching mousemove needs to see the path, not just the endpoints.
func DragContext(ctx context.Context, srcX, srcY, tgtX, tgtY float64, steps int) error {
	actions := []chromedp.Action[chromedp.Void]{
		chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, srcX, srcY),
		chromedp.MouseEvent(input.DispatchMouseEventTypeMousePressed, srcX, srcY, chromedp.ButtonType(input.MouseButtonLeft), chromedp.ClickCount(1)),
	}
	// Inclusive of steps: the last interpolated point already lands on the target, and the explicit
	// move below repeats it.  That duplicate is deliberate - it is what the Go API dispatched before
	// this moved, and a drag implementation watching mousemove can count events.
	for step := 1; step <= steps; step++ {
		x := srcX + (tgtX-srcX)*float64(step)/float64(steps)
		y := srcY + (tgtY-srcY)*float64(step)/float64(steps)
		actions = append(actions, chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, x, y, chromedp.ButtonType(input.MouseButtonLeft)))
	}
	actions = append(actions,
		chromedp.MouseEvent(input.DispatchMouseEventTypeMouseMoved, tgtX, tgtY, chromedp.ButtonType(input.MouseButtonLeft)),
		chromedp.MouseEvent(input.DispatchMouseEventTypeMouseReleased, tgtX, tgtY, chromedp.ButtonType(input.MouseButtonLeft), chromedp.ClickCount(1)),
	)
	return chromedp.Do(ctx, actions...)
}

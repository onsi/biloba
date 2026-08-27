package engine

import (
	"context"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// The browser interactions that cannot be simulated in JavaScript and so have to go over CDP:
// trusted keyboard and pointer input, a file input's file list, and the accessibility tree.  They
// live here rather than in the Ginkgo adapter for the same reason everything else does - the
// daemon needs them too, and two implementations of "dispatch a real click" would drift in exactly
// the ways that are hardest to notice.

// EmulateViewportContext resizes the tab's emulated viewport.  Options are chromedp's, deliberately:
// SetWindowSize takes them in its public signature, and Biloba does not hide chromedp from callers.
func EmulateViewportContext(ctx context.Context, width, height int, opts ...chromedp.EmulateViewportOption) error {
	return chromedp.Run(ctx, chromedp.EmulateViewport(int64(width), int64(height), opts...))
}

// KeyEventContext dispatches keys as real keyboard events, so keydown/keypress/keyup all fire.
func KeyEventContext(ctx context.Context, keys string, opts ...chromedp.KeyOption) error {
	if keys == "" {
		return nil
	}
	return chromedp.Run(ctx, chromedp.KeyEvent(keys, opts...))
}

// MouseClickContext dispatches a press/release pair at a point, with modifiers held, repeated for
// each click count so a double click reports detail=1 then detail=2 the way a real one does.
func MouseClickContext(ctx context.Context, x, y float64, button input.MouseButton, clickCount int, modifiers input.Modifier) error {
	actions := []chromedp.Action{chromedp.MouseEvent(input.MouseMoved, x, y)}
	for count := int64(1); count <= int64(clickCount); count++ {
		actions = append(actions,
			input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(button).WithClickCount(count).WithModifiers(modifiers),
			input.DispatchMouseEvent(input.MouseReleased, x, y).WithButton(button).WithClickCount(count).WithModifiers(modifiers),
		)
	}
	return chromedp.Run(ctx, actions...)
}

// TapContext dispatches a touch start/end pair at a point.
func TapContext(ctx context.Context, x, y float64) error {
	return chromedp.Run(ctx,
		input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: x, Y: y}}),
		input.DispatchTouchEvent(input.TouchEnd, []*input.TouchPoint{}),
	)
}

// SetFileInputFilesContext points a file input at paths.  The browser forbids doing this from
// JavaScript, so it is one of the few interactions that must go over CDP: resolve the element to a
// remote object, then hand DOM.setFileInputFiles its object id.  Reports false when the selector
// matched nothing, so a caller can poll rather than fail.
func SetFileInputFilesContext(ctx context.Context, nodeScript string, paths []string) (bool, error) {
	var node *runtime.RemoteObject
	if err := chromedp.Run(ctx, chromedp.Evaluate(nodeScript, &node)); err != nil {
		return false, err
	}
	if node == nil || node.ObjectID == "" {
		return false, nil
	}
	if err := chromedp.Run(ctx, dom.SetFileInputFiles(paths).WithObjectID(node.ObjectID)); err != nil {
		return false, err
	}
	return true, nil
}

// AccessibilityTreeContext reads the full accessibility tree for the tab.
func AccessibilityTreeContext(ctx context.Context) ([]*accessibility.Node, error) {
	var nodes []*accessibility.Node
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		var readErr error
		nodes, readErr = accessibility.GetFullAXTree().Do(runCtx)
		return readErr
	}))
	if err != nil {
		return nil, err
	}
	return nodes, nil
}

// MouseMoveContext moves the real pointer, which is what activates genuine CSS :hover.
func MouseMoveContext(ctx context.Context, x, y float64) error {
	return chromedp.Run(ctx, chromedp.MouseEvent(input.MouseMoved, x, y))
}

// ClickXYContext moves to a point and clicks it - the simple case, no modifiers or repeat counts.
func ClickXYContext(ctx context.Context, x, y float64) error {
	return chromedp.Run(ctx,
		chromedp.MouseEvent(input.MouseMoved, x, y),
		chromedp.MouseClickXY(x, y),
	)
}

// TouchTapContext enables touch emulation and dispatches a tap.  Emulation is enabled per call
// because a tap can be the first touch interaction a page ever sees.
func TouchTapContext(ctx context.Context, x, y float64) error {
	return chromedp.Run(ctx,
		emulation.SetTouchEmulationEnabled(true),
		input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: x, Y: y}}),
		input.DispatchTouchEvent(input.TouchEnd, []*input.TouchPoint{}),
	)
}

// ScrollWheelContext dispatches a real wheel event at a point.
func ScrollWheelContext(ctx context.Context, x, y, deltaX, deltaY float64) error {
	return chromedp.Run(ctx, input.DispatchMouseEvent(input.MouseWheel, x, y).WithDeltaX(deltaX).WithDeltaY(deltaY))
}

// DragContext presses at src, moves to tgt in steps, and releases - the intermediate moves matter,
// because a drag implementation watching mousemove needs to see the path, not just the endpoints.
func DragContext(ctx context.Context, srcX, srcY, tgtX, tgtY float64, steps int) error {
	actions := []chromedp.Action{
		chromedp.MouseEvent(input.MouseMoved, srcX, srcY),
		chromedp.MouseEvent(input.MousePressed, srcX, srcY, chromedp.ButtonType(input.Left), chromedp.ClickCount(1)),
	}
	// Inclusive of steps: the last interpolated point already lands on the target, and the explicit
	// move below repeats it.  That duplicate is deliberate - it is what the Go API dispatched before
	// this moved, and a drag implementation watching mousemove can count events.
	for step := 1; step <= steps; step++ {
		x := srcX + (tgtX-srcX)*float64(step)/float64(steps)
		y := srcY + (tgtY-srcY)*float64(step)/float64(steps)
		actions = append(actions, chromedp.MouseEvent(input.MouseMoved, x, y, chromedp.ButtonType(input.Left)))
	}
	actions = append(actions,
		chromedp.MouseEvent(input.MouseMoved, tgtX, tgtY, chromedp.ButtonType(input.Left)),
		chromedp.MouseEvent(input.MouseReleased, tgtX, tgtY, chromedp.ButtonType(input.Left), chromedp.ClickCount(1)),
	)
	return chromedp.Run(ctx, actions...)
}

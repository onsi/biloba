package engine

import (
	"context"
	"errors"
	"math"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// The helpers in this file act on a same-process frame through its tab's chromedp context, the way the
// other *Context helpers act on a tab. A frame Session calls them from its serialized operations; the Go
// runner calls them for its frame handles. Each takes a ctx scoped to the frame by ScopeToFrameContext,
// since the frame-local measurements they need run as JavaScript in the frame's document.

// TrackFrameWorlds records each frame document's page JavaScript context as its target announces it,
// which is what ScopeToFrameContext looks frames up in. Call it on a target's chromedp context before
// that context's first Run: Runtime.enable announces the existing contexts then.
func TrackFrameWorlds(ctx context.Context) context.Context { return trackFrameWorlds(ctx) }

// ScopeToFrameContext pins JavaScript evaluation in ctx (EvaluateContext, RunHandlerContext, and the
// helpers built on them) to frameID's current document. ctx must descend from a context passed to
// TrackFrameWorlds. Evaluation is pinned to the document rather than the frame: once the iframe is
// removed or navigated, evaluation fails and FrameContextGone reports it, rather than quietly running
// in whatever document replaced it.
func ScopeToFrameContext(ctx context.Context, frameID cdp.FrameID) (context.Context, error) {
	world, err := mainFrameWorld(ctx, frameID)
	if err != nil {
		return nil, err
	}
	return withExecutionContext(ctx, world), nil
}

// FrameContextGone reports whether err is Chrome refusing an evaluation because the document a
// ScopeToFrameContext context is pinned to no longer exists.
func FrameContextGone(err error) bool { return frameContextGone(err) }

// FrameInfo describes one frame document found by CrossOriginFrames.
type FrameInfo struct {
	ID       cdp.FrameID
	ParentID cdp.FrameID
	LoaderID cdp.LoaderID
	URL      string
	TargetID target.ID // the renderer target whose tree the frame's document is in
}

// TargetFrameTree is one renderer target's frame tree, as CrossOriginFrames walks it.
type TargetFrameTree struct {
	TargetID target.ID
	Tree     *page.FrameTree
	// OriginBoundary settles, in this target, whether a frame the tree cannot place on either side of
	// a cross-origin boundary can read its parent - see FrameOriginBoundaryContext.
	OriginBoundary func(cdp.FrameID) (bool, error)
}

// FrameTreeContext reads the frame tree of ctx's target.
func FrameTreeContext(ctx context.Context) (*page.FrameTree, error) {
	tree, err := getFrameTree(ctx)
	if err != nil {
		return nil, contextError("list frames", err)
	}
	return tree, nil
}

// CrossOriginFrames lists, in frame-tree order, the frames below scope that sit behind a cross-origin
// boundary, together with every frame nested below such a frame.  trees[0] is the tab's own renderer
// target, and scope defaults to its main frame.  The rest are the out-of-process frames below it, each
// in its own target; such a frame is a boundary by definition.  A frame's same-origin children are left
// out unless they sit below a boundary: the frame reaches them with >>>.  Session.Frames applies the
// same rule.
func CrossOriginFrames(trees []TargetFrameTree, scope cdp.FrameID) ([]FrameInfo, error) {
	type node struct {
		frame        *cdp.Frame
		tree         int
		outOfProcess bool
	}
	nodes := map[cdp.FrameID]node{}
	children := map[cdp.FrameID][]cdp.FrameID{}
	var add func(*page.FrameTree, int, bool)
	add = func(current *page.FrameTree, index int, root bool) {
		if _, seen := nodes[current.Frame.ID]; !seen {
			children[current.Frame.ParentID] = append(children[current.Frame.ParentID], current.Frame.ID)
		}
		// A parent may list an out-of-process child too; the child's own target has the real document.
		nodes[current.Frame.ID] = node{frame: current.Frame, tree: index, outOfProcess: root && index > 0}
		for _, child := range current.ChildFrames {
			add(child, index, false)
		}
	}
	for index, tree := range trees {
		if tree.Tree != nil {
			add(tree.Tree, index, true)
		}
	}
	if len(trees) == 0 || trees[0].Tree == nil {
		return nil, &Error{Code: CodeInvalidArgument, Operation: "list frames", Message: "no frame tree to search"}
	}
	if scope == "" {
		scope = trees[0].Tree.Frame.ID
	} else if _, found := nodes[scope]; !found {
		return nil, staleFrameError("list frames", scope)
	}
	frames := []FrameInfo{}
	var walk func(cdp.FrameID, bool) error
	walk = func(parentID cdp.FrameID, crossed bool) error {
		for _, id := range children[parentID] {
			child := nodes[id]
			boundary := crossed || child.outOfProcess || reportedCrossOrigin(child.frame, nodes[parentID].frame)
			if !boundary {
				var err error
				if boundary, err = trees[child.tree].OriginBoundary(id); err != nil {
					return err
				}
			}
			if boundary {
				frames = append(frames, FrameInfo{ID: id, ParentID: child.frame.ParentID, LoaderID: child.frame.LoaderID, URL: child.frame.URL, TargetID: trees[child.tree].TargetID})
			}
			if err := walk(id, boundary); err != nil {
				return err
			}
		}
		return nil
	}
	return frames, walk(scope, false)
}

// reportedCrossOrigin reports whether the frame tree alone puts child behind a cross-origin boundary:
// both origins are reported and they differ.  Chrome reports "://" for about:blank, srcdoc, and data:
// documents - the first two inherit their parent's origin, a data: document gets an opaque one - and a
// sandboxed document keeps its URL's origin in the tree while running in an opaque one.  Only the
// browser can settle those, so they go to FrameOriginBoundaryContext.
func reportedCrossOrigin(child, parent *cdp.Frame) bool {
	const unreported = "://"
	return child.SecurityOrigin != parent.SecurityOrigin && child.SecurityOrigin != unreported && parent.SecurityOrigin != unreported
}

// FrameOriginBoundaryContext asks the browser whether a frame in ctx's target can read its parent.
// CrossOriginFrames needs it for the frames reportedCrossOrigin cannot place.  The check runs in an
// isolated world so a page that replaces Window.parent cannot answer it.
func FrameOriginBoundaryContext(ctx context.Context, frameID cdp.FrameID) (bool, error) {
	var boundary bool
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		id, err := page.CreateIsolatedWorld(frameID).WithWorldName("biloba-origin-check-" + string(frameID)).Do(runCtx)
		if err != nil {
			return err
		}
		result, exception, err := runtime.Evaluate(`(() => { try { void parent.document; return false; } catch (error) { if (error.name === 'SecurityError') return true; throw error; } })()`).WithContextID(id).WithReturnByValue(true).Do(runCtx)
		if err != nil {
			return err
		}
		if exception != nil {
			return exception
		}
		boundary = string(result.Value) == "true"
		return nil
	}))
	if err != nil {
		return false, contextError("check frame origin", err)
	}
	return boundary, nil
}

// TranslateFramePointsContext maps points in a same-process frame's viewport into the viewport of the
// renderer target that hosts it, which is where trusted CDP input lands. The frame owner's content
// quad captures borders, parent scrolling, and CSS transforms; a projective mapping also handles
// perspective-transformed iframe elements. It scrolls the iframe element into view first.
func TranslateFramePointsContext(ctx context.Context, frameID cdp.FrameID, points []Point) ([]Point, error) {
	local := make([]actionPoint, len(points))
	for i, point := range points {
		local[i] = actionPoint{x: point.X, y: point.Y}
	}
	translated, err := translateFramePoints(ctx, frameID, local)
	if err != nil {
		return nil, err
	}
	out := make([]Point, len(translated))
	for i, point := range translated {
		out[i] = Point{X: point.x, Y: point.y}
	}
	return out, nil
}

func translateFramePoints(ctx context.Context, frameID cdp.FrameID, points []actionPoint) ([]actionPoint, error) {
	type viewport struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}
	var size viewport
	if err := EvaluateContext(ctx, `({width: window.innerWidth, height: window.innerHeight})`, false, &size); err != nil {
		return nil, err
	}
	if size.Width <= 0 || size.Height <= 0 {
		return nil, &Error{Code: CodeActionFailed, Operation: "translate frame point", Message: "frame viewport has no area"}
	}
	var quad dom.Quad
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		backendNodeID, _, ownerErr := dom.GetFrameOwner(frameID).Do(runCtx)
		if ownerErr != nil {
			return ownerErr
		}
		if scrollErr := dom.ScrollIntoViewIfNeeded().WithBackendNodeID(backendNodeID).Do(runCtx); scrollErr != nil {
			return scrollErr
		}
		model, modelErr := dom.GetBoxModel().WithBackendNodeID(backendNodeID).Do(runCtx)
		if modelErr != nil {
			return modelErr
		}
		quad = model.Content
		return nil
	}))
	if err != nil {
		return nil, contextError("translate frame point", err)
	}
	return mapPointsToQuad(points, size.Width, size.Height, quad)
}

// FramePointReachableContext reports whether trusted input at point, already translated into the
// hosting target's viewport, would land in frameID's document or one of its descendants. biloba.js
// checks occlusion only inside the frame, but a same-process frame's input is hit-tested against the
// whole tab, so an overlay in the embedding page would otherwise take the click.
func FramePointReachableContext(ctx context.Context, frameID cdp.FrameID, point Point) (bool, error) {
	return framePointReachable(ctx, frameID, actionPoint{x: point.X, y: point.Y})
}

func framePointReachable(ctx context.Context, frameID cdp.FrameID, point actionPoint) (bool, error) {
	var hitFrame cdp.FrameID
	var tree *page.FrameTree
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		var hitErr error
		_, hitFrame, _, hitErr = dom.GetNodeForLocation(int64(math.Round(point.x)), int64(math.Round(point.y))).WithIncludeUserAgentShadowDOM(true).Do(runCtx)
		if hitErr != nil || hitFrame == frameID {
			return hitErr
		}
		// The target may sit in a same-origin child of this frame, reached with >>>.
		tree, hitErr = page.GetFrameTree().Do(runCtx)
		return hitErr
	}))
	var protocolErr *cdproto.Error
	if errors.As(err, &protocolErr) && protocolErr.Message == "No node found at given location" {
		return false, nil
	}
	if err != nil {
		return false, contextError("hit-test frame point", err)
	}
	if hitFrame == frameID {
		return true, nil
	}
	own := findFrameTree(tree, frameID)
	return own != nil && findFrameTree(own, hitFrame) != nil, nil
}

// FrameScreenshotRectContext maps a rectangle in a same-process frame's document coordinates (what
// biloba.js's boundingBox and maskBoxes report) into the hosting target's document coordinates, which
// is the space Page.captureScreenshot clips in. A transformed frame yields the bounding box of the
// transformed rectangle.
func FrameScreenshotRectContext(ctx context.Context, frameID cdp.FrameID, x, y, width, height float64) (float64, float64, float64, float64, error) {
	var frameScroll struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := EvaluateContext(ctx, `({x: window.scrollX, y: window.scrollY})`, false, &frameScroll); err != nil {
		return 0, 0, 0, 0, contextError("translate screenshot rectangle", err)
	}

	left, top := x-frameScroll.X, y-frameScroll.Y
	points := [...]actionPoint{
		{x: left, y: top},
		{x: left + width, y: top},
		{x: left + width, y: top + height},
		{x: left, y: top + height},
	}
	translated, err := translateFramePoints(ctx, frameID, points[:])
	if err != nil {
		return 0, 0, 0, 0, err
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, point := range translated {
		minX, minY = math.Min(minX, point.x), math.Min(minY, point.y)
		maxX, maxY = math.Max(maxX, point.x), math.Max(maxY, point.y)
	}

	var pageX, pageY float64
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		_, _, _, _, viewport, _, metricsErr := page.GetLayoutMetrics().Do(runCtx)
		if metricsErr != nil {
			return metricsErr
		}
		if viewport == nil {
			return errors.New("Chrome did not report a visual viewport")
		}
		pageX, pageY = viewport.PageX, viewport.PageY
		return nil
	})); err != nil {
		return 0, 0, 0, 0, contextError("translate screenshot rectangle", err)
	}
	return minX + pageX, minY + pageY, maxX - minX, maxY - minY, nil
}

// FrameViewportClipContext is a same-process frame's page capture: the frame's viewport, in the hosting
// target's document coordinates. The tab composites the frame, so its document cannot be captured
// beyond the iframe that displays it.
func FrameViewportClipContext(ctx context.Context, frameID cdp.FrameID) (*page.Viewport, error) {
	var viewport struct {
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}
	if err := EvaluateContext(ctx, `({x: window.scrollX, y: window.scrollY, width: window.innerWidth, height: window.innerHeight})`, false, &viewport); err != nil {
		return nil, contextError("measure frame viewport", err)
	}
	x, y, width, height, err := FrameScreenshotRectContext(ctx, frameID, viewport.X, viewport.Y, viewport.Width, viewport.Height)
	if err != nil {
		return nil, err
	}
	return &page.Viewport{X: x, Y: y, Width: width, Height: height, Scale: 1}, nil
}

// AccessibilityTreeForFrameContext reads the accessibility tree of one frame's document.
func AccessibilityTreeForFrameContext(ctx context.Context, frameID cdp.FrameID) ([]*accessibility.Node, error) {
	return accessibilityTreeContext(ctx, frameID)
}

// mapPointsToQuad maps viewport coordinates onto a frame owner's content quad,
// including CSS perspective transforms. It does not access the browser.
func mapPointsToQuad(points []actionPoint, width, height float64, quad dom.Quad) ([]actionPoint, error) {
	if len(quad) != 8 {
		return nil, &Error{Code: CodeActionFailed, Operation: "translate frame point", Message: "frame owner has no content quad"}
	}
	x0, y0, x1, y1 := quad[0], quad[1], quad[2], quad[3]
	x2, y2, x3, y3 := quad[4], quad[5], quad[6], quad[7]
	sx, sy := x0-x1+x2-x3, y0-y1+y2-y3
	dx1, dx2 := x1-x2, x3-x2
	dy1, dy2 := y1-y2, y3-y2
	denominator := dx1*dy2 - dx2*dy1
	affine := math.Abs(sx) < 1e-9 && math.Abs(sy) < 1e-9
	if !affine && math.Abs(denominator) < 1e-9 {
		return nil, &Error{Code: CodeActionFailed, Operation: "translate frame point", Message: "frame owner transform is degenerate"}
	}
	g, h := 0.0, 0.0
	if !affine {
		g = (sx*dy2 - dx2*sy) / denominator
		h = (dx1*sy - sx*dy1) / denominator
	}
	a, b, c := x1-x0+g*x1, x3-x0+h*x3, x0
	d, e, f := y1-y0+g*y1, y3-y0+h*y3, y0
	translated := make([]actionPoint, len(points))
	for i, point := range points {
		u, v := point.x/width, point.y/height
		w := g*u + h*v + 1
		if math.Abs(w) < 1e-9 {
			return nil, &Error{Code: CodeActionFailed, Operation: "translate frame point", Message: "frame owner transform maps outside the viewport"}
		}
		translated[i] = actionPoint{x: (a*u + b*v + c) / w, y: (d*u + e*v + f) / w}
	}
	return translated, nil
}

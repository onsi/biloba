package biloba

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/onsi/biloba/engine"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
)

/*
A cross-origin iframe - a form served from another port, a payment widget from another site - keeps
its document out of reach of the page that embeds it, so neither `>>>` nor a Locator can see inside.
Biloba reaches it the other way round: it finds the frame through Chrome's frame tree and hands back
a frame handle, a *Biloba whose every DOM method, matcher, and JavaScript call runs in the frame's
own document.  Nothing about the browser's same-origin policy is relaxed; Biloba simply talks to the
frame directly, the way DevTools does.

A frame handle belongs to one document.  If the iframe is removed or navigated, or its tab
navigates, the handle's calls fail with frame_detached and you find the frame again.

A handle is also not a tab.  Methods that act on the tab - navigation, Prepare, window size, cookie
writes, network stubbing, dialogs, downloads - fail on a frame handle; call them on the tab.

Chrome may run a cross-site frame in a renderer target of its own (full Chrome does; the default
chrome-headless-shell doesn't).  Biloba attaches to that target and drives the frame there, so its
input needs no translation - but Chrome cannot screenshot it, and the tab's network stubs don't
reach it.
*/

// frameScope is what makes a *Biloba a frame handle: the tab whose renderer hosts the frame and the
// document the handle was found in.  It sits behind a pointer so the shallow clone-with-a-flag views
// (Realistic, WithTimeout, ...) share it.
type frameScope struct {
	owner    *Biloba // the canonical handle of the tab the frame is in
	id       cdp.FrameID
	loaderID cdp.LoaderID
	url      string // from the most recent discovery; guarded by root.lock
	// outOfProcess frames run in a renderer target of their own (Chrome's site isolation): their
	// document and their input live in that target, so nothing needs translating into the tab - and
	// Chrome cannot screenshot them directly.
	outOfProcess bool
}

// frameTarget is the root's attachment to one out-of-process frame's renderer target.  Every handle
// for a document in that target shares it; Prepare detaches it.
type frameTarget struct {
	ctx    context.Context
	detach func()
}

// frameHandleKey identifies one frame document.  A navigation keeps the frame ID and changes the
// loader ID, so a navigated frame gets a fresh handle rather than a stale one.
type frameHandleKey struct {
	target target.ID
	frame  cdp.FrameID
	loader cdp.LoaderID
}

// refusedOnFrame fails the spec when b is a frame handle and method acts on a tab or its browser
// context rather than a document.  A same-process frame shares its tab's renderer attachment, so those
// methods would otherwise silently act on the page that embeds the frame.  Callers return as soon as it
// refuses: under a failure-capturing GinkgoT the spec keeps running, and the refused call must not go
// on to navigate or stub the tab anyway.
func (b *Biloba) refusedOnFrame(method string) bool {
	if b.frame == nil {
		return false
	}
	b.gt.Helper()
	b.gt.Fatalf("%s acts on the tab, not a frame: call it on the tab that owns this frame", method)
	return true
}

/*
IsFrame() returns true if this *Biloba is a frame handle returned by [Biloba.Frame] or [Biloba.AllFrames] rather than a tab.

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
func (b *Biloba) IsFrame() bool {
	return b.frame != nil
}

// tab returns the canonical handle of the tab b belongs to: the owning tab for a frame handle, and the
// tab itself (not a view of it) otherwise.
func (b *Biloba) tab() *Biloba {
	if b.frame != nil {
		return b.frame.owner
	}
	if b.root == nil || b.targetID == b.root.targetID {
		return b.root
	}
	b.root.lock.Lock()
	defer b.root.lock.Unlock()
	if tab := b.root.tabs[b.targetID]; tab != nil {
		return tab
	}
	return b
}

/*
Frames represents a slice of frame handles (see [Biloba.AllFrames])

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
type Frames []*Biloba

/*
Find returns the first frame matching the passed-in FrameQuery (see [Biloba.FrameMatching]), or nil if none match:

	checkout := b.AllFrames().Find(b.FrameMatching().WithURL(ContainSubstring("/checkout")))

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
func (f Frames) Find(query *FrameQuery) *Biloba {
	for _, frame := range f {
		if query.matches(frame) {
			return frame
		}
	}
	return nil
}

/*
Filter returns all frames matching the passed-in FrameQuery (see [Biloba.FrameMatching])

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
func (f Frames) Filter(query *FrameQuery) Frames {
	out := Frames{}
	for _, frame := range f {
		if query.matches(frame) {
			out = append(out, frame)
		}
	}
	return out
}

/*
FrameQuery is a chainable query over the cross-origin frames below a tab or frame.  Like [TabQuery] a single value plays two roles:

  - a Gomega matcher you poll against a tab or frame - read it as [Biloba.HaveFrame] - and
  - a predicate you pass to [Biloba.Frame], [Frames.Find], and [Frames.Filter] - read it as [Biloba.FrameMatching].

Refine it with WithURL, WithTitle, and WithDOMElement.  Every refinement applies to the same frame.

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
type FrameQuery struct {
	titleMatcher  types.GomegaMatcher
	urlMatcher    types.GomegaMatcher
	hasDOMElement bool
	domSelector   any

	observed Frames
	found    *Biloba
}

/*
FrameMatching() returns a [FrameQuery].  Refine it with WithURL/WithTitle/WithDOMElement and hand it to [Biloba.Frame]:

	checkout := b.Frame(b.FrameMatching().WithURL(ContainSubstring("/checkout")))

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
func (b *Biloba) FrameMatching() *FrameQuery {
	return &FrameQuery{}
}

/*
HaveFrame() returns a [FrameQuery] that reads as an assertion.  Poll it against a tab (or frame) to wait for a matching cross-origin frame below it:

	Eventually(b).Should(b.HaveFrame().WithURL(ContainSubstring("/checkout")))

To wait for a frame and then use it, prefer [Biloba.Frame], which polls and returns the frame in one step.

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
func (b *Biloba) HaveFrame() *FrameQuery {
	return &FrameQuery{}
}

/*
WithURL() refines the [FrameQuery] to also require the frame's URL to match.  url may be a string (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
func (q *FrameQuery) WithURL(url any) *FrameQuery {
	out := *q
	out.urlMatcher = matcherOrEqual(url)
	return &out
}

/*
WithTitle() refines the [FrameQuery] to also require the frame document's title to match.  title may be a string (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
func (q *FrameQuery) WithTitle(title any) *FrameQuery {
	out := *q
	out.titleMatcher = matcherOrEqual(title)
	return &out
}

/*
WithDOMElement() refines the [FrameQuery] to also require the frame's document to have at least one element matching selector.  It is the usual way to wait for a frame that is ready to use, not merely present:

	form := b.Frame(b.FrameMatching().WithDOMElement(`input[name="email"]`))

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
func (q *FrameQuery) WithDOMElement(selector any) *FrameQuery {
	out := *q
	out.hasDOMElement = true
	out.domSelector = selector
	return &out
}

// matches is the predicate role: does this single frame satisfy every constraint?  A frame whose
// document fails to answer does not match - it may be mid-load, and a poll will ask again.
func (q *FrameQuery) matches(frame *Biloba) bool {
	if q.urlMatcher != nil {
		if match, _ := q.urlMatcher.Match(frame.frameURL()); !match {
			return false
		}
	}
	if q.titleMatcher != nil {
		title, err := frame.title()
		if err != nil {
			return false
		}
		if match, _ := q.titleMatcher.Match(title); !match {
			return false
		}
	}
	if q.hasDOMElement {
		r := frame.runBilobaHandler("exists", q.domSelector)
		if r.Error() != nil || !r.Success {
			return false
		}
	}
	return true
}

// Match is the Gomega matcher role: does the tab or frame passed in have a matching frame below it?
func (q *FrameQuery) Match(actual any) (bool, error) {
	parent, ok := actual.(*Biloba)
	if !ok {
		return false, fmt.Errorf("HaveFrame must be passed a Biloba tab or frame.  Got:\n%s", format.Object(actual, 1))
	}
	frames, err := parent.discoverFrames()
	if err != nil {
		if parent.frameValidationStopsPolling(err) {
			return false, gomega.StopTrying(err.Error())
		}
		return false, err
	}
	q.observed = frames
	q.found = frames.Find(q)
	return q.found != nil, nil
}

func (q *FrameQuery) description() string {
	clauses := []string{}
	if q.urlMatcher != nil {
		clauses = append(clauses, fmt.Sprintf("URL matching %s", q.urlMatcher.FailureMessage("")))
	}
	if q.titleMatcher != nil {
		clauses = append(clauses, fmt.Sprintf("Title matching %s", q.titleMatcher.FailureMessage("")))
	}
	if q.hasDOMElement {
		clauses = append(clauses, fmt.Sprintf("a DOM element matching %v", q.domSelector))
	}
	if len(clauses) == 0 {
		return "have a cross-origin frame"
	}
	return normalizeWhitespace("have a cross-origin frame with " + strings.Join(clauses, "\nand "))
}

func (q *FrameQuery) presentFrames() string {
	if len(q.observed) == 0 {
		return "There were no cross-origin frames to search."
	}
	out := &strings.Builder{}
	out.WriteString("The frames that were searched were:")
	for _, frame := range q.observed {
		title, _ := frame.title()
		fmt.Fprintf(out, "\n%s (%s)", frame.frameURL(), title)
	}
	return out.String()
}

func (q *FrameQuery) FailureMessage(actual any) string {
	return fmt.Sprintf("Expected to %s.\n%s", q.description(), q.presentFrames())
}

func (q *FrameQuery) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf("Expected not to %s, but there was one.", q.description())
}

/*
Frame(query) polls until a cross-origin frame below this tab (or frame) matches query, then returns a handle to it.  The handle is a *Biloba scoped to the frame's document, so the whole DOM and JavaScript API works through it:

	checkout := b.Frame(b.FrameMatching().WithURL(ContainSubstring("/checkout")).WithDOMElement("#card-number"))
	checkout.SetValue("#card-number", "4242424242424242")
	checkout.Click("#pay")
	Eventually("#receipt").Should(checkout.HaveInnerText(ContainSubstring("Paid")))

Frame polls like any other Biloba action, so it honors [Biloba.WithTimeout], [Biloba.WithPolling], [Biloba.WithContext], and [Biloba.Immediate], and fails the spec when no frame matches in time.  Same-origin iframes are not frames in this sense: reach into them with the `>>>` selector combinator instead.

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
func (b *Biloba) Frame(query *FrameQuery) *Biloba {
	b.gt.Helper()
	q := *query
	if !b.pollOrImmediate(b, &q) {
		return nil
	}
	return q.found
}

/*
AllFrames() returns a handle for every cross-origin frame below this tab (or frame) right now.  It is a snapshot and does not poll - to wait for a frame use [Biloba.Frame].

AllFrames includes every frame nested inside a cross-origin frame, except that below a frame it leaves out that frame's same-origin children: the frame reaches those with `>>>`.

Read https://onsi.github.io/biloba/#working-with-cross-origin-iframes to learn more about frames
*/
func (b *Biloba) AllFrames() Frames {
	b.gt.Helper()
	b.guardConfig("AllFrames")
	frames, err := b.discoverFrames()
	if err != nil {
		b.gt.Fatalf("Failed to list frames:\n%s", err.Error())
	}
	return frames
}

// discoverFrames lists the cross-origin frames below b, in the tab's renderer and in the out-of-process
// frame targets below it.
func (b *Biloba) discoverFrames() (Frames, error) {
	if err := b.validateFrameDocument("list frames"); err != nil {
		return nil, err
	}
	owner := b.tab()
	var scope cdp.FrameID
	if b.frame != nil {
		scope = b.frame.id
	}
	trees, err := owner.frameTrees()
	if err != nil {
		return nil, err
	}
	infos, err := engine.CrossOriginFrames(trees, scope)
	if err != nil {
		return nil, err
	}
	frames := Frames{}
	for _, info := range infos {
		// nil while the document has no page context yet: it is still loading, and a poll will ask again
		if frame := owner.frameHandle(info); frame != nil {
			frames = append(frames, frame)
		}
	}
	if err := b.validateFrameDocument("list frames"); err != nil {
		return nil, err
	}
	return frames, nil
}

// validateFrameDocument makes operations whose CDP command is addressed by frame ID prove that the
// handle's original execution context still exists first. Chrome reuses a frame ID across
// navigations, while the scoped context belongs to exactly one document.
func (b *Biloba) validateFrameDocument(what string) error {
	if b.frame == nil {
		return nil
	}
	if err := b.validateFrameObservation(); err != nil {
		return err
	}
	err := b.runEngine(what, func(ctx context.Context) error {
		return engine.EvaluateContext(ctx, "undefined", false, nil)
	})
	if engine.FrameContextGone(err) {
		// Only this pinned evaluation proves that the subject document is gone.
		// A similar error while inspecting a child is a discovery race worth retrying.
		return &engine.Error{Code: engine.CodeFrameDetached, Message: err.Error(), Cause: err}
	}
	return err
}

// validateFrameObservation proves that a frame handle still belongs to its original document from
// the execution-context registry maintained by CDP events. It sends no renderer command, so request
// and in-flight observations remain available while the frame's JavaScript thread is busy.
func (b *Biloba) validateFrameObservation() error {
	if b.frame == nil {
		return nil
	}
	err := engine.ValidateFrameWorldContext(b.Context, b.frame.id)
	if err != nil && b.frameValidationStopsPolling(err) {
		return fmt.Errorf("frame_detached: %w", err)
	}
	return err
}

// frameValidationStopsPolling reports the document-lifetime failures a retry cannot repair. Other
// validation failures are transient and stay ordinary matcher errors so a later poll can recover.
func (b *Biloba) frameValidationStopsPolling(err error) bool {
	var engineErr *engine.Error
	return (errors.As(err, &engineErr) && engineErr.Code == engine.CodeFrameDetached) ||
		b.Context.Err() != nil
}

// frameTrees reads this tab's frame tree and the trees of the out-of-process frame targets below it,
// for engine.CrossOriginFrames.  A frame target that stops answering - its frame navigated to another
// site, or went away - is detached and left out; the next search attaches to whatever replaced it.
func (b *Biloba) frameTrees() ([]engine.TargetFrameTree, error) {
	// Each target is read through its own chromedp context, under the same backstop as any command.
	on := func(base context.Context, what string, run func(context.Context) error) error {
		ctx, cancel := context.WithTimeout(base, cdpTimeout)
		defer cancel()
		return b.runEngineIn(ctx, cdpTimeout, what, run)
	}
	read := func(targetID target.ID, base context.Context) (engine.TargetFrameTree, error) {
		var tree *page.FrameTree
		err := on(base, "list the page's frames", func(ctx context.Context) error {
			var err error
			tree, err = engine.FrameTreeContext(ctx)
			return err
		})
		return engine.TargetFrameTree{TargetID: targetID, Tree: tree, OriginBoundary: func(id cdp.FrameID) (bool, error) {
			var boundary bool
			err := on(base, "check a frame's origin", func(ctx context.Context) error {
				var err error
				boundary, err = engine.FrameOriginBoundaryContext(ctx, id)
				return err
			})
			return boundary, err
		}}, err
	}
	own, err := read(b.targetID, b.Context)
	if err != nil {
		return nil, err
	}
	trees := []engine.TargetFrameTree{own}
	for _, id := range b.outOfProcessFrameTargets() {
		targetCtx, err := b.frameTargetContext(id)
		if err != nil {
			continue
		}
		tree, err := read(id, targetCtx)
		if err != nil {
			b.detachFrameTarget(id)
			continue
		}
		trees = append(trees, tree)
	}
	return trees, nil
}

// outOfProcessFrameTargets lists the iframe targets below this tab, parents before children.
func (b *Biloba) outOfProcessFrameTargets() []target.ID {
	infos, err := chromedp.Targets(b.root.Context)
	if err != nil {
		return nil
	}
	below := map[target.ID]bool{b.targetID: true}
	out := []target.ID{}
	for added := true; added; {
		added = false
		for _, info := range infos {
			if info.Type != "iframe" || below[info.TargetID] || !below[info.ParentID] || info.BrowserContextID != b.browserContextID {
				continue
			}
			below[info.TargetID] = true
			out = append(out, info.TargetID)
			added = true
		}
	}
	return out
}

// frameTargetContext returns the root's attachment to an out-of-process frame target, attaching on
// first use.  The attachment lives under the tab that embeds the frame.  It streams the frame's
// console output the way the tab's own listener streams its same-process frames'.
func (b *Biloba) frameTargetContext(id target.ID) (context.Context, error) {
	root := b.root
	root.lock.Lock()
	if existing := root.frameTargets[id]; existing != nil && existing.ctx.Err() == nil {
		root.lock.Unlock()
		return existing.ctx, nil
	}
	root.lock.Unlock()
	attachCtx, cancel := context.WithTimeout(context.Background(), tabAttachTimeout)
	defer cancel()
	ctx, detach, err := engine.AttachFrameTargetContext(attachCtx, b.Context, id)
	if err != nil {
		return nil, err
	}
	root.lock.Lock()
	if existing := root.frameTargets[id]; existing != nil && existing.ctx.Err() == nil {
		root.lock.Unlock()
		detach()
		return existing.ctx, nil
	}
	root.frameTargets[id] = &frameTarget{ctx: ctx, detach: detach}
	root.lock.Unlock()
	chromedp.ListenTarget(ctx, func(ev any) {
		if console, ok := ev.(*runtime.EventConsoleAPICalled); ok {
			b.handleEventConsoleAPICalled(console)
		}
	})
	return ctx, nil
}

func (b *Biloba) detachFrameTarget(id target.ID) {
	root := b.root
	root.lock.Lock()
	attached := root.frameTargets[id]
	delete(root.frameTargets, id)
	root.lock.Unlock()
	if attached != nil {
		attached.detach()
	}
}

// frameHandle returns the handle for one frame document hosted by this tab, creating it on first
// sight.  Handles are cached on the root so a polling search does not mint a new one - and a new
// request listener - every attempt; Prepare discards them.  It returns nil while the document has no
// page context to scope to.
func (b *Biloba) frameHandle(info engine.FrameInfo) *Biloba {
	root := b.root
	key := frameHandleKey{target: info.TargetID, frame: info.ID, loader: info.LoaderID}
	root.lock.Lock()
	if existing := root.frames[key]; existing != nil {
		existing.frame.url = info.URL
		root.lock.Unlock()
		return existing
	}
	root.lock.Unlock()

	// A frame in this tab's renderer is reached through the tab's own chromedp context; an
	// out-of-process frame through the root's attachment to its target.
	base, outOfProcess := b.Context, info.TargetID != b.targetID
	if outOfProcess {
		var err error
		if base, err = b.frameTargetContext(info.TargetID); err != nil {
			return nil
		}
	}
	frameCtx, cancel := context.WithCancel(base)
	scoped, err := engine.ScopeToFrameContext(frameCtx, info.ID)
	if err != nil {
		cancel()
		return nil
	}
	frame := newBiloba(b.gt)
	frame.Context = scoped
	frame.ChromeConnection = b.ChromeConnection
	frame.root = root
	frame.targetID = info.TargetID
	frame.browserContextID = b.browserContextID
	frame.downloadDir = b.downloadDir
	frame.pollTrajectory = root.pollTrajectory
	// A frame's diagnostics belong to the page it sits in: its polls and occluded clicks render with its
	// tab's failure artifacts, and a colour-scheme emulation it applies is its tab's to clear.
	frame.probes = b.probes
	frame.occlusions = b.occlusions
	frame.colorSchemeEmulated = b.colorSchemeEmulated
	frame.close = cancel
	frame.frame = &frameScope{owner: b, id: info.ID, loaderID: info.LoaderID, url: info.URL, outOfProcess: outOfProcess}

	root.lock.Lock()
	if existing := root.frames[key]; existing != nil {
		root.lock.Unlock()
		cancel()
		return existing
	}
	root.frames[key] = frame
	root.lock.Unlock()
	frame.listenForFrameRequests()
	return frame
}

// listenForFrameRequests records the requests this frame's document makes, so AllRequests,
// HaveMadeRequest, and BeNetworkIdle on a frame handle are about the frame.  The tab records them
// too: it sees every request its renderer makes.
func (b *Biloba) listenForFrameRequests() {
	id := b.frame.id
	loaderID := b.frame.loaderID
	chromedp.ListenTarget(b.Context, func(ev any) {
		switch ev := ev.(type) {
		case *network.EventRequestWillBeSent:
			if ev.FrameID == id && ev.LoaderID == loaderID {
				b.handleEventRequestWillBeSent(ev)
			}
		case *network.EventLoadingFinished:
			b.handleEventLoadingFinished(ev)
		case *network.EventLoadingFailed:
			b.handleEventLoadingFailed(ev)
		}
	})
}

// frameURL is the frame's URL as of the most recent discovery.
func (b *Biloba) frameURL() string {
	b.root.lock.Lock()
	defer b.root.lock.Unlock()
	return b.frame.url
}

// detachFrames discards every frame handle and frame-target attachment.  Prepare calls it: the
// documents they belong to are about to go away with the navigation to about:blank.
func (b *Biloba) detachFrames() {
	b.lock.Lock()
	frames, targets := b.frames, b.frameTargets
	b.frames, b.frameTargets = map[frameHandleKey]*Biloba{}, map[target.ID]*frameTarget{}
	b.lock.Unlock()
	for _, frame := range frames {
		frame.close()
	}
	for _, attached := range targets {
		attached.detach()
	}
}

// closeFrame detaches one frame handle; it is Close for a frame.
func (b *Biloba) closeFrame() {
	root := b.root
	root.lock.Lock()
	delete(root.frames, frameHandleKey{target: b.targetID, frame: b.frame.id, loader: b.frame.loaderID})
	root.lock.Unlock()
	b.close()
}

// toTabPoint maps a point a biloba.js measurement reported in this frame's viewport into the tab's
// viewport, where trusted input lands, and reports whether input there would reach the frame rather
// than something in the embedding page covering it.  A tab's points pass through.
//
// An out-of-process frame receives input in its own target, at its own coordinates, so its points pass
// through too - and Biloba cannot see what the embedding page draws over it.
func (b *Biloba) toTabPoint(x, y float64) (float64, float64, bool, error) {
	if b.frame == nil || b.frame.outOfProcess {
		return x, y, true, nil
	}
	var point engine.Point
	var reachable bool
	err := b.runEngine("locate the frame in its tab", func(ctx context.Context) error {
		points, err := engine.TranslateFramePointsContext(ctx, b.frame.id, []engine.Point{{X: x, Y: y}})
		if err != nil {
			return err
		}
		point = points[0]
		reachable, err = engine.FramePointReachableContext(ctx, b.frame.id, point)
		return err
	})
	return point.X, point.Y, reachable, err
}

// toTabClip maps a rectangle in this frame's document coordinates into its tab's document
// coordinates, the space Page.captureScreenshot clips in.  A tab's rectangles pass through.
func (b *Biloba) toTabClip(ctx context.Context, x, y, width, height float64) (*page.Viewport, error) {
	if err := b.refuseOutOfProcessCapture(); err != nil {
		return nil, err
	}
	if b.frame != nil {
		var err error
		if x, y, width, height, err = engine.FrameScreenshotRectContext(ctx, b.frame.id, x, y, width, height); err != nil {
			return nil, err
		}
	}
	return &page.Viewport{X: x, Y: y, Width: width, Height: height, Scale: 1}, nil
}

// captureFrameViewport is a frame's whole-page capture: what the frame shows, clipped out of the tab
// that composites it.
func (b *Biloba) captureFrameViewport(ctx context.Context) ([]byte, *page.Viewport, error) {
	if err := b.refuseOutOfProcessCapture(); err != nil {
		return nil, nil, err
	}
	clip, err := engine.FrameViewportClipContext(ctx, b.frame.id)
	if err != nil {
		return nil, nil, err
	}
	img, err := engine.CaptureClipContext(ctx, clip, false)
	return img, clip, err
}

// refuseOutOfProcessCapture explains, instead of passing on Chrome's "Command can only be executed on
// top-level targets", why an out-of-process frame cannot be screenshotted - and what to do instead.
func (b *Biloba) refuseOutOfProcessCapture() error {
	if b.frame == nil || !b.frame.outOfProcess {
		return nil
	}
	return fmt.Errorf("this frame runs in a renderer process of its own, and Chrome can only screenshot a tab: capture the iframe element from the tab instead (b.CaptureScreenshotOf(\"iframe...\"), or Eventually(\"iframe...\").Should(b.HaveScreenshot(...)))")
}

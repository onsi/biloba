package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// FrameQuery selects a cross-origin iframe by its metadata and document state.
type FrameQuery struct {
	Title      *Expectation
	URL        *Expectation
	HasElement *Selector
}

// Frame is a DOM-capable frame-scoped Session. Out-of-process frames use their own target;
// all frame operations use the selected document's normal JavaScript context.
type Frame struct {
	*Session
	url string
}

// URL returns the frame URL observed when the handle was discovered.
func (f *Frame) URL() string { return f.url }

// FrameID identifies the child frame represented by this handle.
func (f *Frame) FrameID() cdp.FrameID { return f.Session.FrameID() }

// Close detaches this handle without removing the iframe from its parent document.
func (f *Frame) Close() error { return f.Session.Close() }

type frameDescriptor struct {
	frame    *cdp.Frame
	targetID target.ID
	oopif    bool
}

// Frames returns handles for every cross-origin iframe below this session.
func (s *Session) Frames(ctx context.Context) ([]*Frame, error) {
	descriptors, err := s.frameDescriptors(ctx)
	if err != nil {
		return nil, err
	}
	frames := make([]*Frame, 0, len(descriptors))
	created := make([]*Frame, 0, len(descriptors))
	for _, descriptor := range descriptors {
		frame, wasCreated, attachErr := s.attachFrame(ctx, descriptor)
		if attachErr != nil {
			for _, attached := range created {
				_ = attached.Close()
			}
			return nil, attachErr
		}
		if wasCreated {
			created = append(created, frame)
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

// WaitForFrame discovers a matching cross-origin frame and waits for its document predicate.
// When several frames match, it returns a ready match; renderer response order can vary.
func (s *Session) WaitForFrame(ctx context.Context, query FrameQuery, policy PollPolicy) (*Frame, error) {
	var matched *Frame
	shouldReturnCandidateError := func(attemptCtx context.Context, err error) bool {
		if attemptCtx.Err() != nil {
			return true
		}
		if !IsFatal(err) {
			return false
		}
		var engineErr *Error
		if !errors.As(err, &engineErr) {
			return true
		}
		// A candidate belongs to one document. Navigation can either invalidate that document or
		// destroy its OOPIF target, which closes this handle; discovery can reacquire its replacement.
		return engineErr.Code != CodeFrameDetached && engineErr.Code != CodeSessionClosed
	}
	_, err := Poll(ctx, policy, func(attemptCtx context.Context) (Observation, bool, error) {
		listErr := s.visitFrameDescriptors(attemptCtx, true, func(descriptor frameDescriptor) (bool, error) {
			urlMatches, matchErr := matchesExpectation(descriptor.frame.URL, query.URL)
			if matchErr != nil {
				return false, matchErr
			}
			if !urlMatches {
				return false, nil
			}
			candidate, _, attachErr := s.attachFrame(attemptCtx, descriptor)
			if attachErr != nil {
				return false, attachErr
			}
			if query.Title != nil {
				title, titleErr := candidate.Title(attemptCtx)
				if titleErr != nil {
					_ = candidate.Close()
					if shouldReturnCandidateError(attemptCtx, titleErr) {
						return false, titleErr
					}
					return false, nil
				}
				titleMatches, titleMatchErr := matchesExpectation(title, query.Title)
				if titleMatchErr != nil {
					_ = candidate.Close()
					return false, titleMatchErr
				}
				if !titleMatches {
					_ = candidate.Close()
					return false, nil
				}
			}
			if query.HasElement != nil {
				exists, existsErr := candidate.Exists(attemptCtx, *query.HasElement)
				if existsErr != nil {
					_ = candidate.Close()
					if shouldReturnCandidateError(attemptCtx, existsErr) {
						return false, existsErr
					}
					return false, nil
				}
				found, _ := exists.Value.(bool)
				if !found {
					_ = candidate.Close()
					return false, nil
				}
			}
			matched = candidate
			return true, nil
		})
		if listErr != nil {
			return Observation{}, false, listErr
		}
		if matched != nil {
			return Observation{Value: matched.url}, true, nil
		}
		return Observation{Value: "no matching frame"}, false, nil
	})
	if err != nil {
		return nil, err
	}
	return matched, nil
}

func matchesExpectation(actual string, expectation *Expectation) (bool, error) {
	if expectation == nil {
		return true, nil
	}
	return MatchExpectation(actual, *expectation)
}

func (s *Session) frameDescriptors(ctx context.Context) ([]frameDescriptor, error) {
	descriptors := []frameDescriptor{}
	err := s.visitFrameDescriptors(ctx, false, func(descriptor frameDescriptor) (bool, error) {
		descriptors = append(descriptors, descriptor)
		return false, nil
	})
	return descriptors, err
}

// visitFrameDescriptors visits reachable documents as renderer trees arrive. A waiter can
// finish without waiting for unrelated renderers, including ones blocked by page scripts.
func (s *Session) visitFrameDescriptors(ctx context.Context, incremental bool, visit func(frameDescriptor) (bool, error)) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return &Error{Code: CodeSessionClosed, Operation: "list frames", Message: "session is closed"}
	}
	if s.browser == nil {
		return &Error{Code: CodeSessionClosed, Operation: "list frames", Message: "browser is closed"}
	}
	infos, err := s.frameTargetInfos(ctx)
	if err != nil {
		return err
	}

	type graphNode struct {
		descriptor frameDescriptor
		parentID   cdp.FrameID
	}
	nodes := map[cdp.FrameID]graphNode{}
	order := []cdp.FrameID{}
	scopeID := s.frameID
	addTree := func(tree *page.FrameTree, targetID target.ID) {
		var add func(*page.FrameTree, bool)
		add = func(current *page.FrameTree, root bool) {
			if _, exists := nodes[current.Frame.ID]; !exists {
				order = append(order, current.Frame.ID)
			}
			nodes[current.Frame.ID] = graphNode{
				descriptor: frameDescriptor{frame: current.Frame, targetID: targetID, oopif: root && targetID != s.targetID},
				parentID:   current.Frame.ParentID,
			}
			for _, child := range current.ChildFrames {
				add(child, false)
			}
		}
		add(tree, true)
	}
	tree, err := s.frameTreeForTarget(ctx, s.targetID)
	if err != nil {
		return err
	}
	addTree(tree, s.targetID)
	if scopeID == "" {
		scopeID = tree.Frame.ID
	} else {
		scope, found := nodes[scopeID]
		if !found || scope.descriptor.frame.LoaderID != s.frameLoaderID {
			return staleFrameError("list frames", scopeID)
		}
	}
	visited := map[cdp.FrameID]bool{}
	// The reported security origin can retain the URL origin for sandboxed documents.
	// For equal origins, ask the browser whether the child can actually read its parent.
	boundaries := map[cdp.FrameID]bool{}
	walkAvailable := func() (bool, error) {
		children := map[cdp.FrameID][]cdp.FrameID{}
		for _, id := range order {
			node := nodes[id]
			children[node.parentID] = append(children[node.parentID], id)
		}
		var walk func(cdp.FrameID, bool) (bool, error)
		walk = func(parentID cdp.FrameID, crossed bool) (bool, error) {
			for _, childID := range children[parentID] {
				child := nodes[childID]
				boundary := crossed || child.descriptor.oopif || child.descriptor.frame.SecurityOrigin != nodes[parentID].descriptor.frame.SecurityOrigin
				if !boundary {
					var checked bool
					boundary, checked = boundaries[childID]
					if !checked {
						var err error
						boundary, err = s.frameOriginBoundary(ctx, child.descriptor)
						if err != nil {
							return false, err
						}
						boundaries[childID] = boundary
					}
				}
				if boundary && !visited[childID] {
					visited[childID] = true
					stop, err := visit(child.descriptor)
					if stop || err != nil {
						return stop, err
					}
				}
				stop, err := walk(childID, boundary)
				if stop || err != nil {
					return stop, err
				}
			}
			return false, nil
		}
		return walk(scopeID, false)
	}
	if incremental {
		if stop, err := walkAvailable(); stop || err != nil {
			return err
		}
	}

	// Fetch child trees concurrently so an unresponsive renderer cannot hold up a
	// matching document in another renderer. Cancellation joins every pending request.
	discoveryCtx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	type treeResult struct {
		tree     *page.FrameTree
		targetID target.ID
		err      error
	}
	results := make(chan treeResult, len(infos))
	completed := make(map[target.ID]*page.FrameTree, len(infos))
	for _, info := range infos {
		workers.Add(1)
		go func(id target.ID) {
			defer workers.Done()
			tree, err := s.frameTreeForTarget(discoveryCtx, id)
			results <- treeResult{tree: tree, targetID: id, err: err}
		}(info.TargetID)
	}
	for range infos {
		select {
		case <-ctx.Done():
			return contextError("list frames", ctx.Err())
		case result := <-results:
			if result.err != nil {
				if ctx.Err() != nil {
					return contextError("list frames", ctx.Err())
				}
				// A child may disappear between listing targets and reading its tree.
				continue
			}

			if incremental {
				addTree(result.tree, result.targetID)
				if stop, err := walkAvailable(); stop || err != nil {
					return err
				}
			} else {
				completed[result.targetID] = result.tree
			}
		}
	}
	// A complete snapshot retains target-list and frame-tree order, regardless of
	// which renderer answered first. Waiters instead visit documents as they become ready.
	if !incremental {
		for _, info := range infos {
			if tree := completed[info.TargetID]; tree != nil {
				addTree(tree, info.TargetID)
			}
		}
		_, err := walkAvailable()
		return err
	}
	return nil
}

func (s *Session) frameOriginBoundary(ctx context.Context, descriptor frameDescriptor) (bool, error) {
	targetCtx, err := s.browser.frameTargetContext(ctx, descriptor.targetID)
	if err != nil {
		return false, err
	}
	opCtx, cancel := executorContext(targetCtx, ctx)
	defer cancel()
	var boundary bool
	err = chromedp.Run(opCtx, chromedp.ActionFunc(func(runCtx context.Context) error {
		// Keep this browser access check separate from page-defined globals (including
		// replacements for Window.parent). User evaluation always uses the page world.
		id, err := page.CreateIsolatedWorld(descriptor.frame.ID).WithWorldName("biloba-origin-check-" + string(descriptor.frame.ID)).Do(runCtx)
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

func (s *Session) frameTreeForTarget(ctx context.Context, targetID target.ID) (*page.FrameTree, error) {
	targetCtx, err := s.browser.frameTargetContext(ctx, targetID)
	if err != nil {
		return nil, err
	}
	opCtx, cancelOperation := executorContext(targetCtx, ctx)
	defer cancelOperation()
	tree, err := getFrameTree(opCtx)
	if err != nil {
		return nil, contextError("list target frames", err)
	}
	return tree, nil
}

func (s *Session) frameTargetInfos(ctx context.Context) ([]*target.Info, error) {
	opCtx, cancel := executorContext(s.browser.ctx, ctx)
	defer cancel()
	chrome := chromedp.FromContext(opCtx)
	infos, err := target.GetTargets().Do(cdp.WithExecutor(opCtx, chrome.Browser))
	if err != nil {
		return nil, contextError("list frame targets", err)
	}
	frames := make([]*target.Info, 0)
	descendants := map[target.ID]bool{s.targetID: true}
	for added := true; added; {
		added = false
		for _, info := range infos {
			if info.Type != "iframe" || info.BrowserContextID != s.browserContextID || descendants[info.TargetID] || !descendants[info.ParentID] {
				continue
			}
			descendants[info.TargetID] = true
			frames = append(frames, info)
			added = true
		}
	}
	return frames, nil
}

func findFrameTree(tree *page.FrameTree, id cdp.FrameID) *page.FrameTree {
	if tree == nil {
		return nil
	}
	if tree.Frame.ID == id {
		return tree
	}
	for _, child := range tree.ChildFrames {
		if found := findFrameTree(child, id); found != nil {
			return found
		}
	}
	return nil
}

func (s *Session) attachFrame(ctx context.Context, descriptor frameDescriptor) (*Frame, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, contextError("attach frame", err)
	}
	targetCtx, err := s.browser.frameTargetContext(ctx, descriptor.targetID)
	if err != nil {
		return nil, false, err
	}
	frameCtx, cancelFrame := context.WithCancel(targetCtx)
	opCtx, cancelOperation := executorContext(frameCtx, ctx)
	defer cancelOperation()

	world, err := mainFrameWorld(targetCtx, descriptor.frame.ID)
	if err != nil {
		cancelFrame()
		return nil, false, err
	}

	tree, err := getFrameTree(opCtx)
	if err != nil {
		cancelFrame()
		return nil, false, contextError("validate attached frame", err)
	}
	current := findFrameTree(tree, descriptor.frame.ID)
	if current == nil || current.Frame.LoaderID != descriptor.frame.LoaderID {
		cancelFrame()
		return nil, false, &Error{Code: CodeConditionNotMet, Operation: "attach frame", Message: fmt.Sprintf("frame %s changed document while it was being attached", descriptor.frame.ID)}
	}

	frameSession := &Session{
		browser: s.browser, ctx: frameCtx, cancel: cancelFrame,
		browserContextID: s.browserContextID, targetID: descriptor.targetID,
		frameID: descriptor.frame.ID, frameLoaderID: descriptor.frame.LoaderID,
		frameWorld: world, frameOOPIF: descriptor.oopif,
		root: s.contextRoot(), artifactDir: s.artifactDir, frameTarget: true,
		initialWidth: s.initialWidth, initialHeight: s.initialHeight,
		highFidelity: s.highFidelity, cacheEnabled: true,
	}
	if err := s.browser.registerFrameSession(frameSession); err != nil {
		frameSession.closed = true
		cancelFrame()
		return nil, false, err
	}
	frameSession.eventsEnabled.Store(true)
	if descriptor.oopif {
		s.browser.listenToSession(frameSession)
	}
	return &Frame{Session: frameSession, url: descriptor.frame.URL}, true, nil
}

func (s *Session) validateFrameDocument(ctx context.Context) error {
	tree, err := getFrameTree(ctx)
	if err != nil {
		return contextError("validate frame", err)
	}
	frame := findFrameTree(tree, s.frameID)
	if frame == nil || frame.Frame.LoaderID != s.frameLoaderID {
		return staleFrameError("use frame", s.frameID)
	}
	world, err := mainFrameWorld(ctx, s.frameID)
	if err != nil || world.uniqueID != s.frameWorld.uniqueID {
		return staleFrameError("use frame", s.frameID)
	}

	return nil
}

func getFrameTree(ctx context.Context) (*page.FrameTree, error) {
	var tree *page.FrameTree
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		var err error
		tree, err = page.GetFrameTree().Do(runCtx)
		return err
	}))
	return tree, err
}

func staleFrameError(operation string, id cdp.FrameID) *Error {
	return &Error{Code: CodeFrameDetached, Operation: operation, Message: fmt.Sprintf("frame %s is no longer attached to its original document", id)}
}

func (b *Browser) registerFrameSession(frame *Session) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return &Error{Code: CodeSessionClosed, Operation: "attach frame", Message: "browser is closed"}
	}
	b.sessions[frame] = struct{}{}
	return nil
}

// frameTargetContext returns a renderer attachment whose lifetime belongs to the Browser, not to
// any one frame handle. Same-process frames reuse their tab's existing attachment; OOPIF renderers
// get one managed attachment shared by document-scoped child contexts. Closing an outer handle can
// therefore never cancel a nested handle that happens to use the same renderer connection.
func (b *Browser) frameTargetContext(ctx context.Context, targetID target.ID) (context.Context, error) {
	if err := ctx.Err(); err != nil {
		return nil, contextError("attach frame target", err)
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, &Error{Code: CodeSessionClosed, Operation: "attach frame target", Message: "browser is closed"}
	}
	for session := range b.sessions {
		if !session.frameTarget && session.targetID == targetID {
			targetCtx := session.ctx
			b.mu.Unlock()
			return targetCtx, nil
		}
	}
	if managed := b.frameTargets[targetID]; managed != nil && managed.ctx.Err() == nil {
		targetCtx := managed.ctx
		b.mu.Unlock()
		return targetCtx, nil
	}
	b.mu.Unlock()

	// Keep chromedp's detach/close cleanup separate from execution cancellation. The first Run
	// initializes Context.Target before issuing renderer commands, which may never answer. Cancel
	// those commands first, wait for initialization to finish, and only then suppress target closure
	// and let chromedp detach. Neither request nor browser cancellation may bypass that ordering.
	chromeCtx, cancelChrome := chromedp.NewContext(context.WithoutCancel(b.ctx), chromedp.WithTargetID(targetID))
	chromeCtx = trackFrameWorlds(chromeCtx)
	targetCtx, stopExecution := context.WithCancel(chromeCtx)
	stopBrowserCancel := context.AfterFunc(b.ctx, stopExecution)
	attachDone := make(chan error, 1)
	cleanupDone := make(chan struct{})
	go func() {
		var ready any
		err := chromedp.Run(targetCtx, chromedp.Evaluate("1", &ready))
		protectFrameTarget(chromeCtx)
		attachDone <- err
		<-targetCtx.Done()
		stopBrowserCancel()
		cancelChrome()
		close(cleanupDone)
	}()
	cancelTarget := func() {
		stopExecution()
		<-cleanupDone
	}
	var err error
	select {
	case err = <-attachDone:
	case <-ctx.Done():
		cancelTarget()
		err = ctx.Err()
	}
	if err != nil {
		cancelTarget()
		return nil, contextError("attach frame target", err)
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cancelTarget()
		return nil, &Error{Code: CodeSessionClosed, Operation: "attach frame target", Message: "browser is closed"}
	}
	if b.frameTargets == nil {
		b.frameTargets = map[target.ID]*frameTargetContext{}
	}
	if existing := b.frameTargets[targetID]; existing != nil && existing.ctx.Err() == nil {
		b.mu.Unlock()
		cancelTarget()
		return existing.ctx, nil
	}
	b.frameTargets[targetID] = &frameTargetContext{ctx: targetCtx, cancel: cancelTarget}
	b.mu.Unlock()
	return targetCtx, nil
}

func protectFrameTarget(frameCtx context.Context) {
	frameContext := chromedp.FromContext(frameCtx)
	if frameContext == nil || frameContext.Target == nil {
		return
	}
	// chromedp closes any target it attached when its context is cancelled. This target belongs to
	// the parent page, so retain the session ID for normal detachment but suppress target closure.
	frameContext.Target.TargetID = ""
}

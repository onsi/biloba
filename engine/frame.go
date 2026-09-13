package engine

import (
	"context"
	"fmt"

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
// same-process cross-origin frames use a CDP isolated world in the owning tab's target.
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
func (s *Session) WaitForFrame(ctx context.Context, query FrameQuery, policy PollPolicy) (*Frame, error) {
	var matched *Frame
	_, err := Poll(ctx, policy, func(attemptCtx context.Context) (Observation, bool, error) {
		descriptors, listErr := s.frameDescriptors(attemptCtx)
		if listErr != nil {
			return Observation{}, false, listErr
		}
		for _, descriptor := range descriptors {
			urlMatches, matchErr := matchesExpectation(descriptor.frame.URL, query.URL)
			if matchErr != nil {
				return Observation{}, false, matchErr
			}
			if !urlMatches {
				continue
			}
			candidate, _, attachErr := s.attachFrame(attemptCtx, descriptor)
			if attachErr != nil {
				return Observation{Value: descriptor.frame.URL}, false, attachErr
			}
			if query.Title != nil {
				title, titleErr := candidate.Title(attemptCtx)
				if titleErr != nil {
					_ = candidate.Close()
					continue
				}
				titleMatches, titleMatchErr := matchesExpectation(title, query.Title)
				if titleMatchErr != nil {
					_ = candidate.Close()
					return Observation{}, false, titleMatchErr
				}
				if !titleMatches {
					_ = candidate.Close()
					continue
				}
			}
			if query.HasElement != nil {
				exists, existsErr := candidate.Exists(attemptCtx, *query.HasElement)
				if existsErr != nil {
					_ = candidate.Close()
					continue
				}
				found, _ := exists.Value.(bool)
				if !found {
					_ = candidate.Close()
					continue
				}
			}
			matched = candidate
			return Observation{Value: candidate.url}, true, nil
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
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, &Error{Code: CodeSessionClosed, Operation: "list frames", Message: "session is closed"}
	}
	if s.browser == nil {
		return nil, &Error{Code: CodeSessionClosed, Operation: "list frames", Message: "browser is closed"}
	}
	infos, err := s.frameTargetInfos(ctx)
	if err != nil {
		return nil, err
	}

	// A Page frame tree is renderer-local: Chrome may stop it at an OOPIF boundary. Read the tree
	// from every descendant iframe target, then join them by Frame.ParentID. This produces the actual
	// document ancestry rather than the coarser target ancestry (which cannot distinguish sibling
	// OOPIFs below a same-process frame).
	targetIDs := []target.ID{s.targetID}
	for _, info := range infos {
		targetIDs = append(targetIDs, info.TargetID)
	}
	type graphNode struct {
		descriptor frameDescriptor
		origin     string
		parentID   cdp.FrameID
	}
	nodes := map[cdp.FrameID]graphNode{}
	order := []cdp.FrameID{}
	for index, targetID := range targetIDs {
		tree, treeErr := s.frameTreeForTarget(ctx, targetID)
		if treeErr != nil {
			if index == 0 {
				return nil, treeErr
			}
			// Target.getTargets and attachment are not atomic. A disappearing child is simply no
			// longer discoverable; the next waiter poll will rebuild the graph from current targets.
			continue
		}
		var addTree func(*page.FrameTree, bool)
		addTree = func(current *page.FrameTree, targetRoot bool) {
			if _, exists := nodes[current.Frame.ID]; !exists {
				order = append(order, current.Frame.ID)
			}
			nodes[current.Frame.ID] = graphNode{
				descriptor: frameDescriptor{frame: current.Frame, targetID: targetID, oopif: targetRoot && index > 0},
				origin:     current.Frame.SecurityOrigin, parentID: current.Frame.ParentID,
			}
			for _, child := range current.ChildFrames {
				addTree(child, false)
			}
		}
		addTree(tree, true)
	}

	scopeID := cdp.FrameID("")
	if s.frameID != "" {
		scopeID = s.frameID
		scope, found := nodes[scopeID]
		if !found || scope.descriptor.frame.LoaderID != s.frameLoaderID {
			return nil, staleFrameError("list frames", s.frameID)
		}
	} else {
		for _, id := range order {
			if nodes[id].descriptor.targetID == s.targetID && nodes[id].parentID == "" {
				scopeID = id
				break
			}
		}
	}
	if scopeID == "" {
		return nil, &Error{Code: CodeActionFailed, Operation: "list frames", Message: "root frame was not present in Chrome's frame tree"}
	}

	children := map[cdp.FrameID][]cdp.FrameID{}
	for _, id := range order {
		node := nodes[id]
		if node.parentID != "" {
			children[node.parentID] = append(children[node.parentID], id)
		}
	}
	descriptors := []frameDescriptor{}
	var walk func(cdp.FrameID, bool)
	walk = func(parentID cdp.FrameID, crossedOrigin bool) {
		parent := nodes[parentID]
		for _, childID := range children[parentID] {
			child := nodes[childID]
			crossed := crossedOrigin || child.origin != parent.origin
			if crossed {
				descriptors = append(descriptors, child.descriptor)
			}
			walk(childID, crossed)
		}
	}
	walk(scopeID, false)
	return descriptors, nil
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

	var executionContextID runtime.ExecutionContextID
	if descriptor.oopif {
		tree, err := getFrameTree(opCtx)
		if err != nil {
			cancelFrame()
			return nil, false, contextError("read attached frame", err)
		}
		descriptor.frame = tree.Frame
	} else {
		var err error
		err = chromedp.Run(opCtx, chromedp.ActionFunc(func(runCtx context.Context) error {
			executionContextID, err = page.CreateIsolatedWorld(descriptor.frame.ID).
				WithWorldName("biloba-frame-" + string(descriptor.frame.ID)).Do(runCtx)
			return err
		}))
		if err != nil {
			cancelFrame()
			return nil, false, contextError("attach frame", err)
		}
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
		executionContextID: executionContextID,
		root:               s.contextRoot(), artifactDir: s.artifactDir, frameTarget: true,
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

	targetCtx, cancelTarget := chromedp.NewContext(b.ctx, chromedp.WithTargetID(targetID))
	attachDone := make(chan error, 1)
	go func() {
		var ready any
		attachDone <- chromedp.Run(targetCtx, chromedp.Evaluate("1", &ready))
	}()
	var err error
	select {
	case err = <-attachDone:
	case <-ctx.Done():
		cancelTarget()
		err = ctx.Err()
	}
	protectFrameTarget(targetCtx)
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

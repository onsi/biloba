package engine

import (
	"context"
	"sync"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// FrameQuery selects an out-of-process iframe by its target metadata and document state.
type FrameQuery struct {
	Title      *Expectation
	URL        *Expectation
	HasElement *Selector
}

// Frame is a DOM-capable handle attached to a cross-origin iframe target.
type Frame struct {
	*Session
	url       string
	closeOnce sync.Once
}

// URL returns the target URL observed when the frame was discovered.
func (f *Frame) URL() string { return f.url }

// Close detaches this handle without removing the iframe from its parent document.
func (f *Frame) Close() error {
	f.closeOnce.Do(func() {
		f.Session.mu.Lock()
		f.Session.closed = true
		f.Session.cancel()
		f.Session.mu.Unlock()
	})
	return nil
}

// Frames returns handles for every out-of-process iframe in this session's browser context.
func (s *Session) Frames(ctx context.Context) ([]*Frame, error) {
	infos, err := s.frameTargetInfos(ctx)
	if err != nil {
		return nil, err
	}
	frames := make([]*Frame, 0, len(infos))
	for _, info := range infos {
		frame, attachErr := s.attachFrame(ctx, info)
		if attachErr != nil {
			for _, attached := range frames {
				_ = attached.Close()
			}
			return nil, attachErr
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

// WaitForFrame discovers a matching cross-origin frame and waits for its document predicate.
func (s *Session) WaitForFrame(ctx context.Context, query FrameQuery, policy PollPolicy) (*Frame, error) {
	var candidate *Frame
	_, err := Poll(ctx, policy, func(attemptCtx context.Context) (Observation, bool, error) {
		if candidate == nil {
			infos, listErr := s.frameTargetInfos(attemptCtx)
			if listErr != nil {
				return Observation{}, false, listErr
			}
			for _, info := range infos {
				matched, matchErr := frameMetadataMatches(info, query)
				if matchErr != nil {
					return Observation{}, false, matchErr
				}
				if !matched {
					continue
				}
				candidate, matchErr = s.attachFrame(attemptCtx, info)
				if matchErr != nil {
					return Observation{Value: info.URL}, false, matchErr
				}
				break
			}
		}
		if candidate == nil {
			return Observation{Value: "no matching frame target"}, false, nil
		}
		if query.HasElement == nil {
			return Observation{Value: candidate.url}, true, nil
		}
		exists, existsErr := candidate.Exists(attemptCtx, *query.HasElement)
		if existsErr != nil {
			return exists, false, existsErr
		}
		found, _ := exists.Value.(bool)
		return exists, found, nil
	})
	if err != nil {
		if candidate != nil {
			_ = candidate.Close()
		}
		return nil, err
	}
	return candidate, nil
}

func (s *Session) frameTargetInfos(ctx context.Context) ([]*target.Info, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, &Error{Code: CodeSessionClosed, Operation: "list frames", Message: "session is closed"}
	}
	if s.browser == nil {
		return nil, &Error{Code: CodeSessionClosed, Operation: "list frames", Message: "browser is closed"}
	}
	opCtx, cancel := executorContext(s.browser.ctx, ctx)
	defer cancel()
	chrome := chromedp.FromContext(opCtx)
	infos, err := target.GetTargets().Do(cdp.WithExecutor(opCtx, chrome.Browser))
	if err != nil {
		return nil, contextError("list frames", err)
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

func frameMetadataMatches(info *target.Info, query FrameQuery) (bool, error) {
	for _, value := range []struct {
		actual      string
		expectation *Expectation
	}{{info.Title, query.Title}, {info.URL, query.URL}} {
		if value.expectation == nil {
			continue
		}
		matched, err := MatchExpectation(value.actual, *value.expectation)
		if err != nil || !matched {
			return false, err
		}
	}
	return true, nil
}

func (s *Session) attachFrame(ctx context.Context, info *target.Info) (*Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, contextError("attach frame", err)
	}
	frameCtx, cancelFrame := chromedp.NewContext(s.browser.ctx, chromedp.WithTargetID(info.TargetID))
	var ready any
	if err := chromedp.Run(frameCtx, chromedp.Evaluate("1", &ready)); err != nil {
		protectFrameTarget(frameCtx)
		cancelFrame()
		return nil, contextError("attach frame", err)
	}
	protectFrameTarget(frameCtx)
	frameSession := &Session{
		browser: s.browser, ctx: frameCtx, cancel: cancelFrame,
		browserContextID: s.browserContextID, targetID: info.TargetID,
		root: s.contextRoot(), artifactDir: s.artifactDir,
	}
	s.browser.listenToSession(frameSession)
	return &Frame{Session: frameSession, url: info.URL}, nil
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

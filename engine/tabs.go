package engine

import (
	"context"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

type TabQuery struct {
	SpawnedOnly bool
	Title       *Expectation
	URL         *Expectation
	HasElement  *Selector
}

// Tabs discovers and returns every page target in this session's isolated browser context.
func (s *Session) Tabs(ctx context.Context) ([]*Session, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, &Error{Code: CodeSessionClosed, Operation: "list tabs", Message: "session is closed"}
	}
	if s.browser == nil {
		return nil, &Error{Code: CodeSessionClosed, Operation: "list tabs", Message: "browser is closed"}
	}
	opCtx, cancel := executorContext(s.browser.ctx, ctx)
	defer cancel()
	chrome := chromedp.FromContext(opCtx)
	infos, err := target.GetTargets().Do(cdp.WithExecutor(opCtx, chrome.Browser))
	if err != nil {
		return nil, contextError("list tabs", err)
	}
	out := []*Session{}
	for _, info := range infos {
		if info.Type != "page" || info.BrowserContextID != s.browserContextID {
			continue
		}
		tab, attachErr := s.browser.sessionForTarget(ctx, info.TargetID, info.OpenerID, s.contextRoot())
		if attachErr != nil {
			return nil, attachErr
		}
		out = append(out, tab)
	}
	return out, nil
}

func (b *Browser) sessionForTarget(ctx context.Context, targetID, openerID target.ID, root *Session) (*Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, &Error{Code: CodeSessionClosed, Operation: "attach tab", Message: "browser is closed"}
	}
	for session := range b.sessions {
		if session.targetID == targetID {
			if session.openerID == "" {
				session.openerID = openerID
			}
			return session, nil
		}
	}
	tabCtx, cancelTab := chromedp.NewContext(b.ctx, chromedp.WithTargetID(targetID))
	done := make(chan error, 1)
	go func() { done <- chromedp.Run(tabCtx, chromedp.Evaluate("1", nil)) }()
	select {
	case err := <-done:
		if err != nil {
			cancelTab()
			return nil, contextError("attach tab", err)
		}
	case <-ctx.Done():
		cancelTab()
		return nil, contextError("attach tab", ctx.Err())
	}
	session := &Session{
		browser: b, ctx: tabCtx, cancel: cancelTab, browserContextID: root.browserContextID,
		targetID: targetID, openerID: openerID, root: root, artifactDir: b.artifactDir,
		initialWidth: b.windowWidth, initialHeight: b.windowHeight,
		highFidelity: b.mode == ChromeModeHeadless, cacheEnabled: true,
	}
	if session.initialWidth <= 0 || session.initialHeight <= 0 {
		width, height, sizeErr := ViewportDimensionsContext(tabCtx)
		if sizeErr != nil {
			cancelTab()
			return nil, contextError("read initial window size", sizeErr)
		}
		session.initialWidth, session.initialHeight = int(width), int(height)
	}
	if err := session.applyViewport(tabCtx, session.initialWidth, session.initialHeight); err != nil {
		cancelTab()
		return nil, contextError("apply initial viewport", err)
	}
	session.eventsEnabled.Store(true)
	b.listenToSession(session)
	if err := session.setupDownloads(tabCtx); err != nil {
		cancelTab()
		return nil, contextError("configure downloads", err)
	}
	b.sessions[session] = struct{}{}
	return session, nil
}

func (s *Session) WaitForTab(ctx context.Context, query TabQuery, policy PollPolicy) (*Session, error) {
	var matched *Session
	_, err := Poll(ctx, policy, func(attemptCtx context.Context) (Observation, bool, error) {
		tabs, listErr := s.Tabs(attemptCtx)
		if listErr != nil {
			return Observation{}, false, listErr
		}
		for _, tab := range tabs {
			if query.SpawnedOnly && tab.openerID != s.targetID {
				continue
			}
			ok, matchErr := tab.matchesTabQuery(attemptCtx, query)
			if matchErr != nil {
				return Observation{}, false, matchErr
			}
			if ok {
				matched = tab
				return Observation{Value: tab.targetID}, true, nil
			}
		}
		return Observation{Value: len(tabs)}, false, nil
	})
	return matched, err
}

func (s *Session) matchesTabQuery(ctx context.Context, query TabQuery) (bool, error) {
	if query.Title != nil {
		value, err := s.Title(ctx)
		if err != nil {
			return false, err
		}
		matched, err := MatchExpectation(value, *query.Title)
		if err != nil || !matched {
			return false, err
		}
	}
	if query.URL != nil {
		value, err := s.URL(ctx)
		if err != nil {
			return false, err
		}
		matched, err := MatchExpectation(value.Value, *query.URL)
		if err != nil || !matched {
			return false, err
		}
	}
	if query.HasElement != nil {
		value, err := s.Exists(ctx, *query.HasElement)
		if err != nil {
			return false, err
		}
		if found, ok := value.Value.(bool); !ok || !found {
			return false, nil
		}
	}
	return true, nil
}

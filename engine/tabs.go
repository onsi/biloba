package engine

import (
	"context"
	"time"

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
	return s.tabs(ctx)
}

// tabs is used by root Prepare while the root's serial lock is held.
func (s *Session) tabs(ctx context.Context) ([]*Session, error) {
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
		if tab == nil {
			continue
		}
		out = append(out, tab)
	}
	return out, nil
}

// A brand-new or app-spawned target can transiently fail - not wedge - its first attach under heavy
// parallel load, and Chrome keeps a closing target visible in Target.getTargets for a few tens of
// milliseconds while it tears down.  Both are mirrored from registerTabFor in biloba.go: retry the
// probe on the same context, and give it a watchdog of its own rather than spending the caller's
// deadline.  A target that still will not attach is skipped, not raised - otherwise one dying popup
// fails the whole listTabs, and with it the Prepare that calls it.
const (
	tabProbeAttempts     = 3
	tabProbeRetryBackoff = 50 * time.Millisecond
	tabAttachTimeout     = 20 * time.Second
)

func (b *Browser) sessionForTarget(ctx context.Context, targetID, openerID target.ID, root *Session) (*Session, error) {
	if existing, settled, err := b.registeredSessionForTarget(targetID, openerID); settled {
		return existing, err
	}
	// The probe runs unlocked.  Holding b.mu across it puts every other session on this worker -
	// OpenSession, openTab, Close, Sessions, target reconciliation - behind a round trip to a target
	// that may be mid-teardown.  listenToSession's own comment says as much from the other side.
	tabCtx, cancelTab := chromedp.NewContext(b.ctx, chromedp.WithTargetID(targetID))
	if !attachProbeSucceeds(ctx, tabCtx) {
		cancelTab()
		// A target that will not attach is skipped - it is almost always one Chrome is still
		// reporting while it tears down.  The caller giving up is a different thing and stays an
		// error, or a cancelled Prepare would quietly report an empty tab list.
		if ctx.Err() != nil {
			return nil, contextError("attach tab", ctx.Err())
		}
		return nil, nil
	}
	b.mu.Lock()
	// Re-check under the lock: the browser may have closed, the target may have been destroyed, or
	// another goroutine may have attached to it while we were probing.
	if b.closed {
		b.mu.Unlock()
		cancelTab()
		return nil, &Error{Code: CodeSessionClosed, Operation: "attach tab", Message: "browser is closed"}
	}
	if _, closed := b.closedIDs[targetID]; closed {
		b.mu.Unlock()
		cancelTab()
		return nil, nil
	}
	for session := range b.sessions {
		if session.targetID == targetID {
			if session.openerID == "" {
				session.openerID = openerID
			}
			b.mu.Unlock()
			cancelTab()
			return session, nil
		}
	}
	defer b.mu.Unlock()
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

// registeredSessionForTarget answers from the registry alone, without attaching.  settled reports
// whether the answer is final; when it is false the caller has to attach.
func (b *Browser) registeredSessionForTarget(targetID, openerID target.ID) (session *Session, settled bool, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, true, &Error{Code: CodeSessionClosed, Operation: "attach tab", Message: "browser is closed"}
	}
	if _, closed := b.closedIDs[targetID]; closed {
		return nil, true, nil // destroyed: the caller skips it rather than failing
	}
	for existing := range b.sessions {
		if existing.targetID == targetID {
			if existing.openerID == "" {
				existing.openerID = openerID
			}
			return existing, true, nil
		}
	}
	return nil, false, nil
}

// attachProbeSucceeds runs Runtime.evaluate against a freshly attached target, retrying a transient
// failure on the same context.  It never cancels between attempts: once the attach has landed,
// cancelling closes a healthy target out from under its owner.
func attachProbeSucceeds(ctx context.Context, tabCtx context.Context) bool {
	for attempt := range tabProbeAttempts {
		if attempt > 0 {
			select {
			case <-time.After(tabProbeRetryBackoff):
			case <-ctx.Done():
				return false
			}
		}
		done := make(chan error, 1)
		go func() { done <- chromedp.Run(tabCtx, chromedp.Evaluate("1", nil)) }()
		select {
		case err := <-done:
			if err == nil {
				return true
			}
		case <-time.After(tabAttachTimeout):
			return false // a genuine wedge; a context timeout cannot unblock it
		case <-ctx.Done():
			return false
		}
	}
	return false
}

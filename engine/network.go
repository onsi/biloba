package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// EnableInterceptionContext turns on request interception after disabling the HTTP cache.
func EnableInterceptionContext(ctx context.Context) error {
	return chromedp.Run(ctx,
		network.SetCacheDisabled(true),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*"}}),
	)
}

// RunActionContext dispatches one CDP action against the target executor carried by ctx.
func RunActionContext(ctx context.Context, action chromedp.Action) error {
	return chromedp.Run(ctx, action)
}

// ResponseBodyContext reads and decodes the body of a response paused by Fetch interception.
func ResponseBodyContext(ctx context.Context, requestID fetch.RequestID) ([]byte, error) {
	var body []byte
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		var readErr error
		body, readErr = fetch.GetResponseBody(requestID).Do(runCtx)
		return readErr
	}))
	return body, err
}

// Request is one request observed in a session since its last Prepare.
type Request struct {
	URL          string
	Method       string
	Headers      map[string]string
	ResourceType string
}

// HeldResponse identifies a matching response paused by HoldResponse.
type HeldResponse struct {
	URL    string
	Status int
}

type heldResponseEntry struct {
	response HeldResponse
	release  chan struct{}
	released bool
}

type responseHold struct {
	expectation Expectation
	entries     []*heldResponseEntry
	notify      chan struct{}
	released    bool
}

func (s *Session) recordRequest(event *network.EventRequestWillBeSent) {
	headers := make(map[string]string, len(event.Request.Headers))
	for name, value := range event.Request.Headers {
		headers[name] = fmt.Sprint(value)
	}
	s.requestMu.Lock()
	s.requests = append(s.requests, Request{
		URL: event.Request.URL, Method: event.Request.Method, Headers: headers, ResourceType: event.Type.String(),
	})
	s.requestMu.Unlock()
}

// Requests returns a snapshot of requests observed by the session.
func (s *Session) Requests() []Request {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	out := make([]Request, len(s.requests))
	copy(out, s.requests)
	return out
}

func (s *Session) clearRequests() {
	s.requestMu.Lock()
	s.requests = nil
	s.requestMu.Unlock()
}

// HoldResponse begins pausing responses whose URLs match expectation.
func (s *Session) HoldResponse(ctx context.Context, expectation Expectation) (string, error) {
	if _, err := MatchExpectation("", expectation); err != nil {
		return "", &Error{Code: CodeInvalidArgument, Operation: "hold response", Message: err.Error(), Cause: err}
	}
	var id string
	err := s.serial(ctx, "hold response", func(opCtx context.Context) error {
		s.holdMu.Lock()
		if s.holds == nil {
			s.holds = map[string]*responseHold{}
		}
		s.holdSequence++
		id = fmt.Sprintf("hold-%d", s.holdSequence)
		s.holds[id] = &responseHold{expectation: expectation, notify: make(chan struct{})}
		s.holdOrder = append(s.holdOrder, id)
		alreadyEnabled := s.fetchEnabled
		s.fetchEnabled = true
		s.holdMu.Unlock()
		if alreadyEnabled {
			return nil
		}
		return chromedp.Run(opCtx, chromedp.ActionFunc(func(runCtx context.Context) error {
			return fetch.Enable().WithPatterns([]*fetch.RequestPattern{{
				URLPattern: "*", RequestStage: fetch.RequestStageResponse,
			}}).Do(runCtx)
		}))
	})
	return id, err
}

// AwaitResponseHold waits until a matching response is paused.
func (s *Session) AwaitResponseHold(ctx context.Context, id string) (HeldResponse, error) {
	for {
		s.holdMu.Lock()
		hold := s.holds[id]
		if hold == nil {
			s.holdMu.Unlock()
			return HeldResponse{}, &Error{Code: CodeInvalidArgument, Operation: "await response hold", Message: "response hold not found"}
		}
		if len(hold.entries) > 0 {
			response := hold.entries[0].response
			s.holdMu.Unlock()
			return response, nil
		}
		notify := hold.notify
		s.holdMu.Unlock()
		select {
		case <-notify:
		case <-ctx.Done():
			return HeldResponse{}, contextError("await response hold", ctx.Err())
		}
	}
}

// ReleaseResponseHold releases all responses held by id and stops holding future matches.
func (s *Session) ReleaseResponseHold(_ context.Context, id string) error {
	s.holdMu.Lock()
	defer s.holdMu.Unlock()
	hold := s.holds[id]
	if hold == nil {
		return &Error{Code: CodeInvalidArgument, Operation: "release response hold", Message: "response hold not found"}
	}
	hold.released = true
	for _, entry := range hold.entries {
		releaseHeldResponse(entry)
	}
	return nil
}

func (s *Session) handlePausedResponse(event *fetch.EventRequestPaused) {
	if event.Request == nil {
		go s.continueResponse(event.RequestID)
		return
	}
	s.holdMu.Lock()
	var selected *responseHold
	for _, id := range s.holdOrder {
		hold := s.holds[id]
		if hold == nil || hold.released {
			continue
		}
		matched, err := MatchExpectation(event.Request.URL, hold.expectation)
		if err == nil && matched {
			selected = hold
			break
		}
	}
	if selected == nil {
		s.holdMu.Unlock()
		go s.continueResponse(event.RequestID)
		return
	}
	entry := &heldResponseEntry{
		response: HeldResponse{URL: event.Request.URL, Status: int(event.ResponseStatusCode)},
		release:  make(chan struct{}),
	}
	selected.entries = append(selected.entries, entry)
	close(selected.notify)
	selected.notify = make(chan struct{})
	s.holdMu.Unlock()
	go func() {
		select {
		case <-entry.release:
		case <-s.ctx.Done():
		}
		s.continueResponse(event.RequestID)
	}()
}

func (s *Session) continueResponse(id fetch.RequestID) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	_ = chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		return fetch.ContinueResponse(id).Do(runCtx)
	}))
}

func (s *Session) releaseAllResponseHolds() {
	s.holdMu.Lock()
	defer s.holdMu.Unlock()
	for _, hold := range s.holds {
		hold.released = true
		for _, entry := range hold.entries {
			releaseHeldResponse(entry)
		}
	}
}

func (s *Session) resetResponseHolds(ctx context.Context) error {
	s.releaseAllResponseHolds()
	s.holdMu.Lock()
	enabled := s.fetchEnabled
	s.fetchEnabled = false
	s.holds = nil
	s.holdOrder = nil
	s.holdMu.Unlock()
	if !enabled {
		return nil
	}
	return chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		return fetch.Disable().Do(runCtx)
	}))
}

func releaseHeldResponse(entry *heldResponseEntry) {
	if entry.released {
		return
	}
	entry.released = true
	close(entry.release)
}

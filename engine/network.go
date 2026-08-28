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

type RequestQuery struct {
	URL, Method, ResourceType *Expectation
}

// HeldResponse identifies a matching response paused by HoldResponse.
type HeldResponse struct {
	ID      string
	URL     string
	Status  int
	Headers map[string]string
	Body    []byte
}

type ResponseHoldOptions struct {
	Limit        int
	MaxBodyBytes int64
	Callsite     string
}

type ResponseHoldStats struct {
	Count         int
	Held          int
	PassedThrough int
	Holding       int
	LastError     string
}

type heldResponseEntry struct {
	response HeldResponse
	release  chan struct{}
	released bool
}

type responseHold struct {
	id          string
	callsite    string
	order       uint64
	expectation Expectation
	entries     []*heldResponseEntry
	notify      chan struct{}
	limit       int
	bodyLimit   int64
	count       int
	shadowed    int
	passed      int
	pending     int
	lastError   string
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
	for i, request := range s.requests {
		out[i] = request
		out[i].Headers = cloneStringMap(request.Headers)
	}
	return out
}

// RequestsMatching returns an ordered defensive snapshot filtered by request metadata.
func (s *Session) RequestsMatching(query RequestQuery) []Request {
	requests := s.Requests()
	matched := make([]Request, 0, len(requests))
	for _, request := range requests {
		matches := true
		for _, candidate := range []struct {
			value       any
			expectation *Expectation
		}{{request.URL, query.URL}, {request.Method, query.Method}, {request.ResourceType, query.ResourceType}} {
			if candidate.expectation == nil {
				continue
			}
			ok, _ := MatchExpectation(candidate.value, *candidate.expectation)
			matches = matches && ok
		}
		if matches {
			matched = append(matched, request)
		}
	}
	return matched
}

// WaitForRequest polls until the first request matching query has been observed.
func (s *Session) WaitForRequest(ctx context.Context, query RequestQuery, policy PollPolicy) (Request, error) {
	var found Request
	_, err := Poll(ctx, policy, func(context.Context) (Observation, bool, error) {
		matches := s.RequestsMatching(query)
		if len(matches) == 0 {
			return Observation{}, false, nil
		}
		found = matches[0]
		return Observation{Value: found}, true, nil
	})
	return found, err
}

func (s *Session) clearRequests() {
	s.requestMu.Lock()
	s.requests = nil
	s.requestMu.Unlock()
}

// HoldResponse begins pausing responses whose URLs match expectation.
func (s *Session) HoldResponse(ctx context.Context, expectation Expectation) (string, error) {
	return s.HoldResponseWithOptions(ctx, expectation, ResponseHoldOptions{})
}

// HoldResponseWithOptions begins pausing responses whose URLs match expectation.
func (s *Session) HoldResponseWithOptions(ctx context.Context, expectation Expectation, options ResponseHoldOptions) (string, error) {
	if _, err := MatchExpectation("", expectation); err != nil {
		return "", &Error{Code: CodeInvalidArgument, Operation: "hold response", Message: err.Error(), Cause: err}
	}
	if options.Limit < 0 {
		return "", &Error{Code: CodeInvalidArgument, Operation: "hold response", Message: "limit must not be negative", Observed: options.Limit}
	}
	if options.MaxBodyBytes < 0 {
		return "", &Error{Code: CodeInvalidArgument, Operation: "hold response", Message: "body limit must not be negative", Observed: options.MaxBodyBytes}
	}
	var id string
	err := s.serial(ctx, "hold response", func(opCtx context.Context) error {
		s.holdMu.Lock()
		if s.holds == nil {
			s.holds = map[string]*responseHold{}
		}
		s.holdSequence++
		id = fmt.Sprintf("%s-hold-%d", s.targetID, s.holdSequence)
		bodyLimit := options.MaxBodyBytes
		if bodyLimit == 0 {
			bodyLimit = DefaultInterceptedBodyLimit
		}
		s.holds[id] = &responseHold{
			id: id, callsite: options.Callsite, order: s.nextInterceptionOrder(), expectation: expectation,
			notify: make(chan struct{}), limit: options.Limit, bodyLimit: bodyLimit,
		}
		s.holdOrder = append(s.holdOrder, id)
		s.holdMu.Unlock()
		if err := s.ensureInterception(opCtx); err != nil {
			s.holdMu.Lock()
			delete(s.holds, id)
			s.holdOrder = s.holdOrder[:len(s.holdOrder)-1]
			s.holdMu.Unlock()
			return err
		}
		return nil
	})
	return id, err
}

// AwaitResponseHold waits until a matching response is paused.
func (s *Session) AwaitResponseHold(ctx context.Context, id string) (HeldResponse, error) {
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	for {
		s.holdMu.Lock()
		hold := s.holds[id]
		if hold == nil {
			s.holdMu.Unlock()
			return HeldResponse{}, &Error{Code: CodeInvalidArgument, Operation: "await response hold", Message: "response hold not found"}
		}
		if entry := oldestHeldEntry(hold); entry != nil {
			response := cloneHeldResponse(entry.response)
			s.holdMu.Unlock()
			return response, nil
		}
		if hold.released && len(hold.entries) > 0 {
			response := cloneHeldResponse(hold.entries[0].response)
			s.holdMu.Unlock()
			return response, nil
		}
		if hold.lastError != "" {
			message := hold.lastError
			s.holdMu.Unlock()
			return HeldResponse{}, &Error{Code: CodeActionFailed, Operation: "await response hold", Message: message}
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
func (s *Session) ReleaseResponseHold(ctx context.Context, id string) error {
	s.holdMu.Lock()
	hold := s.holds[id]
	if hold == nil {
		s.holdMu.Unlock()
		return &Error{Code: CodeInvalidArgument, Operation: "release response hold", Message: "response hold not found"}
	}
	hold.released = true
	for _, entry := range hold.entries {
		releaseHeldResponse(entry)
	}
	s.holdMu.Unlock()
	opCtx, cancel := executorContext(s.ctx, ctx)
	defer cancel()
	return s.disableInterceptionIfUnused(opCtx)
}

// ReleaseHeldResponse releases one response and leaves its hold armed.
func (s *Session) ReleaseHeldResponse(_ context.Context, holdID, responseID string) error {
	s.holdMu.Lock()
	defer s.holdMu.Unlock()
	hold := s.holds[holdID]
	if hold == nil {
		return &Error{Code: CodeInvalidArgument, Operation: "release held response", Message: "response hold not found"}
	}
	for _, entry := range hold.entries {
		if entry.response.ID != responseID {
			continue
		}
		if entry.released {
			return &Error{Code: CodeInvalidArgument, Operation: "release held response", Message: "response was already released", Observed: responseID}
		}
		releaseHeldResponse(entry)
		return nil
	}
	return &Error{Code: CodeInvalidArgument, Operation: "release held response", Message: "response was not held", Observed: responseID}
}

// ReleaseNextResponseHold releases the oldest response and leaves its hold armed.
func (s *Session) ReleaseNextResponseHold(_ context.Context, id string) error {
	s.holdMu.Lock()
	defer s.holdMu.Unlock()
	hold := s.holds[id]
	if hold == nil {
		return &Error{Code: CodeInvalidArgument, Operation: "release next response", Message: "response hold not found"}
	}
	entry := oldestHeldEntry(hold)
	if entry == nil {
		return &Error{Code: CodeActionFailed, Operation: "release next response", Message: "response hold is not holding a response"}
	}
	releaseHeldResponse(entry)
	return nil
}

// ResponseHoldStats returns cumulative and currently-held response counts.
func (s *Session) ResponseHoldStats(id string) (ResponseHoldStats, error) {
	s.holdMu.Lock()
	defer s.holdMu.Unlock()
	hold := s.holds[id]
	if hold == nil {
		return ResponseHoldStats{}, &Error{Code: CodeInvalidArgument, Operation: "response hold stats", Message: "response hold not found"}
	}
	holding := 0
	for _, entry := range hold.entries {
		if !entry.released {
			holding++
		}
	}
	return ResponseHoldStats{Count: hold.count, Held: hold.count - hold.passed, PassedThrough: hold.passed, Holding: holding + hold.pending, LastError: hold.lastError}, nil
}

func (s *Session) handlePausedResponse(event *fetch.EventRequestPaused, selected *responseHold) {
	if event.Request == nil {
		go s.continueResponse(event.RequestID)
		return
	}
	if selected == nil {
		go s.continueResponse(event.RequestID)
		return
	}
	s.holdMu.Lock()
	if selected.released || (selected.limit > 0 && holdingCount(selected)+selected.pending >= selected.limit) {
		selected.passed++
		s.holdMu.Unlock()
		go s.continueResponse(event.RequestID)
		return
	}
	selected.pending++
	s.holdMu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		defer cancel()
		body, err := ResponseBodyContext(ctx, event.RequestID)
		if err != nil {
			s.holdMu.Lock()
			selected.pending--
			selected.passed++
			selected.lastError = err.Error()
			close(selected.notify)
			selected.notify = make(chan struct{})
			s.holdMu.Unlock()
			s.continueResponse(event.RequestID)
			return
		}
		if int64(len(body)) > selected.bodyLimit {
			s.holdMu.Lock()
			selected.pending--
			selected.passed++
			selected.lastError = fmt.Sprintf("intercepted response body size %d exceeds limit %d", len(body), selected.bodyLimit)
			close(selected.notify)
			selected.notify = make(chan struct{})
			s.holdMu.Unlock()
			s.continueResponse(event.RequestID)
			return
		}
		headers := make(map[string]string, len(event.ResponseHeaders))
		for _, header := range event.ResponseHeaders {
			headers[header.Name] = header.Value
		}
		entry := &heldResponseEntry{
			response: HeldResponse{ID: string(event.RequestID), URL: event.Request.URL, Status: int(event.ResponseStatusCode), Headers: headers, Body: body},
			release:  make(chan struct{}),
		}
		s.holdMu.Lock()
		selected.pending--
		// Prepare or close may have terminally released the hold while Chrome returned the body.
		if selected.released {
			selected.passed++
			s.holdMu.Unlock()
			s.continueResponse(event.RequestID)
			return
		}
		selected.entries = append(selected.entries, entry)
		close(selected.notify)
		selected.notify = make(chan struct{})
		s.holdMu.Unlock()
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

func (s *Session) clearResponseHoldBookkeeping() {
	s.holdMu.Lock()
	s.holds = nil
	s.holdOrder = nil
	s.fetchEnabled = false
	s.holdMu.Unlock()
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
	return chromedp.Run(ctx, fetch.Disable(), network.SetCacheDisabled(false))
}

func releaseHeldResponse(entry *heldResponseEntry) {
	if entry.released {
		return
	}
	entry.released = true
	close(entry.release)
}

func oldestHeldEntry(hold *responseHold) *heldResponseEntry {
	for _, entry := range hold.entries {
		if !entry.released {
			return entry
		}
	}
	return nil
}

func holdingCount(hold *responseHold) int {
	count := 0
	for _, entry := range hold.entries {
		if !entry.released {
			count++
		}
	}
	return count
}

func cloneHeldResponse(response HeldResponse) HeldResponse {
	response.Headers = cloneStringMap(response.Headers)
	response.Body = append([]byte(nil), response.Body...)
	return response
}

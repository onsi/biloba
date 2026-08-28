package engine

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const DefaultInterceptedBodyLimit int64 = 16 << 20

type ResponseOverride struct {
	Status  *int
	Headers map[string]string
	Body    *[]byte
}
type RequestOverride struct {
	URL, Method *string
	Headers     map[string]string
	Body        *[]byte
}

type InterceptedResponse struct {
	URL     string
	Status  int
	Headers map[string]string
	Body    []byte
}

type ResponseTransform func(context.Context, InterceptedResponse) (ResponseOverride, error)

type NetworkHandlerOptions struct {
	URL               Expectation
	Fulfill           *ResponseOverride
	Abort             bool
	Request           *RequestOverride
	Response          *ResponseOverride
	Transform         ResponseTransform
	ResponseBodyLimit int64
	TransformTimeout  time.Duration
}
type NetworkHandler struct{ ID string }
type NetworkHandlerStats struct {
	Count, Shadowed int
	LastError       string
}
type networkHandlerEntry struct {
	id      string
	order   uint64
	options NetworkHandlerOptions
	stats   NetworkHandlerStats
	active  bool
}
type Response struct {
	URL          string
	Status       int
	Headers      map[string]string
	ResourceType string
}
type ResponseQuery struct{ URL, Status *Expectation }
type NetworkState struct {
	Offline                              bool
	Latency                              time.Duration
	DownloadThroughput, UploadThroughput float64
}

func (s *Session) RegisterNetworkHandler(ctx context.Context, options NetworkHandlerOptions) (NetworkHandler, error) {
	if _, err := MatchExpectation("", options.URL); err != nil {
		return NetworkHandler{}, &Error{Code: CodeInvalidArgument, Operation: "register network handler", Message: err.Error(), Cause: err}
	}
	actions := 0
	if options.Fulfill != nil {
		actions++
	}
	if options.Abort {
		actions++
	}
	if options.Request != nil {
		actions++
	}
	if options.Response != nil {
		actions++
	}
	if options.Transform != nil {
		actions++
	}
	if actions != 1 {
		return NetworkHandler{}, &Error{Code: CodeInvalidArgument, Operation: "register network handler", Message: "exactly one network action is required"}
	}
	if options.ResponseBodyLimit < 0 {
		return NetworkHandler{}, &Error{Code: CodeInvalidArgument, Operation: "register network handler", Message: "response body limit must not be negative"}
	}
	if options.TransformTimeout < 0 {
		return NetworkHandler{}, &Error{Code: CodeInvalidArgument, Operation: "register network handler", Message: "transform timeout must not be negative"}
	}
	for _, override := range []*ResponseOverride{options.Fulfill, options.Response} {
		if override != nil && override.Status != nil && (*override.Status < 100 || *override.Status > 599) {
			return NetworkHandler{}, &Error{Code: CodeInvalidArgument, Operation: "register network handler", Message: "response status must be between 100 and 599", Observed: *override.Status}
		}
		limit := options.ResponseBodyLimit
		if limit == 0 {
			limit = DefaultInterceptedBodyLimit
		}
		if override != nil && override.Body != nil && int64(len(*override.Body)) > limit {
			return NetworkHandler{}, &Error{Code: CodeInvalidArgument, Operation: "register network handler", Message: fmt.Sprintf("response body size %d exceeds limit %d", len(*override.Body), limit), Observed: len(*override.Body)}
		}
	}
	var handle NetworkHandler
	err := s.serial(ctx, "register network handler", func(op context.Context) error {
		s.networkMu.Lock()
		if s.networkHandlers == nil {
			s.networkHandlers = map[string]*networkHandlerEntry{}
		}
		s.networkSequence++
		id := fmt.Sprintf("%s-network-%d", s.targetID, s.networkSequence)
		s.networkHandlers[id] = &networkHandlerEntry{id: id, order: s.nextInterceptionOrder(), options: cloneNetworkHandlerOptions(options), active: true}
		s.networkOrder = append(s.networkOrder, id)
		s.networkMu.Unlock()
		handle.ID = id
		if err := s.ensureInterception(op); err != nil {
			s.networkMu.Lock()
			delete(s.networkHandlers, id)
			s.networkOrder = s.networkOrder[:len(s.networkOrder)-1]
			s.networkMu.Unlock()
			return err
		}
		return nil
	})
	return handle, err
}
func (s *Session) RemoveNetworkHandler(ctx context.Context, id string) error {
	return s.serial(ctx, "remove network handler", func(op context.Context) error {
		s.networkMu.Lock()
		handler := s.networkHandlers[id]
		if handler == nil || !handler.active {
			s.networkMu.Unlock()
			return &Error{Code: CodeInvalidArgument, Operation: "remove network handler", Message: "network handler not found"}
		}
		handler.active = false
		for i, value := range s.networkOrder {
			if value == id {
				s.networkOrder = append(s.networkOrder[:i], s.networkOrder[i+1:]...)
				break
			}
		}
		s.networkMu.Unlock()
		return s.disableInterceptionIfUnused(op)
	})
}
func (s *Session) NetworkHandlerStats(id string) (NetworkHandlerStats, error) {
	s.networkMu.Lock()
	defer s.networkMu.Unlock()
	h := s.networkHandlers[id]
	if h == nil {
		return NetworkHandlerStats{}, &Error{Code: CodeInvalidArgument, Operation: "network handler stats", Message: "network handler not found"}
	}
	return h.stats, nil
}
func (s *Session) selectNetworkHandler(url string, responseStage bool) *networkHandlerEntry {
	s.networkMu.Lock()
	defer s.networkMu.Unlock()
	var winner *networkHandlerEntry
	for _, id := range s.networkOrder {
		h := s.networkHandlers[id]
		isResponseHandler := h != nil && (h.options.Response != nil || h.options.Transform != nil)
		if h == nil || !h.active || isResponseHandler != responseStage {
			continue
		}
		matched, err := MatchExpectation(url, h.options.URL)
		if err != nil || !matched {
			continue
		}
		if winner == nil {
			winner = h
			h.stats.Count++
		} else {
			h.stats.Shadowed++
		}
	}
	return winner
}
func (s *Session) ensureInterception(ctx context.Context) error {
	s.holdMu.Lock()
	if s.fetchEnabled {
		s.holdMu.Unlock()
		return nil
	}
	s.holdMu.Unlock()
	if err := chromedp.Run(ctx, network.SetCacheDisabled(true), fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}})); err != nil {
		return err
	}
	s.holdMu.Lock()
	s.fetchEnabled = true
	s.holdMu.Unlock()
	return nil
}

func (s *Session) disableInterceptionIfUnused(ctx context.Context) error {
	s.networkMu.Lock()
	hasHandlers := false
	for _, handler := range s.networkHandlers {
		if handler.active {
			hasHandlers = true
			break
		}
	}
	cacheEnabled := s.cacheEnabled
	s.networkMu.Unlock()
	s.holdMu.Lock()
	hasHolds := false
	for _, hold := range s.holds {
		if !hold.released {
			hasHolds = true
			break
		}
	}
	enabled := s.fetchEnabled
	if !hasHandlers && !hasHolds {
		s.fetchEnabled = false
	}
	s.holdMu.Unlock()
	if !enabled || hasHandlers || hasHolds {
		return nil
	}
	return chromedp.Run(ctx, fetch.Disable(), network.SetCacheDisabled(!cacheEnabled))
}
func (s *Session) handlePausedEvent(event *fetch.EventRequestPaused) {
	if !s.eventsEnabled.Load() {
		if event.ResponseStatusCode != 0 || event.ResponseErrorReason != "" {
			go s.continueResponse(event.RequestID)
		} else {
			go s.continueRequest(event.RequestID)
		}
		return
	}
	if event.ResponseStatusCode != 0 || event.ResponseErrorReason != "" {
		if event.Request == nil {
			go s.continueResponse(event.RequestID)
			return
		}
		handler, hold := s.selectResponseOwner(event.Request.URL)
		if handler != nil {
			s.handleResponseModification(event, handler)
		} else {
			s.handlePausedResponse(event, hold)
		}
		return
	}
	go s.resolveRequest(event)
}
func (s *Session) resolveRequest(event *fetch.EventRequestPaused) {
	if event.Request == nil {
		s.continueRequest(event.RequestID)
		return
	}
	h := s.selectNetworkHandler(event.Request.URL, false)
	if h == nil {
		s.continueRequest(event.RequestID)
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(run context.Context) error {
		switch {
		case h.options.Fulfill != nil:
			o := h.options.Fulfill
			status := 200
			if o.Status != nil {
				status = *o.Status
			}
			headers := cloneStringMap(o.Headers)
			if o.Body != nil {
				stripEntityHeaders(headers)
			}
			p := fetch.FulfillRequest(event.RequestID, int64(status)).WithResponseHeaders(headerEntries(headers))
			if o.Body != nil {
				p = p.WithBody(base64.StdEncoding.EncodeToString(*o.Body))
			}
			return p.Do(run)
		case h.options.Abort:
			return fetch.FailRequest(event.RequestID, network.ErrorReasonFailed).Do(run)
		case h.options.Request != nil:
			o := h.options.Request
			p := fetch.ContinueRequest(event.RequestID).WithInterceptResponse(true)
			if o.URL != nil {
				p = p.WithURL(*o.URL)
			}
			if o.Method != nil {
				p = p.WithMethod(*o.Method)
			}
			if o.Body != nil {
				p = p.WithPostData(base64.StdEncoding.EncodeToString(*o.Body))
			}
			if o.Headers != nil {
				headers := map[string]string{}
				for k, v := range event.Request.Headers {
					headers[k] = fmt.Sprint(v)
				}
				for k, v := range o.Headers {
					headers[k] = v
				}
				if o.Body != nil {
					stripEntityHeaders(headers)
				}
				p = p.WithHeaders(headerEntries(headers))
			} else if o.Body != nil {
				headers := map[string]string{}
				for k, v := range event.Request.Headers {
					headers[k] = fmt.Sprint(v)
				}
				stripEntityHeaders(headers)
				p = p.WithHeaders(headerEntries(headers))
			}
			return p.Do(run)
		}
		return nil
	}))
	if err != nil {
		s.recordNetworkHandlerError(h, err)
		_ = chromedp.Run(ctx, fetch.FailRequest(event.RequestID, network.ErrorReasonFailed))
	}
}
func (s *Session) continueRequest(id fetch.RequestID) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	_ = chromedp.Run(ctx, fetch.ContinueRequest(id).WithInterceptResponse(true))
}
func (s *Session) handleResponseModification(event *fetch.EventRequestPaused, h *networkHandlerEntry) {
	go func() {
		timeout := 5 * time.Second
		if h.options.TransformTimeout > 0 {
			timeout = h.options.TransformTimeout
		}
		ctx, cancel := context.WithTimeout(s.ctx, timeout)
		defer cancel()
		o := ResponseOverride{}
		status := int(event.ResponseStatusCode)
		headers := map[string]string{}
		for _, entry := range event.ResponseHeaders {
			headers[entry.Name] = entry.Value
		}
		body, err := ResponseBodyContext(ctx, event.RequestID)
		if err != nil {
			s.recordNetworkHandlerError(h, err)
			s.continueResponse(event.RequestID)
			return
		}
		limit := h.options.ResponseBodyLimit
		if limit == 0 {
			limit = DefaultInterceptedBodyLimit
		}
		if int64(len(body)) > limit {
			err = fmt.Errorf("intercepted response body size %d exceeds limit %d", len(body), limit)
			s.recordNetworkHandlerError(h, err)
			s.continueResponse(event.RequestID)
			return
		}
		if h.options.Transform != nil {
			type transformResult struct {
				override ResponseOverride
				err      error
			}
			result := make(chan transformResult, 1)
			response := InterceptedResponse{
				URL: event.Request.URL, Status: status, Headers: cloneStringMap(headers), Body: append([]byte(nil), body...),
			}
			go func() {
				override, transformErr := h.options.Transform(ctx, response)
				result <- transformResult{override: override, err: transformErr}
			}()
			select {
			case transformed := <-result:
				o, err = transformed.override, transformed.err
				if err != nil {
					s.recordNetworkHandlerError(h, err)
					s.continueResponse(event.RequestID)
					return
				}
			case <-ctx.Done():
				s.recordNetworkHandlerError(h, ctx.Err())
				s.continueResponse(event.RequestID)
				return
			}
		} else {
			o = *h.options.Response
		}
		if o.Status != nil {
			status = *o.Status
		}
		if status < 100 || status > 599 {
			err = fmt.Errorf("response status %d is outside 100..599", status)
			s.recordNetworkHandlerError(h, err)
			s.continueResponse(event.RequestID)
			return
		}
		for k, v := range o.Headers {
			headers[k] = v
		}
		if o.Body != nil {
			if int64(len(*o.Body)) > limit {
				err = fmt.Errorf("replacement response body size %d exceeds limit %d", len(*o.Body), limit)
				s.recordNetworkHandlerError(h, err)
				s.continueResponse(event.RequestID)
				return
			}
			body = *o.Body
			stripEntityHeaders(headers)
		}
		if err := chromedp.Run(ctx, fetch.FulfillRequest(event.RequestID, int64(status)).WithResponseHeaders(headerEntries(headers)).WithBody(base64.StdEncoding.EncodeToString(body))); err != nil {
			s.recordNetworkHandlerError(h, err)
			s.continueResponse(event.RequestID)
		}
	}()
}

func (s *Session) selectResponseOwner(url string) (*networkHandlerEntry, *responseHold) {
	s.networkMu.Lock()
	handlers := make([]*networkHandlerEntry, 0)
	for _, id := range s.networkOrder {
		handler := s.networkHandlers[id]
		if handler == nil || !handler.active || (handler.options.Response == nil && handler.options.Transform == nil) {
			continue
		}
		matched, _ := MatchExpectation(url, handler.options.URL)
		if matched {
			handlers = append(handlers, handler)
		}
	}
	s.networkMu.Unlock()

	s.holdMu.Lock()
	var hold *responseHold
	for _, id := range s.holdOrder {
		candidate := s.holds[id]
		if candidate == nil || candidate.released {
			continue
		}
		matched, _ := MatchExpectation(url, candidate.expectation)
		if matched {
			hold = candidate
			break
		}
	}
	s.holdMu.Unlock()

	if len(handlers) == 0 && hold == nil {
		return nil, nil
	}
	if hold != nil && (len(handlers) == 0 || hold.order < handlers[0].order) {
		s.networkMu.Lock()
		for _, handler := range handlers {
			handler.stats.Shadowed++
		}
		s.networkMu.Unlock()
		return nil, hold
	}
	s.networkMu.Lock()
	handlers[0].stats.Count++
	for _, handler := range handlers[1:] {
		handler.stats.Shadowed++
	}
	s.networkMu.Unlock()
	return handlers[0], nil
}

func (s *Session) nextInterceptionOrder() uint64 {
	s.interceptionMu.Lock()
	defer s.interceptionMu.Unlock()
	s.interceptionSeq++
	return s.interceptionSeq
}

func (s *Session) recordNetworkHandlerError(handler *networkHandlerEntry, err error) {
	s.networkMu.Lock()
	handler.stats.LastError = err.Error()
	s.networkMu.Unlock()
}

func cloneNetworkHandlerOptions(options NetworkHandlerOptions) NetworkHandlerOptions {
	cloned := options
	if options.Fulfill != nil {
		value := cloneResponseOverride(*options.Fulfill)
		cloned.Fulfill = &value
	}
	if options.Request != nil {
		value := *options.Request
		value.Headers = cloneStringMap(options.Request.Headers)
		if options.Request.Body != nil {
			body := append([]byte(nil), (*options.Request.Body)...)
			value.Body = &body
		}
		cloned.Request = &value
	}
	if options.Response != nil {
		value := cloneResponseOverride(*options.Response)
		cloned.Response = &value
	}
	return cloned
}

func cloneResponseOverride(override ResponseOverride) ResponseOverride {
	override.Headers = cloneStringMap(override.Headers)
	if override.Body != nil {
		body := append([]byte(nil), (*override.Body)...)
		override.Body = &body
	}
	return override
}
func headerEntries(headers map[string]string) []*fetch.HeaderEntry {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*fetch.HeaderEntry, 0, len(keys))
	for _, k := range keys {
		out = append(out, &fetch.HeaderEntry{Name: k, Value: headers[k]})
	}
	return out
}

func stripEntityHeaders(headers map[string]string) {
	for name := range headers {
		switch strings.ToLower(name) {
		case "content-length", "content-encoding", "transfer-encoding":
			delete(headers, name)
		}
	}
}

func (s *Session) recordResponse(event *network.EventResponseReceived) {
	if event.Response == nil {
		return
	}
	headers := map[string]string{}
	for k, v := range event.Response.Headers {
		headers[k] = fmt.Sprint(v)
	}
	s.networkMu.Lock()
	s.responses = append(s.responses, Response{URL: event.Response.URL, Status: int(event.Response.Status), Headers: headers, ResourceType: event.Type.String()})
	s.networkMu.Unlock()
}
func (s *Session) Responses(q ResponseQuery) []Response {
	s.networkMu.Lock()
	items := append([]Response(nil), s.responses...)
	s.networkMu.Unlock()
	out := []Response{}
	for _, r := range items {
		ok := true
		for _, p := range []struct {
			v any
			e *Expectation
		}{{r.URL, q.URL}, {r.Status, q.Status}} {
			if p.e != nil {
				matched, _ := MatchExpectation(p.v, *p.e)
				ok = ok && matched
			}
		}
		if ok {
			r.Headers = cloneStringMap(r.Headers)
			out = append(out, r)
		}
	}
	return out
}
func (s *Session) trackRequest(id network.RequestID) {
	s.networkMu.Lock()
	if s.inflight == nil {
		s.inflight = map[network.RequestID]struct{}{}
	}
	s.inflight[id] = struct{}{}
	s.networkMu.Unlock()
}
func (s *Session) finishRequest(id network.RequestID) {
	s.networkMu.Lock()
	delete(s.inflight, id)
	s.networkMu.Unlock()
}
func (s *Session) InflightRequestCount() int {
	s.networkMu.Lock()
	defer s.networkMu.Unlock()
	return len(s.inflight)
}
func (s *Session) WaitForNetworkIdle(ctx context.Context, p PollPolicy) (PollResult, error) {
	return Poll(ctx, p, func(context.Context) (Observation, bool, error) {
		n := s.InflightRequestCount()
		return Observation{Value: n}, n == 0, nil
	})
}
func (s *Session) SetNetworkState(ctx context.Context, state NetworkState) error {
	if state.Latency < 0 || state.DownloadThroughput < 0 || state.UploadThroughput < 0 {
		return &Error{Code: CodeInvalidArgument, Operation: "set network state", Message: "latency and throughput must not be negative"}
	}
	return s.serial(ctx, "set network state", func(op context.Context) error {
		download, upload := state.DownloadThroughput, state.UploadThroughput
		if download == 0 {
			download = -1
		}
		if upload == 0 {
			upload = -1
		}
		return chromedp.Run(op, network.OverrideNetworkState(state.Offline, float64(state.Latency.Milliseconds()), download, upload))
	})
}
func (s *Session) ResetNetworkState(ctx context.Context) error {
	return s.SetNetworkState(ctx, NetworkState{})
}
func (s *Session) SetCacheEnabled(ctx context.Context, enabled bool) error {
	return s.serial(ctx, "set cache enabled", func(op context.Context) error {
		s.networkMu.Lock()
		s.cacheEnabled = enabled
		s.networkMu.Unlock()
		s.holdMu.Lock()
		intercepting := s.fetchEnabled
		s.holdMu.Unlock()
		return chromedp.Run(op, network.SetCacheDisabled(intercepting || !enabled))
	})
}
func (s *Session) resetNetworkState(ctx context.Context) error {
	s.networkMu.Lock()
	s.networkHandlers = nil
	s.networkOrder = nil
	s.responses = nil
	s.inflight = nil
	s.cacheEnabled = true
	s.networkMu.Unlock()
	return chromedp.Run(ctx, network.OverrideNetworkState(false, 0, -1, -1), network.SetCacheDisabled(false))
}
func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

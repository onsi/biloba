package biloba

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

/*
Request represents an HTTP request observed by a tab.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
type Request struct {
	URL          string
	Method       string
	Headers      map[string]string
	ResourceType string
}

func newRequest(r *network.Request, resourceType network.ResourceType) *Request {
	headers := map[string]string{}
	for k, v := range r.Headers {
		headers[k] = fmt.Sprint(v)
	}
	return &Request{
		URL:          r.URL,
		Method:       r.Method,
		Headers:      headers,
		ResourceType: resourceType.String(),
	}
}

/*
Requests represents a slice of *Request

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
type Requests []*Request

/*
Find returns the first request matching the passed-in RequestQuery (see [Biloba.HaveMadeRequest]), or nil if none match:

	req := b.AllRequests().Find(b.HaveMadeRequest(ContainSubstring("/api/users")).WithMethod("POST"))

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (r Requests) Find(query *RequestQuery) *Request {
	for _, req := range r {
		if query.matches(req) {
			return req
		}
	}
	return nil
}

/*
Filter returns a Requests slice containing all requests matching the passed-in RequestQuery (see [Biloba.HaveMadeRequest]):

	apiCalls := b.AllRequests().Filter(b.HaveMadeRequest(ContainSubstring("/api/")))

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (r Requests) Filter(query *RequestQuery) Requests {
	out := Requests{}
	for _, req := range r {
		if query.matches(req) {
			out = append(out, req)
		}
	}
	return out
}

/*
AllRequests() returns all requests observed by this tab since the last call to Prepare().

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) AllRequests() Requests {
	b.lock.Lock()
	defer b.lock.Unlock()
	out := make(Requests, len(b.requests))
	copy(out, b.requests)
	return out
}

/*
RequestQuery is a chainable query over observed requests, keyed on the request URL.  A single value plays two roles:

  - a Gomega matcher you assert against a tab - read it as [Biloba.HaveMadeRequest] (does this tab have a matching request?), and
  - a predicate you pass to [Requests.Find] / [Requests.Filter] - read it as [Biloba.RequestMatching] (does this one request match?).

Constrain it further by chaining WithMethod (more dimensions can be added the same way).  Every refinement applies to the same request.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
type RequestQuery struct {
	urlMatcher    types.GomegaMatcher
	methodMatcher types.GomegaMatcher
	observed      Requests
}

/*
RequestMatching() returns a [RequestQuery] keyed on the request URL.  url may be a string (exact match) or a Gomega matcher.  Use this spelling when the query reads as a predicate - i.e. when handing it to [Requests.Find] / [Requests.Filter]:

	req := b.AllRequests().Find(b.RequestMatching(ContainSubstring("/api/users")).WithMethod("GET"))

When you're asserting against a tab, the [Biloba.HaveMadeRequest] alias reads more naturally.  The two are interchangeable - they return the same query.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) RequestMatching(url any) *RequestQuery {
	return &RequestQuery{urlMatcher: matcherOrEqual(url)}
}

/*
HaveMadeRequest() is an alias for [Biloba.RequestMatching] that reads as an assertion.  Apply the returned [RequestQuery] to the tab so you can poll until the request has been made:

	Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/users")))
	Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/users")).WithMethod("POST"))

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) HaveMadeRequest(url any) *RequestQuery {
	return b.RequestMatching(url)
}

/*
WithMethod() refines the [RequestQuery] to also require the request's HTTP method to match.  method may be a string (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (q *RequestQuery) WithMethod(method any) *RequestQuery {
	return &RequestQuery{
		urlMatcher:    q.urlMatcher,
		methodMatcher: matcherOrEqual(method),
	}
}

// matches is the predicate role: does this single request satisfy every constraint?
func (q *RequestQuery) matches(req *Request) bool {
	if match, _ := q.urlMatcher.Match(req.URL); !match {
		return false
	}
	if q.methodMatcher != nil {
		if match, _ := q.methodMatcher.Match(req.Method); !match {
			return false
		}
	}
	return true
}

// Match is the Gomega matcher role: does the tab have any request that matches?
func (q *RequestQuery) Match(actual any) (bool, error) {
	tab, ok := actual.(*Biloba)
	if !ok {
		return false, fmt.Errorf("HaveMadeRequest must be passed a Biloba tab.  Got:\n%s", format.Object(actual, 1))
	}
	q.observed = tab.AllRequests()
	return q.observed.Find(q) != nil, nil
}

func (q *RequestQuery) description() string {
	out := &strings.Builder{}
	fmt.Fprintf(out, "have made a request with URL matching %s", q.urlMatcher.FailureMessage(""))
	if q.methodMatcher != nil {
		fmt.Fprintf(out, "\nand Method matching %s", q.methodMatcher.FailureMessage(""))
	}
	return normalizeWhitespace(out.String())
}

func (q *RequestQuery) presentRequests() string {
	if len(q.observed) == 0 {
		return "The tab has not made any requests."
	}
	out := &strings.Builder{}
	out.WriteString("The requests the tab has made were:")
	for _, req := range q.observed {
		fmt.Fprintf(out, "\n%s %s", req.Method, req.URL)
	}
	return out.String()
}

func (q *RequestQuery) FailureMessage(actual any) string {
	return fmt.Sprintf("Expected the tab to %s.\n%s", q.description(), q.presentRequests())
}

func (q *RequestQuery) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf("Expected the tab not to %s, but it did.", q.description())
}

/*
BeNetworkIdle() is a matcher that passes when this tab has no in-flight requests.  Apply it to the tab itself and poll to wait for the network to settle:

	Eventually(b).Should(b.BeNetworkIdle())

Note: Biloba considers a tab idle the instant its in-flight request count reaches zero - it does not wait for a quiet period.  This is a deliberately pragmatic definition; if you need to wait for a specific request to complete, assert on it directly.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) BeNetworkIdle() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		b.lock.Lock()
		defer b.lock.Unlock()
		return len(b.inflightRequests) == 0, nil
	}).WithTemplate("Expected the tab to be network idle, but it has {{.Data}} in-flight request(s).", func() int {
		b.lock.Lock()
		defer b.lock.Unlock()
		return len(b.inflightRequests)
	}())
}

/*
StubResponse describes the response that a stubbed request should return.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
type StubResponse struct {
	Status  int               // the HTTP status code to return (defaults to 200)
	Body    string            // the response body
	Headers map[string]string // response headers (e.g. {"Content-Type": "application/json"})
}

func (r StubResponse) headerEntries() []*fetch.HeaderEntry {
	out := []*fetch.HeaderEntry{}
	for name, value := range r.Headers {
		out = append(out, &fetch.HeaderEntry{Name: name, Value: value})
	}
	return out
}

// requestHandler is one entry in a tab's ordered, first-match-wins list of network handlers.
// Every handler matches on the request URL; exactly one of the action fields is set, which
// determines how a matching paused (request-stage) request is resolved:
//
//   - stub        → fulfill with a canned StubResponse (see StubRequest)
//   - abort       → fail the request, simulating a network error (see AbortRequest)
//   - modify      → continue with the provided request overrides (see ModifyRequest)
//
// Response-stage interception (ModifyResponse) is tracked separately in responseHandlers.
type requestHandler struct {
	matcher types.GomegaMatcher
	stub    *StubResponse
	abort   bool
	modify  *RequestModification
}

/*
StubRequest intercepts requests whose URL matches url and fulfills them with the provided StubResponse instead of hitting the network.  url may be a string (exact match) or a Gomega matcher (e.g. ContainSubstring("/api/users")):

	b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{
		Body:    `[{"name": "Jane"}]`,
		Headers: map[string]string{"Content-Type": "application/json"},
	})

Stubs are scoped to the tab they are registered on and are cleared by Prepare().  Requests that match no stub are passed through to the real network.  Registering the first stub on a tab enables request interception for that tab, which pauses and resumes every request the tab makes - so only stub when you need to.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) StubRequest(url any, response StubResponse) {
	b.gt.Helper()
	if response.Status == 0 {
		response.Status = http.StatusOK
	}
	b.lock.Lock()
	resp := response
	b.requestHandlers = append(b.requestHandlers, &requestHandler{matcher: matcherOrEqual(url), stub: &resp})
	b.lock.Unlock()
	b.ensureFetchEnabled()
}

/*
AbortRequest fails any request whose URL matches url, simulating a network failure: the page's
fetch/XHR rejects exactly as it would if the request could not be made.  url may be a string
(exact match) or a Gomega matcher (e.g. ContainSubstring("/api/users")):

	b.AbortRequest(ContainSubstring("/api/users"))

Like StubRequest, AbortRequest is scoped to the tab, cleared by Prepare(), and enables request
interception.  Handlers are first-match-wins in registration order, so register your aborts and
stubs in the order you want them consulted.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) AbortRequest(url any) {
	b.gt.Helper()
	b.lock.Lock()
	b.requestHandlers = append(b.requestHandlers, &requestHandler{matcher: matcherOrEqual(url), abort: true})
	b.lock.Unlock()
	b.ensureFetchEnabled()
}

/*
RequestModification is a chainable builder describing how to rewrite a matching request before it
goes out on the wire.  Build one with [Biloba.ModifyRequest] and chain WithURL/WithMethod/WithHeader/WithBody.
Only the overrides you set are applied; everything else passes through unchanged.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
type RequestModification struct {
	url     *string
	method  *string
	body    *string
	headers []*fetch.HeaderEntry
}

/*
ModifyRequest intercepts requests whose URL matches url and continues them to the real network with
the overrides accumulated on the returned [RequestModification] builder.  url may be a string
(exact match) or a Gomega matcher:

	b.ModifyRequest(ContainSubstring("/api/users")).
		WithMethod("POST").
		WithHeader("X-Test", "true").
		WithBody(`{"name":"Jane"}`)

Like StubRequest, ModifyRequest is scoped to the tab, cleared by Prepare(), enables request
interception, and participates in the same first-match-wins handler list.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) ModifyRequest(url any) *RequestModification {
	b.gt.Helper()
	mod := &RequestModification{}
	b.lock.Lock()
	b.requestHandlers = append(b.requestHandlers, &requestHandler{matcher: matcherOrEqual(url), modify: mod})
	b.lock.Unlock()
	b.ensureFetchEnabled()
	return mod
}

// WithURL overrides the request URL (the change is not observable by the page).
func (m *RequestModification) WithURL(url string) *RequestModification {
	m.url = &url
	return m
}

// WithMethod overrides the request's HTTP method.
func (m *RequestModification) WithMethod(method string) *RequestModification {
	m.method = &method
	return m
}

// WithHeader sets (or adds) a request header.  May be called repeatedly to accumulate headers.
func (m *RequestModification) WithHeader(name, value string) *RequestModification {
	m.headers = append(m.headers, &fetch.HeaderEntry{Name: name, Value: value})
	return m
}

// WithBody overrides the request's post data.
func (m *RequestModification) WithBody(body string) *RequestModification {
	m.body = &body
	return m
}

func (m *RequestModification) apply(id fetch.RequestID) *fetch.ContinueRequestParams {
	params := fetch.ContinueRequest(id)
	if m.url != nil {
		params = params.WithURL(*m.url)
	}
	if m.method != nil {
		params = params.WithMethod(*m.method)
	}
	if len(m.headers) > 0 {
		params = params.WithHeaders(m.headers)
	}
	if m.body != nil {
		params = params.WithPostData(base64.StdEncoding.EncodeToString([]byte(*m.body)))
	}
	return params
}

/*
InterceptedResponse is the real response handed to a [ResponseModification.Using] transform.  It
carries the upstream Status, Headers, and Body so you can read them and return a modified [StubResponse].

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
type InterceptedResponse struct {
	Status  int
	Headers map[string]string
	Body    string
}

/*
ResponseModification is a chainable builder describing how to rewrite a matching real response as it
comes back.  Build one with [Biloba.ModifyResponse] and either chain WithStatus/WithHeader/WithBody,
or supply a transform with Using(func(InterceptedResponse) StubResponse) to read the real response
and return a fully-formed replacement.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
type ResponseModification struct {
	matcher types.GomegaMatcher
	status  *int
	body    *string
	headers map[string]string
	using   func(InterceptedResponse) StubResponse
}

/*
ModifyResponse intercepts the real response to requests whose URL matches url and fulfills the page
with a modified version of it.  url may be a string (exact match) or a Gomega matcher.

Chain WithStatus/WithHeader/WithBody to override pieces of the real response:

	b.ModifyResponse(ContainSubstring("/api/users")).WithStatus(503)

Or supply a transform that reads the real response and returns a replacement:

	b.ModifyResponse(ContainSubstring("/api/users")).Using(func(r biloba.InterceptedResponse) biloba.StubResponse {
		return biloba.StubResponse{Status: r.Status, Body: strings.ToUpper(r.Body), Headers: r.Headers}
	})

ModifyResponse enables response-stage interception for the tab (a heavier mode than request stubbing,
since the tab pauses at both the request and response stages).  It is scoped to the tab and cleared by
Prepare().  Handlers are first-match-wins in registration order.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) ModifyResponse(url any) *ResponseModification {
	b.gt.Helper()
	mod := &ResponseModification{matcher: matcherOrEqual(url)}
	b.lock.Lock()
	b.responseHandlers = append(b.responseHandlers, mod)
	b.lock.Unlock()
	b.ensureFetchEnabled()
	return mod
}

// WithStatus overrides the response status code.
func (m *ResponseModification) WithStatus(status int) *ResponseModification {
	m.status = &status
	return m
}

// WithHeader sets (or replaces) a response header.  May be called repeatedly to accumulate headers.
func (m *ResponseModification) WithHeader(name, value string) *ResponseModification {
	if m.headers == nil {
		m.headers = map[string]string{}
	}
	m.headers[name] = value
	return m
}

// WithBody overrides the response body.
func (m *ResponseModification) WithBody(body string) *ResponseModification {
	m.body = &body
	return m
}

// Using supplies a transform that receives the real (intercepted) response and returns the
// replacement StubResponse.  When set, Using takes precedence over WithStatus/WithHeader/WithBody.
func (m *ResponseModification) Using(transform func(InterceptedResponse) StubResponse) *ResponseModification {
	m.using = transform
	return m
}

// resolve computes the final StubResponse to fulfill with, given the real intercepted response.
func (m *ResponseModification) resolve(original InterceptedResponse) StubResponse {
	if m.using != nil {
		out := m.using(original)
		if out.Status == 0 {
			out.Status = http.StatusOK
		}
		return out
	}
	out := StubResponse{Status: original.Status, Body: original.Body, Headers: map[string]string{}}
	for k, v := range original.Headers {
		out.Headers[k] = v
	}
	if m.status != nil {
		out.Status = *m.status
	}
	if m.body != nil {
		out.Body = *m.body
	}
	for k, v := range m.headers {
		out.Headers[k] = v
	}
	return out
}

// ensureFetchEnabled turns on the Fetch domain once, with a single request-stage "*" pattern that
// pauses every request the tab makes.  Response interception is driven per-request, not by a global
// response-stage pattern: when a request's URL has a matching ModifyResponse handler, the
// request-stage continue sets interceptResponse=true so that one request pauses again at the response
// stage (see handleRequestStagePause).  This keeps request- and response-stage handling from
// double-pausing unrelated requests.
func (b *Biloba) ensureFetchEnabled() {
	b.gt.Helper()
	b.lock.Lock()
	needEnable := !b.fetchEnabled
	b.fetchEnabled = true
	b.lock.Unlock()

	if !needEnable {
		return
	}

	if err := chromedp.Run(b.Context, fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*"}})); err != nil {
		b.gt.Fatalf("Failed to enable network interception:\n%s", err.Error())
	}
}

func (b *Biloba) requestHandlerFor(url string) *requestHandler {
	b.lock.Lock()
	defer b.lock.Unlock()
	for _, h := range b.requestHandlers {
		if match, _ := h.matcher.Match(url); match {
			return h
		}
	}
	return nil
}

func (b *Biloba) responseHandlerFor(url string) *ResponseModification {
	b.lock.Lock()
	defer b.lock.Unlock()
	for _, h := range b.responseHandlers {
		if match, _ := h.matcher.Match(url); match {
			return h
		}
	}
	return nil
}

// handleEventRequestPaused responds to a paused request.  With response-stage interception enabled a
// request pauses twice: once at the request stage (ResponseStatusCode/ResponseErrorReason unset) and
// again at the response stage (those fields populated).  We route on the stage so request-stage
// handlers (stub/abort/modify) and response-stage handlers (ModifyResponse) coexist without hanging
// the page.  Because the listener runs on the target's event loop, issuing the CDP response
// synchronously here would deadlock - so we always resolve in a goroutine.
func (b *Biloba) handleEventRequestPaused(ev *fetch.EventRequestPaused) {
	isResponseStage := ev.ResponseStatusCode != 0 || ev.ResponseErrorReason != ""
	if isResponseStage {
		b.handleResponseStagePause(ev)
		return
	}
	b.handleRequestStagePause(ev)
}

func (b *Biloba) handleRequestStagePause(ev *fetch.EventRequestPaused) {
	handler := b.requestHandlerFor(ev.Request.URL)
	go func() {
		// When a response handler matches this URL, the request-stage continue must opt into
		// response interception so the request pauses again at the response stage.  (A request-stage
		// "*" pattern matches first and would otherwise consume the request before the response-stage
		// pattern could fire.)  A stub/abort short-circuits the real network, so it never reaches the
		// response stage and doesn't need this.
		interceptResponse := b.responseHandlerFor(ev.Request.URL) != nil
		var action chromedp.Action
		switch {
		case handler == nil:
			cr := fetch.ContinueRequest(ev.RequestID)
			if interceptResponse {
				cr = cr.WithInterceptResponse(true)
			}
			action = cr
		case handler.abort:
			action = fetch.FailRequest(ev.RequestID, network.ErrorReasonBlockedByClient)
		case handler.modify != nil:
			cr := handler.modify.apply(ev.RequestID)
			if interceptResponse {
				cr = cr.WithInterceptResponse(true)
			}
			action = cr
		case handler.stub != nil:
			params := fetch.FulfillRequest(ev.RequestID, int64(handler.stub.Status)).
				WithBody(base64.StdEncoding.EncodeToString([]byte(handler.stub.Body)))
			if headers := handler.stub.headerEntries(); len(headers) > 0 {
				params = params.WithResponseHeaders(headers)
			}
			action = params
		default:
			action = fetch.ContinueRequest(ev.RequestID)
		}
		chromedp.Run(b.Context, action)
	}()
}

func (b *Biloba) handleResponseStagePause(ev *fetch.EventRequestPaused) {
	handler := b.responseHandlerFor(ev.Request.URL)
	go func() {
		if handler == nil {
			// Not ours to modify: hand the real response straight back to the page.
			chromedp.Run(b.Context, fetch.ContinueResponse(ev.RequestID))
			return
		}

		original := InterceptedResponse{
			Status:  int(ev.ResponseStatusCode),
			Headers: map[string]string{},
		}
		for _, h := range ev.ResponseHeaders {
			original.Headers[h.Name] = h.Value
		}
		// GetResponseBody is only valid at the response stage; chromedp decodes base64 for us.  It
		// must run through chromedp.Run so it picks up the target's CDP executor from the context.
		var body []byte
		chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			body, err = fetch.GetResponseBody(ev.RequestID).Do(ctx)
			return err
		}))
		original.Body = string(body)

		response := handler.resolve(original)
		if response.Status == 0 {
			response.Status = http.StatusOK
		}
		params := fetch.FulfillRequest(ev.RequestID, int64(response.Status)).
			WithBody(base64.StdEncoding.EncodeToString([]byte(response.Body)))
		if headers := response.headerEntries(); len(headers) > 0 {
			params = params.WithResponseHeaders(headers)
		}
		chromedp.Run(b.Context, params)
	}()
}

func (b *Biloba) handleEventRequestWillBeSent(ev *network.EventRequestWillBeSent) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.requests = append(b.requests, newRequest(ev.Request, ev.Type))
	b.inflightRequests[ev.RequestID] = true
}

func (b *Biloba) handleEventLoadingFinished(ev *network.EventLoadingFinished) {
	b.lock.Lock()
	defer b.lock.Unlock()
	delete(b.inflightRequests, ev.RequestID)
}

func (b *Biloba) handleEventLoadingFailed(ev *network.EventLoadingFailed) {
	b.lock.Lock()
	defer b.lock.Unlock()
	delete(b.inflightRequests, ev.RequestID)
}

package biloba

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	ginkgotypes "github.com/onsi/ginkgo/v2/types"
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
	b.guardConfig("AllRequests")
	b.lock.Lock()
	defer b.lock.Unlock()
	out := make(Requests, len(b.state.requests))
	copy(out, b.state.requests)
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
		return len(b.state.inflightRequests) == 0, nil
	}).WithTemplate("Expected the tab to be network idle, but it has {{.Data}} in-flight request(s).", func() int {
		b.lock.Lock()
		defer b.lock.Unlock()
		return len(b.state.inflightRequests)
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

	prov handlerProvenance
}

// handlerProvenance is the registration-site bookkeeping every network handler carries.  It exists
// solely for the on-failure shadowed-handler diagnostic: a handler that never won a single dispatch
// AND lost at least one to an earlier handler is silently dead code, which is precisely the
// "fails downstream, points nowhere useful" shape the poll-trajectory and occluded-click notes exist
// to kill.  api/stage/location are immutable after registration; fired/shadowed/shadower are mutated
// by the dispatch lookups and are therefore guarded by b.lock.
type handlerProvenance struct {
	api      string                   // the API that registered this handler (StubRequest, ModifyResponse, ...)
	stage    string                   // "request" or "response" - the noun the note uses
	location ginkgotypes.CodeLocation // the user's registration site

	fired    int                // dispatches this handler actually claimed
	shadowed int                // dispatches it matched but an earlier handler claimed first
	shadower *handlerProvenance // the earlier handler that claimed the first of them
}

// newHandlerProvenance captures the user's registration site.  skip is relative to the exported
// registration method, which is always the direct caller (b.StubRequest(...) et al are called from
// the spec), so every call site passes 2: one frame for newHandlerProvenance, one for the method.
func newHandlerProvenance(api, stage string) handlerProvenance {
	return handlerProvenance{api: api, stage: stage, location: ginkgotypes.NewCodeLocation(2)}
}

// indefiniteArticle keeps the note grammatical across the five registration APIs ("An AbortRequest
// handler", "A ModifyResponse handler").
func indefiniteArticle(word string) string {
	if word != "" && strings.ContainsRune("AEIOU", rune(word[0])) {
		return "An"
	}
	return "A"
}

// recordShadowed notes that this handler matched a dispatch that an earlier handler claimed first.
// Called under b.lock from the dispatch lookups.
func (p *handlerProvenance) recordShadowed(winner *handlerProvenance) {
	p.shadowed++
	if p.shadower == nil {
		p.shadower = winner
	}
}

// firedCount is the read side of the same counter the shadowed-handler diagnostic keeps: how many
// dispatches this handler actually claimed.  Every network-handler handle exposes it as Count() so a
// spec can assert its handler fired rather than trusting that it did - a typo'd URL otherwise passes
// silently to the real network.  fired is guarded by b.lock, and the dispatch goroutines write it, so
// take the lock to read it.
func firedCount(b *Biloba, prov *handlerProvenance) int {
	b.lock.Lock()
	defer b.lock.Unlock()
	return prov.fired
}

/*
RequestStub is the handle [Biloba.StubRequest] returns.  Its one job is [RequestStub.Count]: proof that
the stub you registered actually answered a request.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
type RequestStub struct {
	b    *Biloba
	prov *handlerProvenance
}

/*
Count returns how many requests this stub has fulfilled.  It is a snapshot - it takes no poll-config
knobs - but it is safe to poll, which is how a spec asserts its stub actually fired:

	stub := b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `[]`})
	b.Click("#refresh")
	Eventually(stub.Count).Should(Equal(1))

Without this the usual failure mode is silent: a URL that matches nothing goes straight to the real
network and the spec passes for the wrong reason.  Note that Count is a fact about *this* handler, not
about the URL: handlers are first-match-wins, so a stub shadowed by an earlier one stays at 0.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (s *RequestStub) Count() int { return firedCount(s.b, s.prov) }

/*
RequestAbort is the handle [Biloba.AbortRequest] returns.  Its one job is [RequestAbort.Count]: proof
that the abort you registered actually failed a request.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
type RequestAbort struct {
	b    *Biloba
	prov *handlerProvenance
}

/*
Count returns how many requests this handler has aborted.  It is a snapshot - it takes no poll-config
knobs - but it is safe to poll:

	abort := b.AbortRequest(ContainSubstring("/api/users"))
	b.Click("#refresh")
	Eventually(abort.Count).Should(Equal(1))

See [RequestStub.Count] for why asserting on it is worth the line.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (a *RequestAbort) Count() int { return firedCount(a.b, a.prov) }

/*
StubRequest intercepts requests whose URL matches url and fulfills them with the provided StubResponse instead of hitting the network.  url may be a string (exact match) or a Gomega matcher (e.g. ContainSubstring("/api/users")):

	b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{
		Body:    `[{"name": "Jane"}]`,
		Headers: map[string]string{"Content-Type": "application/json"},
	})

Stubs are scoped to the tab they are registered on and are cleared by Prepare().  Requests that match no stub are passed through to the real network.  Registering the first stub on a tab enables request interception for that tab, which pauses and resumes every request the tab makes - so only stub when you need to.

It returns a [RequestStub] handle you can ignore, or keep to assert the stub actually fired:

	stub := b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `[]`})
	b.Click("#refresh")
	Eventually(stub.Count).Should(Equal(1))

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) StubRequest(url any, response StubResponse) *RequestStub {
	b.gt.Helper()
	b.guardConfig("StubRequest")
	if response.Status == 0 {
		response.Status = http.StatusOK
	}
	b.lock.Lock()
	resp := response
	handler := &requestHandler{matcher: matcherOrEqual(url), stub: &resp, prov: newHandlerProvenance("StubRequest", "request")}
	b.state.requestHandlers = append(b.state.requestHandlers, handler)
	b.lock.Unlock()
	b.ensureFetchEnabled()
	return &RequestStub{b: b, prov: &handler.prov}
}

/*
AbortRequest fails any request whose URL matches url, simulating a network failure: the page's
fetch/XHR rejects exactly as it would if the request could not be made.  url may be a string
(exact match) or a Gomega matcher (e.g. ContainSubstring("/api/users")):

	b.AbortRequest(ContainSubstring("/api/users"))

Like StubRequest, AbortRequest is scoped to the tab, cleared by Prepare(), and enables request
interception.  Handlers are first-match-wins in registration order, so register your aborts and
stubs in the order you want them consulted.

It returns a [RequestAbort] handle you can ignore, or keep to assert the abort actually fired with
Eventually(abort.Count).Should(Equal(1)).

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) AbortRequest(url any) *RequestAbort {
	b.gt.Helper()
	b.guardConfig("AbortRequest")
	b.lock.Lock()
	handler := &requestHandler{matcher: matcherOrEqual(url), abort: true, prov: newHandlerProvenance("AbortRequest", "request")}
	b.state.requestHandlers = append(b.state.requestHandlers, handler)
	b.lock.Unlock()
	b.ensureFetchEnabled()
	return &RequestAbort{b: b, prov: &handler.prov}
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

	b    *Biloba
	prov *handlerProvenance
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
	b.guardConfig("ModifyRequest")
	mod := &RequestModification{b: b}
	b.lock.Lock()
	handler := &requestHandler{matcher: matcherOrEqual(url), modify: mod, prov: newHandlerProvenance("ModifyRequest", "request")}
	mod.prov = &handler.prov
	b.state.requestHandlers = append(b.state.requestHandlers, handler)
	b.lock.Unlock()
	b.ensureFetchEnabled()
	return mod
}

/*
Count returns how many requests this handler has modified.  It is a snapshot - it takes no poll-config
knobs - but it is safe to poll:

	mod := b.ModifyRequest(ContainSubstring("/api/users")).WithMethod("POST")
	b.Click("#refresh")
	Eventually(mod.Count).Should(Equal(1))

See [RequestStub.Count] for why asserting on it is worth the line.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (m *RequestModification) Count() int { return firedCount(m.b, m.prov) }

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
	hold    *ResponseHold // set when this handler was registered by HoldResponse

	b    *Biloba
	prov handlerProvenance
}

/*
Count returns how many responses this handler has modified.  It is a snapshot - it takes no
poll-config knobs - but it is safe to poll:

	mod := b.ModifyResponse(ContainSubstring("/api/users")).WithStatus(503)
	b.Click("#refresh")
	Eventually(mod.Count).Should(Equal(1))

See [RequestStub.Count] for why asserting on it is worth the line.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (m *ResponseModification) Count() int { return firedCount(m.b, &m.prov) }

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
Prepare().  Handlers are first-match-wins in registration order: once a handler matches a URL, later
handlers for that same URL are never consulted.  Watch for this inside an Ordered container, where
Prepare() runs only once (OncePerOrdered) - a handler registered by an earlier It is still registered
and will silently shadow an identical handler registered by a later It.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) ModifyResponse(url any) *ResponseModification {
	b.gt.Helper()
	b.guardConfig("ModifyResponse")
	mod := &ResponseModification{b: b, matcher: matcherOrEqual(url), prov: newHandlerProvenance("ModifyResponse", "response")}
	b.lock.Lock()
	b.state.responseHandlers = append(b.state.responseHandlers, mod)
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
	maps.Copy(out.Headers, original.Headers)
	if m.status != nil {
		out.Status = *m.status
	}
	if m.body != nil {
		out.Body = *m.body
	}
	maps.Copy(out.Headers, m.headers)
	return out
}

// holdResponseTimeout is the default deadline for ResponseHold.Await().  Awaiting an intercepted
// response is a Cat 5a waiting command: it keeps this generous purpose-built deadline rather than
// inheriting Gomega's 1s default, and honors WithTimeout/WithContext.
var holdResponseTimeout = 30 * time.Second

// heldEntry is one response this hold took ownership of.  Each entry owns its own release channel so
// responses can be released one at a time (see ResponseHold.Release/ReleaseNext) rather than all at
// once.  released is the single source of truth for "has this entry's channel been closed?" - closing
// is always funneled through releaseEntry so a terminal Release followed by forceRelease can never
// double-close.  Matches that passed straight through because the hold was at its Limit never become
// entries: the hold never held them.
type heldEntry struct {
	response InterceptedResponse
	release  chan struct{}
	released bool
}

/*
ResponseHold is the handle returned by [Biloba.HoldResponse].  It lets a spec block a matching
response in flight, wait for it to arrive, count how many have matched, and then let them through -
all of them, or one at a time.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
type ResponseHold struct {
	b       *Biloba
	matcher types.GomegaMatcher

	lock     sync.Mutex
	count    int           // every matching response intercepted so far, held or not
	passed   int           // of those, the ones that were never frozen (at the Limit, or after a terminal Release)
	limit    int           // at most this many responses may be held concurrently (0 = unlimited)
	held     []*heldEntry  // every response this hold has taken ownership of, oldest first (released ones included)
	blocked  int           // how many goroutines are currently parked inside the hold
	released bool          // a bare Release() was called: the hold is terminal and holds nothing further
	notify   chan struct{} // closed-and-replaced on every arrival, and when a bare Release goes terminal, so Await can wake without polling
}

/*
HoldResponse intercepts the real response to requests whose URL matches url and holds it hostage -
the response is paused in flight until you Release it, at which point it is passed through to the
page unchanged.  url may be a string (exact match) or a Gomega matcher.  This is the tool for
forcing an arrival order when testing optimistic-UI reconciliation:

	hold := b.HoldResponse(ContainSubstring("/api/users"))
	b.Click("#refresh")
	hold.Await()                            // block until the response is actually being held
	b.Click("#rename")                      // drive the app into the racy window
	hold.Release()                          // now let the stale response land
	Expect(hold.Count()).To(Equal(1))

By default a hold is all-or-nothing: it freezes EVERY matching response, not just the first, and a
bare Release() frees all of them at once and stops holding future matches.  So a second request to
the same URL is frozen too - if you need it to fly past while the first stays held, cap the hold with
[ResponseHold.Limit].  To let held responses go one at a time (and keep the hold armed) use
[ResponseHold.ReleaseNext] or pass the response to [ResponseHold.Release].

HoldResponse builds on [Biloba.ModifyResponse], so the same rules apply: it is scoped to the tab,
cleared by Prepare(), and participates in the same first-match-wins handler list.  Matching is
tab-wide and URL-based, so a hold can catch a response belonging to an *earlier* page load - a URL
substring does not identify a page generation.  Scope it (drive the flow in a dedicated b.NewTab())
or assert Count() when that matters.  First-match-wins also means a second HoldResponse for a URL an
earlier one already claims is dead code: re-arm the hold you have with ReleaseNext instead.

A held response is a real Chrome pause, so Biloba force-releases every hold at the end of the spec
(and again in Prepare()) - a failing spec can never wedge the tab for the specs that follow.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) HoldResponse(url any) *ResponseHold {
	b.gt.Helper()
	// the two knobs HoldResponse accepts are the two Await honors - it stashes them for the wait.
	b.guardConfig("HoldResponse", knobTimeout, knobContext)
	h := &ResponseHold{
		b:       b,
		matcher: matcherOrEqual(url),
		notify:  make(chan struct{}),
	}
	mod := &ResponseModification{b: b, matcher: h.matcher, hold: h, using: h.intercept, prov: newHandlerProvenance("HoldResponse", "response")}
	b.lock.Lock()
	b.state.responseHandlers = append(b.state.responseHandlers, mod)
	b.lock.Unlock()
	b.ensureFetchEnabled()
	b.gt.DeferCleanup(h.forceRelease)
	return h
}

/*
Limit caps how many matching responses this hold may freeze at the same time.  While n are already
held, further matches pass straight through to the page untouched (they still count).  Limit(1) is
what makes "hold response #1 while response #2 lands" expressible - the ordering a default hold can
never produce, since it freezes #2 as well:

	hold := b.HoldResponse(ContainSubstring("/api/save")).Limit(1)
	b.Click("#save")                                 // the first save's response is held
	hold.Await()
	b.Click("#save")                                 // the second save's response flies past and lands
	Eventually(hold.PassedThrough).Should(Equal(1))  // ...and this is how you say so
	hold.Release()                                   // now the FIRST response lands, last

Releasing a held response frees up capacity, so the next match is held again.  n must be at least 1;
by default a hold is unlimited.  A response that flew past under the cap is counted by
[ResponseHold.PassedThrough] - assert on that rather than on the total, so raising the limit breaks the
spec instead of silently changing what it tests.

Two edges worth knowing: the hold starts intercepting the moment [Biloba.HoldResponse] returns, so a
response already in flight when the chained .Limit(n) runs can be held before the cap is set (only
reachable if you register a hold against traffic that is already moving).  And the cap applies to
matches as they arrive - lowering it later never releases responses already held, and raising it never
retroactively holds anything that has already passed through.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (h *ResponseHold) Limit(n int) *ResponseHold {
	h.b.gt.Helper()
	h.b.guardConfig("Limit", knobTimeout, knobContext)
	if n < 1 {
		h.b.gt.Fatalf("HoldResponse(...).Limit(%d) is invalid - a hold must be allowed to hold at least one response.\nPass a limit of 1 or more, or drop Limit entirely to hold every matching response.", n)
		return h
	}
	h.lock.Lock()
	h.limit = n
	h.lock.Unlock()
	return h
}

// intercept is the ResponseModification transform HoldResponse registers.  It runs on the goroutine
// handleResponseStagePause spun up for this paused response (never on the CDP event loop, and never
// while holding b.lock) so parking here blocks nothing but this one response.
func (h *ResponseHold) intercept(r InterceptedResponse) StubResponse {
	passthrough := StubResponse{Status: r.Status, Body: r.Body, Headers: r.Headers}

	h.lock.Lock()
	h.count++
	if h.limit > 0 && h.holdingCount() >= h.limit {
		// at capacity: this match was never this hold's business.  It passes straight through and is
		// not recorded as an entry - Await must not hand back a response the hold never held.
		h.passed++
		h.lock.Unlock()
		return passthrough
	}
	entry := &heldEntry{response: r, release: make(chan struct{})}
	h.held = append(h.held, entry)
	h.signal()
	if h.released {
		// terminal: the hold is done holding, but the entry is still recorded so a post-Release Await
		// keeps returning the first intercepted response rather than blocking forever.  It was never
		// actually frozen, though, so it counts as passed through.
		h.passed++
		h.releaseEntry(entry)
		h.lock.Unlock()
		return passthrough
	}
	h.blocked++
	h.lock.Unlock()
	// the entry was appended, and the decision to park was made, under the same lock hold - so a
	// concurrent release can always see this entry and close its channel.  No wakeup can be lost.

	defer func() {
		h.lock.Lock()
		h.blocked--
		h.lock.Unlock()
	}()

	select {
	case <-entry.release:
	case <-h.b.Context.Done(): // the tab is going away - never hang the suite waiting on a dead tab
	}
	return passthrough
}

// holdingCount is how many entries are still frozen.  Must be called with h.lock held.
func (h *ResponseHold) holdingCount() int {
	n := 0
	for _, e := range h.held {
		if !e.released {
			n++
		}
	}
	return n
}

// releaseEntry frees one held response.  It is the only place an entry's channel is closed, so it can
// safely be called on an entry that is already released.  Must be called with h.lock held.
func (h *ResponseHold) releaseEntry(e *heldEntry) {
	if e.released {
		return
	}
	e.released = true
	close(e.release)
}

// signal wakes every Await parked on the current notify channel.  Must be called with h.lock held.
func (h *ResponseHold) signal() {
	close(h.notify)
	h.notify = make(chan struct{})
}

/*
Await blocks until this hold is holding a matching response, then returns the oldest one it is still
holding so the spec can assert on it.  It returns immediately if one is already held.  Responses that
passed straight through because the hold was at its [ResponseHold.Limit] were never held, so Await
skips them; after a bare Release() (which ends the hold) Await returns the first response the hold
intercepted rather than blocking.

Release one response and Await again to wait for the NEXT one:

	hold.Await()       // the first GET /home
	hold.ReleaseNext()
	hold.Await()       // blocks until the second GET /home is held

Await is a waiting command: it keeps its own generous default deadline and honors WithTimeout and
WithContext (set them on the tab you build the hold from - b.WithTimeout(d).HoldResponse(url)).
WithPolling and Immediate are a hard error.  On timeout Await fails the spec.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (h *ResponseHold) Await() InterceptedResponse {
	h.b.gt.Helper()
	h.b.guardConfig("Await", knobTimeout, knobContext)
	timeout := h.b.waitingTimeout(holdResponseTimeout)
	ctx, cancel := h.b.waitingContext(holdResponseTimeout)
	defer cancel()

	for {
		h.lock.Lock()
		if entry := h.oldestHeld(); entry != nil {
			response := entry.response
			h.lock.Unlock()
			return response
		}
		if h.released && len(h.held) > 0 {
			// backward compatibility: a terminally-released hold holds nothing, but Await has always
			// handed back the first response it intercepted.
			response := h.held[0].response
			h.lock.Unlock()
			return response
		}
		notify, tally := h.notify, h.tally()
		h.lock.Unlock()

		select {
		case <-notify:
		case <-ctx.Done():
			h.b.gt.Fatalf("Timed out after %s waiting for HoldResponse to intercept a response with URL matching %s\n%s", timeout, h.description(), tally)
			return InterceptedResponse{}
		}
	}
}

// oldestHeld returns the oldest entry that is still frozen, or nil.  Must be called with h.lock held.
func (h *ResponseHold) oldestHeld() *heldEntry {
	for _, e := range h.held {
		if !e.released {
			return e
		}
	}
	return nil
}

/*
Release lets held responses through to the page, unchanged.

Called with no arguments it is terminal: every response currently held goes through AND the hold
stops holding future matches (they pass straight through).  It is idempotent and safe to call when
nothing is held.

Called with responses (as returned by [ResponseHold.Await]) it releases only those, and the hold
stays armed - the next matching response is held just like the first:

	first := hold.Await()
	...
	hold.Release(first)   // only this one lands; the hold is still armed

Releasing a response this hold is not currently holding fails the spec.  Responses are matched by
value, oldest first, since two byte-identical held responses are genuinely indistinguishable.

Release is a fact about the *network*: it returns once the release is signalled, which is not the same
thing as the page having received the response, let alone having rendered it.  When the next assertion
is about what the renderer did, follow Release with an app-state barrier - Eventually on the DOM the
response produces, or on state the app exposes on window - rather than a sleep.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (h *ResponseHold) Release(responses ...InterceptedResponse) {
	h.b.gt.Helper()
	// WithTimeout/WithContext are legal on the tab a hold is built from (they configure Await) so
	// they can't be rejected here; WithPolling/Immediate were already rejected by HoldResponse.
	h.b.guardConfig("Release", knobTimeout, knobContext)
	if len(responses) == 0 {
		h.release_()
		return
	}
	for _, response := range responses {
		h.releaseResponse(response)
	}
}

/*
ReleaseNext lets the oldest response this hold is still holding through to the page, unchanged, and
leaves the hold armed so the next matching response is held too.  Use it to step responses through
one at a time:

	hold.Await()
	hold.ReleaseNext()

It fails the spec when nothing is currently held - a release that finds nothing to release means the
spec's sequencing is off, and that should be loud.  Await first.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (h *ResponseHold) ReleaseNext() {
	h.b.gt.Helper()
	h.b.guardConfig("ReleaseNext", knobTimeout, knobContext)
	h.lock.Lock()
	if entry := h.oldestHeld(); entry != nil {
		h.releaseEntry(entry)
		h.lock.Unlock()
		return
	}
	tally := h.tally()
	h.lock.Unlock()
	// never fail while holding h.lock - gt.Fatalf does not return in a real spec.
	h.b.gt.Fatalf("ReleaseNext() was called but this hold is not holding any responses with URL matching %s\n%s\nAwait() until a response is actually being held before you release it.", h.description(), tally)
}

// releaseResponse frees the oldest still-held entry equal to response.  InterceptedResponse is a
// plain value struct users may assert on, so it carries no identity of its own - value equality,
// oldest first, is the honest resolution: two byte-identical held responses are indistinguishable.
func (h *ResponseHold) releaseResponse(response InterceptedResponse) {
	h.b.gt.Helper()
	h.lock.Lock()
	everHeld := false
	for _, e := range h.held {
		if !reflect.DeepEqual(e.response, response) {
			continue
		}
		everHeld = true
		if !e.released {
			h.releaseEntry(e)
			h.lock.Unlock()
			return
		}
	}
	count, holding := h.count, h.holdingCount()
	h.lock.Unlock()

	// never fail while holding h.lock - gt.Fatalf does not return in a real spec.
	if everHeld {
		h.b.gt.Fatalf("Release() was passed a response that this hold has already released:\n%s\nThis hold has intercepted %d matching response(s) and is currently holding %d.", format.Object(response, 1), count, holding)
		return
	}
	h.b.gt.Fatalf("Release() was passed a response that this hold has never held:\n%s\nThis hold has intercepted %d matching response(s) with URL matching %s and is currently holding %d.\nRelease() only accepts responses returned by Await().", format.Object(response, 1), count, h.description(), holding)
}

func (h *ResponseHold) release_() {
	h.lock.Lock()
	if h.released {
		h.lock.Unlock()
		return
	}
	h.released = true
	for _, e := range h.held {
		h.releaseEntry(e)
	}
	h.signal()
	h.lock.Unlock()
}

/*
Count returns how many matching responses this hold has intercepted so far - every match, whether it
was held or passed straight through (because the hold was at its [ResponseHold.Limit], or already
released).  It is a snapshot - it takes no poll-config knobs - but it is safe to poll, which is how a
spec waits for a passed-through response to have reached the interceptor:

	Eventually(hold.Count).Should(Equal(1))

Count is the total; [ResponseHold.Held] and [ResponseHold.PassedThrough] split it, and are what you
want when the fact under test is which side a particular response landed on.

Count is a fact about the *network*, not about the page: it says the response reached this tab's
interceptor, not that the renderer has done anything with it.  When your assertion is about what the
app did with the response, pair it with an app-state barrier (a DOM change, a store the app exposes on
window) - the same lesson as [ResponseHold.Release].

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (h *ResponseHold) Count() int {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.count
}

/*
Held returns how many matching responses this hold has actually frozen - cumulative, including ones it
has since released.  It is a snapshot but safe to poll, exactly like [ResponseHold.Count].

Held plus [ResponseHold.PassedThrough] always equals Count.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (h *ResponseHold) Held() int {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.count - h.passed
}

/*
PassedThrough returns how many matching responses reached this hold and were never frozen: they
arrived while the hold was at its [ResponseHold.Limit], or after a bare Release() ended it.

This is how a spec states the fixture "response #2 was NOT held" directly, instead of inferring it from
the total plus the limit:

	hold := b.HoldResponse(ContainSubstring("/api/save")).Limit(1)
	b.Click("#save")
	held := hold.Await()                                 // #1 is frozen
	b.Click("#save")
	Eventually(hold.PassedThrough).Should(Equal(1))      // #2 flew past - and a raised Limit would fail here
	hold.Release(held)

Asserting on Count() alone cannot say that: a regression that raises the limit to 2 keeps Count() == 2
green while quietly destroying the ordering under test.

It is a snapshot but safe to poll, exactly like [ResponseHold.Count].

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (h *ResponseHold) PassedThrough() int {
	h.lock.Lock()
	defer h.lock.Unlock()
	return h.passed
}

// tally renders the intercepted/held/passed-through breakdown the Await and ReleaseNext failures
// carry.  Must be called with h.lock held.
func (h *ResponseHold) tally() string {
	out := fmt.Sprintf("%d matching response(s) have been intercepted so far", h.count)
	if h.passed > 0 {
		out += fmt.Sprintf(" (%d held, %d passed straight through)", h.count-h.passed, h.passed)
	}
	return out + "."
}

func (h *ResponseHold) description() string {
	return normalizeWhitespace(h.matcher.FailureMessage(""))
}

// forceRelease is the deadlock backstop: it releases everything - every entry, however it was
// released so far, and every match still to come - and then waits (briefly) for the parked
// goroutines to actually hand their responses back to Chrome.  It runs as a DeferCleanup
// registered by HoldResponse and again from Prepare(), so neither a failing spec nor a panic can
// leave a Fetch pause outstanding and wedge the tab for every spec that follows.
func (h *ResponseHold) forceRelease() {
	h.release_()
	for range 200 {
		h.lock.Lock()
		blocked := h.blocked
		h.lock.Unlock()
		if blocked == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// releaseHeldResponses force-releases every hold among the passed-in response handlers.  Prepare()
// calls it with the handlers it is about to discard, before it disables the Fetch domain.
func releaseHeldResponses(handlers []*ResponseModification) {
	for _, handler := range handlers {
		if handler.hold != nil {
			handler.hold.forceRelease()
		}
	}
}

// ensureFetchEnabled turns on the Fetch domain once, with a single request-stage "*" pattern that
// pauses every request the tab makes.  Response interception is driven per-request, not by a global
// response-stage pattern: when a request's URL has a matching ModifyResponse handler, the
// request-stage continue sets interceptResponse=true so that one request pauses again at the response
// stage (see handleRequestStagePause).  This keeps request- and response-stage handling from
// double-pausing unrelated requests.
//
// It also disables the HTTP cache for as long as interception is on.  A response served out of
// Chrome's cache never traverses the network stack, so it raises no Fetch event at all - the request
// would still show up in Network.requestWillBeSent (and therefore in HaveMadeRequest) while silently
// skipping every stub/abort/modify handler.  Interception has to mean interception, so we trade the
// cache away while it is enabled and restore it in Prepare() when we turn Fetch back off.
func (b *Biloba) ensureFetchEnabled() {
	b.gt.Helper()
	b.lock.Lock()
	needEnable := !b.state.fetchEnabled
	b.state.fetchEnabled = true
	b.lock.Unlock()

	if !needEnable {
		return
	}

	// cache first, then Fetch: that way there is no window in which requests are being intercepted
	// but could still be answered from cache.
	if err := b.runCDP("enable network interception",
		network.SetCacheDisabled(true),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*"}}),
	); err != nil {
		b.gt.Fatalf("Failed to enable network interception:\n%s", err.Error())
	}
}

// requestHandlerFor resolves the request-stage handler for url.  Dispatch stays first-match-wins;
// the loop no longer short-circuits only so it can note which later handlers ALSO matched and were
// therefore shadowed (see handlerProvenance).  That costs a handful of matcher calls per request on
// a tab that has request handlers at all - we bail before evaluating anything when it has none.
func (b *Biloba) requestHandlerFor(url string) *requestHandler {
	b.lock.Lock()
	defer b.lock.Unlock()
	if len(b.state.requestHandlers) == 0 {
		return nil
	}
	var winner *requestHandler
	for _, h := range b.state.requestHandlers {
		if match, _ := h.matcher.Match(url); match {
			if winner == nil {
				winner = h
			} else {
				h.prov.recordShadowed(&winner.prov)
			}
		}
	}
	if winner != nil {
		winner.prov.fired++
	}
	return winner
}

// responseHandlerFor resolves the response-stage handler for url and records the dispatch (see
// requestHandlerFor).  Call it only when actually dispatching - the request-stage probe that decides
// whether to opt into response interception uses hasResponseHandlerFor so it doesn't double-count.
func (b *Biloba) responseHandlerFor(url string) *ResponseModification {
	b.lock.Lock()
	defer b.lock.Unlock()
	if len(b.state.responseHandlers) == 0 {
		return nil
	}
	var winner *ResponseModification
	for _, h := range b.state.responseHandlers {
		if match, _ := h.matcher.Match(url); match {
			if winner == nil {
				winner = h
			} else {
				h.prov.recordShadowed(&winner.prov)
			}
		}
	}
	if winner != nil {
		winner.prov.fired++
	}
	return winner
}

// hasResponseHandlerFor is the non-recording probe: does any response handler claim this URL?  It
// short-circuits on the first match and leaves the provenance bookkeeping to the response-stage
// dispatch, which is the pause that actually resolves a handler.
func (b *Biloba) hasResponseHandlerFor(url string) bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	for _, h := range b.state.responseHandlers {
		if match, _ := h.matcher.Match(url); match {
			return true
		}
	}
	return false
}

// renderShadowedHandlers returns the on-failure note for this tab's silently-dead network handlers:
// handlers that never claimed a single dispatch and lost at least one to an earlier handler.  Both
// conditions matter - a handler whose URL simply was never requested is not evidence of anything and
// must stay silent, or the diagnostic cries wolf.
func (b *Biloba) renderShadowedHandlers() string {
	// Snapshot everything the note needs under the lock (dispatch goroutines are still free to run),
	// then format outside it.
	type note struct {
		api, location, stage string
		shadowed             int
		byAPI, byLocation    string
	}
	notes := []note{}
	b.lock.Lock()
	collect := func(p *handlerProvenance) {
		if p.fired > 0 || p.shadowed == 0 || p.shadower == nil {
			return
		}
		notes = append(notes, note{
			api: p.api, location: p.location.String(), stage: p.stage, shadowed: p.shadowed,
			byAPI: p.shadower.api, byLocation: p.shadower.location.String(),
		})
	}
	for _, h := range b.state.requestHandlers {
		collect(&h.prov)
	}
	for _, h := range b.state.responseHandlers {
		collect(&h.prov)
	}
	b.lock.Unlock()

	out := &strings.Builder{}
	for _, n := range notes {
		fmt.Fprintf(out, "⚠ %s %s handler registered at %s never ran — an earlier %s handler\n  (registered at %s) claimed %d matching %s(s) first.\n",
			indefiniteArticle(n.api), n.api, n.location, n.byAPI, n.byLocation, n.shadowed, n.stage)
	}
	if out.Len() == 0 {
		return ""
	}
	out.WriteString("  Network handlers are first-match-wins in registration order, so a later handler for a URL an\n  earlier one already claims is dead code.  Prepare() is what clears them: inside an Ordered\n  container (where BeforeEach(..., OncePerOrdered) skips Prepare) they accumulate across specs, so a\n  handler registered by an earlier It permanently claims that URL.\n")
	return out.String()
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
		interceptResponse := b.hasResponseHandlerFor(ev.Request.URL)
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
		b.runCDP("answer the intercepted request", action)
	}()
}

func (b *Biloba) handleResponseStagePause(ev *fetch.EventRequestPaused) {
	handler := b.responseHandlerFor(ev.Request.URL)
	go func() {
		if handler == nil {
			// Not ours to modify: hand the real response straight back to the page.
			b.runCDP("continue the intercepted response", fetch.ContinueResponse(ev.RequestID))
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
		b.runCDP("read the intercepted response body", chromedp.ActionFunc(func(ctx context.Context) error {
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
		b.runCDP("fulfil the intercepted response", params)
	}()
}

func (b *Biloba) handleEventRequestWillBeSent(ev *network.EventRequestWillBeSent) {
	b.lock.Lock()
	defer b.lock.Unlock()
	b.state.requests = append(b.state.requests, newRequest(ev.Request, ev.Type))
	b.state.inflightRequests[ev.RequestID] = true
}

func (b *Biloba) handleEventLoadingFinished(ev *network.EventLoadingFinished) {
	b.lock.Lock()
	defer b.lock.Unlock()
	delete(b.state.inflightRequests, ev.RequestID)
}

func (b *Biloba) handleEventLoadingFailed(ev *network.EventLoadingFailed) {
	b.lock.Lock()
	defer b.lock.Unlock()
	delete(b.state.inflightRequests, ev.RequestID)
}

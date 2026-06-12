package biloba

import (
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

type requestStub struct {
	matcher  types.GomegaMatcher
	response StubResponse
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
	b.stubs = append(b.stubs, &requestStub{matcher: matcherOrEqual(url), response: response})
	needEnable := !b.fetchEnabled
	b.fetchEnabled = true
	b.lock.Unlock()

	if needEnable {
		err := chromedp.Run(b.Context, fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*"}}))
		if err != nil {
			b.gt.Fatalf("Failed to enable request stubbing:\n%s", err.Error())
		}
	}
}

func (b *Biloba) stubFor(url string) *requestStub {
	b.lock.Lock()
	defer b.lock.Unlock()
	for _, stub := range b.stubs {
		if match, _ := stub.matcher.Match(url); match {
			return stub
		}
	}
	return nil
}

// handleEventRequestPaused responds to a paused request (one fulfilled or continued; never
// dropped, or the page would hang). Because the listener callback runs on the target's event
// loop, issuing the CDP response synchronously here would deadlock - so we resolve in a goroutine.
func (b *Biloba) handleEventRequestPaused(ev *fetch.EventRequestPaused) {
	stub := b.stubFor(ev.Request.URL)
	go func() {
		var action chromedp.Action
		if stub != nil {
			params := fetch.FulfillRequest(ev.RequestID, int64(stub.response.Status)).
				WithBody(base64.StdEncoding.EncodeToString([]byte(stub.response.Body)))
			if headers := stub.response.headerEntries(); len(headers) > 0 {
				params = params.WithResponseHeaders(headers)
			}
			action = params
		} else {
			action = fetch.ContinueRequest(ev.RequestID)
		}
		chromedp.Run(b.Context, action)
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

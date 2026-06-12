package biloba

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
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
Find returns the first request matching the passed-in RequestFilters (all filters must match), or nil if none match

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (r Requests) Find(filters ...RequestFilter) *Request {
	for _, req := range r {
		if requestMatchesAll(req, filters) {
			return req
		}
	}
	return nil
}

/*
Filter returns a Requests slice containing all requests matching the passed-in RequestFilters (all filters must match)

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (r Requests) Filter(filters ...RequestFilter) Requests {
	out := Requests{}
	for _, req := range r {
		if requestMatchesAll(req, filters) {
			out = append(out, req)
		}
	}
	return out
}

/*
RequestFilter is used to select requests

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
type RequestFilter func(*Request) bool

func requestMatchesAll(req *Request, filters []RequestFilter) bool {
	for _, f := range filters {
		if !f(req) {
			return false
		}
	}
	return true
}

/*
RequestWithURL returns a RequestFilter that selects requests with a matching URL.  url may be a string (exact match) or a Gomega matcher (e.g. ContainSubstring("/api/users")).

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) RequestWithURL(url any) RequestFilter {
	m := matcherOrEqual(url)
	return func(req *Request) bool {
		match, _ := m.Match(req.URL)
		return match
	}
}

/*
RequestWithMethod returns a RequestFilter that selects requests with a matching HTTP method.  method may be a string (exact match) or a Gomega matcher.

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) RequestWithMethod(method any) RequestFilter {
	m := matcherOrEqual(method)
	return func(req *Request) bool {
		match, _ := m.Match(req.Method)
		return match
	}
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
HaveMadeRequest() is a matcher that passes if this tab has observed a request satisfying all the passed-in RequestFilters.  Apply it to the tab itself so you can poll for a request to be made:

	Eventually(b).Should(b.HaveMadeRequest(b.RequestWithURL(ContainSubstring("/api/users"))))
	Eventually(b).Should(b.HaveMadeRequest(b.RequestWithURL(ContainSubstring("/api/users")), b.RequestWithMethod("POST")))

Read https://onsi.github.io/biloba/#stubbing-and-observing-the-network to learn more about working with the network in Biloba
*/
func (b *Biloba) HaveMadeRequest(filters ...RequestFilter) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		return b.AllRequests().Find(filters...) != nil, nil
	}).WithTemplate("Did not find request satisfying requirements.")
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

package biloba

import (
	"fmt"

	"github.com/chromedp/cdproto/network"
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

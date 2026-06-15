package biloba

import (
	"context"
	"net/http"
	"strings"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

/*
Navigate() causes this tab to navigate to the provided URL.  The spec fails if the response does not have status code 200

Read https://onsi.github.io/biloba/#navigation to learn more about navigation
*/
func (b *Biloba) Navigate(url string) *Biloba {
	return b.NavigateWithStatus(url, http.StatusOK)
}

/*
NavigateWithStatus() causes this tab to navigate to the provided URL and asserts that the response has the provided status code.

Read https://onsi.github.io/biloba/#navigation to learn more about navigation
*/
func (b *Biloba) NavigateWithStatus(url string, status int) *Biloba {
	b.gt.Helper()

	// Chrome 149+ fires Network.loadingFailed (ERR_HTTP_RESPONSE_CODE_FAILURE) for 4xx/5xx
	// responses, causing chromedp.Navigate to return an error instead of a success.
	// We capture the actual HTTP status via a network listener and check it ourselves.
	lctx, lcancel := context.WithCancel(b.Context)
	defer lcancel()
	var capturedStatus int64
	chromedp.ListenTarget(lctx, func(ev any) {
		if e, ok := ev.(*network.EventResponseReceived); ok {
			if e.Type == network.ResourceTypeDocument {
				capturedStatus = e.Response.Status
			}
		}
	})

	err := chromedp.Run(b.Context, chromedp.Navigate(url))
	isHTTPError := err != nil && strings.Contains(err.Error(), "ERR_HTTP_RESPONSE_CODE_FAILURE")

	if err != nil && !isHTTPError {
		b.gt.Fatalf("failed to navigate to %s: %s", url, err.Error())
		return b
	}

	if capturedStatus != 0 {
		if int(capturedStatus) != status {
			b.gt.Fatalf("failed to navigate to %s: expected status code %d, got %d", url, status, capturedStatus)
		}
		return b
	}

	// No HTTP response received (e.g. about:blank). If we got an error, report it.
	if err != nil {
		b.gt.Fatalf("failed to navigate to %s: %s", url, err.Error())
	}
	return b
}

/*
Location() returns the location (i.e. url) of the current tab.
*/
func (b *Biloba) Location() string {
	var location string
	err := chromedp.Run(b.Context, chromedp.Location(&location))
	if err != nil {
		b.gt.Fatalf("Failed to fetch location:\n%s", err.Error())
		return ""
	}
	return location
}

/*
HaveURL(expected) is a Gomega matcher that matches against the current tab's [Biloba.Location].  Apply it to the tab itself so you can poll for navigation:

	Eventually(b).Should(b.HaveURL("https://onsi.github.io/biloba/"))
	Eventually(b).Should(b.HaveURL(HaveSuffix("biloba/")))

expected can be a string (exact match) or a Gomega matcher.
*/
func (b *Biloba) HaveURL(expected any) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		data["Result"] = b.Location()
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveURL:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
Title() returns the window title of the current tab.
*/
func (b *Biloba) Title() string {
	var title string
	err := chromedp.Run(b.Context, chromedp.Title(&title))
	if err != nil {
		b.gt.Fatalf("Failed to fetch title:\n%s", err.Error())
		return ""
	}
	return title
}

/*
HaveTitle(expected) is a Gomega matcher that matches against the current tab's [Biloba.Title].  Apply it to the tab itself so you can poll for the title to change:

	Eventually(b).Should(b.HaveTitle("Introduction"))
	Eventually(b).Should(b.HaveTitle(HaveSuffix("Introduction")))

expected can be a string (exact match) or a Gomega matcher.
*/
func (b *Biloba) HaveTitle(expected any) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		data["Result"] = b.Title()
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveTitle:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

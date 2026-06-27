package biloba

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

// navigationTimeout bounds a single navigation so a wedged target can't hang the whole suite.  Real
// Chrome occasionally never acknowledges chromedp.Navigate under parallel/CI load, leaving the call
// blocked on b.Context (which has no deadline) until the entire Ginkgo suite timeout elapses - one
// stuck navigation in a BeforeEach then reads as a multi-minute suite failure.  The bound is generous
// enough that a healthy navigation (even a slow real-network page load) never trips it.  It is a var
// (not a const) only so navigation_test.go can shrink it via SetNavigationTimeoutForTest.
var navigationTimeout = 30 * time.Second

/*
Navigate() causes this tab to navigate to the provided URL.  The spec fails if the response does not have status code 200

Navigate is a waiting command: it keeps its own generous default deadline (~30s), which you can override with [Biloba.WithTimeout] or abort with [Biloba.WithContext].  WithPolling and Immediate are not supported.

Read https://onsi.github.io/biloba/#navigation to learn more about navigation
*/
func (b *Biloba) Navigate(url string) *Biloba {
	b.gt.Helper()
	b.guardConfig("Navigate", knobTimeout, knobContext)
	return b.navigateWithStatus(url, http.StatusOK)
}

/*
NavigateWithStatus() causes this tab to navigate to the provided URL and asserts that the response has the provided status code.

Like [Biloba.Navigate] it is a waiting command: override its default deadline with [Biloba.WithTimeout] or abort it with [Biloba.WithContext]; WithPolling and Immediate are not supported.

Read https://onsi.github.io/biloba/#navigation to learn more about navigation
*/
func (b *Biloba) NavigateWithStatus(url string, status int) *Biloba {
	b.gt.Helper()
	b.guardConfig("NavigateWithStatus", knobTimeout, knobContext)
	return b.navigateWithStatus(url, status)
}

// navigateWithStatus is the unguarded substrate behind Navigate/NavigateWithStatus.  It honors the
// WithTimeout/WithContext knobs the four-bucket model allows a waiting command (via waitingContext)
// while keeping navigationTimeout as its default deadline when WithTimeout is unset.
func (b *Biloba) navigateWithStatus(url string, status int) *Biloba {
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

	timeout := navigationTimeout
	if b.timeout != nil {
		timeout = *b.timeout
	}
	nctx, ncancel := b.waitingContext(navigationTimeout)
	defer ncancel()
	err := chromedp.Run(nctx, chromedp.Navigate(url))
	isHTTPError := err != nil && strings.Contains(err.Error(), "ERR_HTTP_RESPONSE_CODE_FAILURE")

	if errors.Is(err, context.DeadlineExceeded) {
		b.gt.Fatalf("timed out after %s navigating to %s: the navigation never completed (Chrome may have wedged)", timeout, url)
		return b
	}

	if err != nil && !isHTTPError {
		b.gt.Fatalf("failed to navigate to %s: %s", url, err.Error())
		return b
	}

	// In high-fidelity mode the compositor's trusted-input surface is (re)sized from the device
	// metrics in effect at page commit, so re-assert the viewport emulation now that the new page has
	// loaded - otherwise realistic-mode wheel/scroll input is silently dropped near the viewport
	// bottom.  No-op in the default chrome-headless-shell lane.
	b.reassertViewportForCompositor()

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
	b.gt.Helper()
	b.guardConfig("Location")
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
	b.gt.Helper()
	b.guardConfig("Title")
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

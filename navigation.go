package biloba

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/onsi/biloba/engine"
	"github.com/onsi/gomega/gcustom"
)

// navigationTimeout bounds a single navigation so a wedged target can't hang the whole suite.  Real
// Chrome occasionally never acknowledges chromedp.Navigate under parallel/CI load, leaving the call
// blocked on b.Context (which has no deadline) until the entire Ginkgo suite timeout elapses - one
// stuck navigation in a BeforeEach then reads as a multi-minute suite failure.  The bound is generous
// enough that a healthy navigation (even a slow real-network page load) never trips it.  It is a var
// (not a const) only so navigation_test.go can shrink it via SetNavigationTimeoutForTest.
var navigationTimeout = 30 * time.Second

// simulateTransientReadError, when non-nil, is consulted by location()/title() before they run the
// real chromedp.Location/chromedp.Title action. It lets navigation_test.go simulate the transient CDP
// error a live navigation can legitimately throw (e.g. "Inspected target navigated or closed (-32000)"
// while the target is swapped mid-navigation) without needing to reproduce the underlying race. nil
// (the default) means "no injected error, run the real action." Overridden from tests via
// SetTransientReadErrorForTest.
var simulateTransientReadError func() error

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

	timeout := b.waitingTimeout(navigationTimeout)
	nctx, ncancel := b.waitingContext(navigationTimeout)
	defer ncancel()
	var result engine.NavigationResult
	err := b.runEngineIn(nctx, timeout, "navigate to "+url, func(ctx context.Context) error {
		var err error
		result, err = engine.NavigateContext(ctx, url)
		return err
	})

	if errors.Is(err, context.DeadlineExceeded) {
		b.gt.Fatalf("Timed out after %s navigating to %s: the navigation never completed (Chrome may have wedged)", timeout, url)
		return b
	}

	// Chrome 149+ fires Network.loadingFailed (ERR_HTTP_RESPONSE_CODE_FAILURE) for 4xx/5xx
	// responses, so chromedp.Navigate returns an error where a non-200 status is exactly what the
	// spec asked for.  That error is not, on its own, a navigation failure - the status we observed
	// is what decides (see below).  Any other error is a genuine failure and stops us here.  The two
	// are independent: an HTTP loading failure whose document status we never observed still has to
	// surface, rather than being mistaken for a destination that simply has no HTTP response.
	if err != nil && !result.HTTPFailure {
		b.gt.Fatalf("failed to navigate to %s: %s", url, err.Error())
		return b
	}

	// In high-fidelity mode the compositor's trusted-input surface is (re)sized from the device
	// metrics in effect at page commit, so re-assert the viewport emulation now that the new page has
	// loaded - otherwise realistic-mode wheel/scroll input is silently dropped near the viewport
	// bottom.  No-op in the default chrome-headless-shell lane.
	b.reassertViewportForCompositor()

	if result.Status != 0 {
		if result.Status != status {
			b.gt.Fatalf("failed to navigate to %s: expected status code %d, got %d", url, status, result.Status)
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
GetLocation() returns the location (i.e. url) of the current tab.

GetLocation polls by default: it repeatedly asks Chrome for the tab's location, honoring
WithTimeout/WithPolling/WithContext, or Immediate() to act once and fail fast.  A transient CDP error
(e.g. "Inspected target navigated or closed" while a live navigation swaps the target out from under
the read) is treated as a retryable miss, not a fatal - it keeps polling and only surfaces the
underlying error if it never clears by the deadline.

Read https://onsi.github.io/biloba/#navigation to learn more about navigation
*/
func (b *Biloba) GetLocation() string {
	b.gt.Helper()
	var result string
	matcher := gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		location, err := b.location()
		if err != nil {
			return false, err
		}
		result = location
		return true, nil
	}).WithMessage("fetch the tab's location")
	b.pollOrImmediate(b, matcher)
	return result
}

// location is the unguarded, single-shot substrate behind GetLocation. Internal callers (TabQuery
// matching/rendering in tabs.go, SetCookie in cookies.go, GomegaString in biloba.go) call this
// directly instead of GetLocation so a transient CDP error never polls or fails the spec from a
// snapshot/rendering context - they take the zero value "" on error instead.
func (b *Biloba) location() (string, error) {
	if simulateTransientReadError != nil {
		if err := simulateTransientReadError(); err != nil {
			return "", err
		}
	}
	var location string
	err := b.runEngine("read the tab's location", func(ctx context.Context) error {
		var err error
		location, err = engine.LocationContext(ctx)
		return err
	})
	return location, err
}

/*
HaveURL(expected) is a Gomega matcher that matches against the current tab's [Biloba.GetLocation].  Apply it to the tab itself so you can poll for navigation:

	Eventually(b).Should(b.HaveURL("https://onsi.github.io/biloba/"))
	Eventually(b).Should(b.HaveURL(HaveSuffix("biloba/")))

expected can be a string (exact match) or a Gomega matcher.  Like GetLocation, a transient CDP error while reading the location is a retryable miss under Eventually rather than an immediate failure.

HaveURL returns a [ValueMatcher], so you can [ValueMatcher.Capture] the url that satisfied the assertion:

	var url string
	Eventually(b).Should(b.HaveURL(HaveSuffix("/done")).Capture(&url))

Read https://onsi.github.io/biloba/#navigation to learn more about navigation
*/
func (b *Biloba) HaveURL(expected any) *ValueMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return capturableResult(gcustom.MakeMatcher(func(tab *Biloba) (bool, error) {
		location, err := tab.location()
		if err != nil {
			return false, err
		}
		data["Result"] = location
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveURL:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data), data)
}

/*
GetTitle() returns the window title of the current tab.

GetTitle polls by default: it repeatedly asks Chrome for the tab's title, honoring
WithTimeout/WithPolling/WithContext, or Immediate() to act once and fail fast.  A transient CDP error
(e.g. "Inspected target navigated or closed" while a live navigation swaps the target out from under
the read) is treated as a retryable miss, not a fatal - it keeps polling and only surfaces the
underlying error if it never clears by the deadline.

Read https://onsi.github.io/biloba/#navigation to learn more about navigation
*/
func (b *Biloba) GetTitle() string {
	b.gt.Helper()
	var result string
	matcher := gcustom.MakeMatcher(func(_ *Biloba) (bool, error) {
		title, err := b.title()
		if err != nil {
			return false, err
		}
		result = title
		return true, nil
	}).WithMessage("fetch the tab's title")
	b.pollOrImmediate(b, matcher)
	return result
}

// title is the unguarded, single-shot substrate behind GetTitle. Internal callers (TabQuery
// matching/rendering in tabs.go, GomegaString in biloba.go) call this directly instead of GetTitle so
// a transient CDP error never polls or fails the spec from a snapshot/rendering context - they take
// the zero value "" on error instead.
func (b *Biloba) title() (string, error) {
	if simulateTransientReadError != nil {
		if err := simulateTransientReadError(); err != nil {
			return "", err
		}
	}
	var title string
	err := b.runEngine("read the tab's title", func(ctx context.Context) error {
		var err error
		title, err = engine.TitleContext(ctx)
		return err
	})
	return title, err
}

/*
HaveTitle(expected) is a Gomega matcher that matches against the current tab's [Biloba.GetTitle].  Apply it to the tab itself so you can poll for the title to change:

	Eventually(b).Should(b.HaveTitle("Introduction"))
	Eventually(b).Should(b.HaveTitle(HaveSuffix("Introduction")))

expected can be a string (exact match) or a Gomega matcher.  Like GetTitle, a transient CDP error while reading the title is a retryable miss under Eventually rather than an immediate failure.

HaveTitle returns a [ValueMatcher], so you can [ValueMatcher.Capture] the title that satisfied the assertion:

	var title string
	Eventually(b).Should(b.HaveTitle(HavePrefix("Nav-")).Capture(&title))

Read https://onsi.github.io/biloba/#navigation to learn more about navigation
*/
func (b *Biloba) HaveTitle(expected any) *ValueMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return capturableResult(gcustom.MakeMatcher(func(tab *Biloba) (bool, error) {
		title, err := tab.title()
		if err != nil {
			return false, err
		}
		data["Result"] = title
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveTitle:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data), data)
}

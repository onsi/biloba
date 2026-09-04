package engine

import (
	"context"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// NavigationResult is the runner-neutral observation produced by one completed
// navigation. Status is zero for destinations without an HTTP response.
type NavigationResult struct {
	Status int
	// HTTPFailure reports that the error returned alongside this result is Chrome's
	// loading failure for a 4xx/5xx document (ERR_HTTP_RESPONSE_CODE_FAILURE) rather
	// than a transport/target failure.  It is deliberately independent of Status: the
	// two are separate observations and callers decide what to do with each, because a
	// document whose Network.responseReceived event we never saw (listener registration
	// race, target swapped mid-navigation) still has to be reported as an HTTP failure
	// rather than silently becoming "no response at all".
	HTTPFailure bool
}

// NavigateContext performs one navigation and captures the main-document HTTP
// status.  Chrome 149+ reports 4xx/5xx responses as loading failures, so the raw
// CDP error is returned as-is together with HTTPFailure telling the caller which
// kind of error it is - suppressing it here would fold two independent decisions
// (was this an HTTP error? did we observe a status?) into one.
func NavigateContext(ctx context.Context, destination string) (NavigationResult, error) {
	listenCtx, cancelListen := context.WithCancel(ctx)
	defer cancelListen()
	var statusMu sync.Mutex
	var status int
	chromedp.ListenTarget(listenCtx, func(event any) {
		response, ok := event.(*network.EventResponseReceived)
		if !ok || response.Type != network.ResourceTypeDocument {
			return
		}
		statusMu.Lock()
		status = int(response.Response.Status)
		statusMu.Unlock()
	})
	err := chromedp.Run(ctx, chromedp.Navigate(destination))
	statusMu.Lock()
	result := NavigationResult{Status: status}
	statusMu.Unlock()
	result.HTTPFailure = httpStatusFailure(err)
	return result, err
}

// httpStatusFailure classifies a navigation error as Chrome's loading failure for a 4xx/5xx
// document.  It looks at the error and nothing else - deliberately, since whether we also observed
// the document's Network.responseReceived event is a separate question with its own answer.
func httpStatusFailure(err error) bool {
	return err != nil && strings.Contains(err.Error(), "ERR_HTTP_RESPONSE_CODE_FAILURE")
}

func LocationContext(ctx context.Context) (string, error) {
	var location string
	err := chromedp.Run(ctx, chromedp.Location(&location))
	return location, err
}

func TitleContext(ctx context.Context) (string, error) {
	var title string
	err := chromedp.Run(ctx, chromedp.Title(&title))
	return title, err
}

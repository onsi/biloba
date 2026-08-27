package engine

import (
	"context"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// EnableInterceptionContext turns on request interception for every URL.
//
// The order is load-bearing: the cache is disabled first, then Fetch is enabled.  The other way
// round leaves a window in which requests are being intercepted but could still be answered from
// cache, which reads as an interceptor that silently did not fire.
func EnableInterceptionContext(ctx context.Context) error {
	return chromedp.Run(ctx,
		network.SetCacheDisabled(true),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*"}}),
	)
}

// RunActionContext dispatches one CDP action against the target.  Interception handlers resolve
// which action to take (continue, fulfil, fail) from their own policy and then need it sent; the
// choice is the caller's, the dispatch is the engine's.
func RunActionContext(ctx context.Context, action chromedp.Action) error {
	return chromedp.Run(ctx, action)
}

// ResponseBodyContext reads a paused response's body.  Only valid at the response stage, and it has
// to go through chromedp.Run so it picks up the target's CDP executor from the context; chromedp
// decodes the base64 for us.
func ResponseBodyContext(ctx context.Context, requestID fetch.RequestID) ([]byte, error) {
	var body []byte
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		var readErr error
		body, readErr = fetch.GetResponseBody(requestID).Do(runCtx)
		return readErr
	}))
	return body, err
}

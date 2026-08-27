package engine

import (
	"context"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// The browser- and target-level commands that set a tab up, tear it down, or ask Chrome about
// itself.  They are separated from the per-operation primitives because most of them run once, at
// connect or reset time, and because several are browser-scoped: they take a BrowserContextID and
// must be dispatched on the Browser connection rather than the target's.

// ChromeProductContext reports Chrome's product string, e.g. "HeadlessChrome/152.0.7977.64".
func ChromeProductContext(ctx context.Context) (string, error) {
	var product string
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		_, p, _, _, _, versionErr := browser.GetVersion().Do(runCtx)
		product = p
		return versionErr
	}))
	return product, err
}

// ViewportDimensionsContext reads the tab's current inner width and height in CSS pixels.
func ViewportDimensionsContext(ctx context.Context) (width, height int64, err error) {
	var dims []int64
	if err = chromedp.Run(ctx, chromedp.Evaluate("[window.innerWidth, window.innerHeight]", &dims)); err != nil {
		return 0, 0, err
	}
	if len(dims) != 2 {
		return 0, 0, nil
	}
	return dims[0], dims[1], nil
}

// SetFocusEmulationContext makes the tab behave as though its page always holds the system focus.
// An automated headless window never actually has OS focus, and full ("new") headless Chrome gates
// focus/blur *event* dispatch on it - element.focus() still moves activeElement, but the events
// never fire, so onBlur handlers silently never run.  Per-target, so every tab must opt in.
func SetFocusEmulationContext(ctx context.Context) error {
	return chromedp.Run(ctx, emulation.SetFocusEmulationEnabled(true))
}

// EnableCrashReportingContext enables the target-level event Chrome uses to report a dead renderer.
func EnableCrashReportingContext(ctx context.Context) error {
	return chromedp.Run(ctx, inspector.Enable())
}

// InstallScriptOnNewDocumentContext asks Chrome to evaluate script at the start of every new document.
func InstallScriptOnNewDocumentContext(ctx context.Context, script string) error {
	return chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(script).Do(runCtx)
		return err
	}))
}

// DisableInterceptionContext tears request interception down and restores the HTTP cache.
// Symmetric with EnableInterceptionContext, and just as order-dependent in reverse: leaving the
// cache disabled would silently change how later work is served.
func DisableInterceptionContext(ctx context.Context) error {
	return chromedp.Run(ctx, fetch.Disable(), network.SetCacheDisabled(false))
}

// ConfigureDownloadsContext points a browser context's downloads at a directory and asks Chrome to
// report download events.  Browser-scoped: it takes a BrowserContextID.
func ConfigureDownloadsContext(ctx context.Context, browserContextID cdp.BrowserContextID, downloadDir string) error {
	return chromedp.Run(ctx, browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).
		WithDownloadPath(downloadDir).
		WithEventsEnabled(true).
		WithBrowserContextID(browserContextID))
}

// NewIsolatedTargetContext creates an isolated browser context and a target within it, returning
// both ids.  Chrome 149+ requires WithNewWindow(true) for a target in a non-default browser
// context, and DisposeOnDetach ties the context's lifetime to the connection that made it.
func NewIsolatedTargetContext(ctx context.Context) (cdp.BrowserContextID, target.ID, error) {
	var browserContextID cdp.BrowserContextID
	var targetID target.ID
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		chrome := chromedp.FromContext(runCtx)
		browserExecutor := cdp.WithExecutor(runCtx, chrome.Browser)
		var createErr error
		browserContextID, createErr = target.CreateBrowserContext().WithDisposeOnDetach(true).Do(browserExecutor)
		if createErr != nil {
			return createErr
		}
		targetID, createErr = target.CreateTarget("about:blank").
			WithBrowserContextID(browserContextID).
			WithNewWindow(true).
			Do(browserExecutor)
		return createErr
	}))
	return browserContextID, targetID, err
}

// TargetBrowserContextContext reports which browser context a target belongs to - the way a tab
// Chrome opened for us (a spawned tab) reveals whose isolation it inherited.
func TargetBrowserContextContext(ctx context.Context) (cdp.BrowserContextID, error) {
	var browserContextID cdp.BrowserContextID
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(runCtx context.Context) error {
		info, infoErr := target.GetTargetInfo().Do(runCtx)
		if infoErr != nil {
			return infoErr
		}
		browserContextID = info.BrowserContextID
		return nil
	}))
	return browserContextID, err
}

// PingContext is the cheapest round trip that proves a target is attached and answering.
func PingContext(ctx context.Context) error {
	return chromedp.Run(ctx, chromedp.Evaluate("1", nil))
}

// OuterWindowSizeContext reads the browser window's outer dimensions.  Zero width and height with a
// nil error means the page answered with something unusable, which callers treat as "leave it alone"
// rather than as a failure.
func OuterWindowSizeContext(ctx context.Context) (width, height int, err error) {
	var dims []int
	if err = chromedp.Run(ctx, chromedp.Evaluate("[window.outerWidth, window.outerHeight]", &dims)); err != nil {
		return 0, 0, err
	}
	if len(dims) != 2 {
		return 0, 0, nil
	}
	return dims[0], dims[1], nil
}

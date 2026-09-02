package biloba

import (
	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func (b *Biloba) configureDownloadBehaviorForAllTabsWithBrowserContextID(browserContextId cdp.BrowserContextID) {
	for _, tab := range b.AllTabs() {
		if tab.browserContextID == browserContextId {
			tab.configureDownloadBehavior()
		}
	}
}

func (b *Biloba) configureDownloadBehavior() {
	b.runCDP("configure the download behavior",
		browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).
			WithDownloadPath(b.root.downloadDir).
			WithEventsEnabled(true).
			WithBrowserContextID(b.browserContextID))
}

// enableCrashReporting turns on the Inspector domain, which is what delivers
// Inspector.targetCrashed.  chromedp enables Page/Runtime/Network/DOM on its own but not this one, so
// without it the crash event arrives only by luck: it did on macOS and never did on Linux CI, which
// meant diagnoseCDPError silently degraded to deadline_exceeded on the platform most suites run on.
func (b *Biloba) enableCrashReporting() {
	b.runCDP("enable crash reporting", inspector.Enable())
}

func (b *Biloba) setUpListeners() {
	b.configureDownloadBehavior()
	b.enableCrashReporting()

	chromedp.ListenTarget(b.Context, func(ev any) {
		switch ev := ev.(type) {
		case *page.EventJavascriptDialogOpening:
			b.handleEventJavascriptDialogOpening(ev)
		case *runtime.EventConsoleAPICalled:
			b.handleEventConsoleAPICalled(ev)
		case *page.EventFrameNavigated:
			b.handleEventFrameNavigated(ev)
		case *browser.EventDownloadWillBegin:
			b.handleEventDownloadWillBegin(ev)
		case *browser.EventDownloadProgress:
			b.handleEventDownloadProgress(ev)
		case *network.EventRequestWillBeSent:
			b.handleEventRequestWillBeSent(ev)
		case *network.EventLoadingFinished:
			b.handleEventLoadingFinished(ev)
		case *network.EventLoadingFailed:
			b.handleEventLoadingFailed(ev)
		case *fetch.EventRequestPaused:
			b.handleEventRequestPaused(ev)
		case *inspector.EventTargetCrashed:
			b.handleEventTargetCrashed(ev)
		}
	})
}

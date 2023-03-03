package biloba

import (
	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
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
	chromedp.Run(b.Context, browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).
		WithDownloadPath(b.root.downloadDir).
		WithEventsEnabled(true).
		WithBrowserContextID(b.browserContextID))
}

func (b *Biloba) setUpListeners() {
	b.configureDownloadBehavior()

	chromedp.ListenTarget(b.Context, func(ev interface{}) {
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
		}
	})
}

package biloba

import (
	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
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

// enableCrashReporting turns on the Inspector domain, which is one of the two ways Chrome announces a
// dead renderer.  chromedp enables Page/Runtime/Network/DOM on its own but not this one.
func (b *Biloba) enableCrashReporting() {
	b.runCDP("enable crash reporting", inspector.Enable())
}

// listenForCrashOnTheBrowser watches the browser-level target.targetCrashed in addition to the
// page-level inspector.targetCrashed that setUpListeners already handles.  Chrome reports a dead
// renderer on both, but not dependably on the same one: the page-session event arrives on macOS and
// does not on Linux, where the renderer dies with its session and takes the delivery path down with
// it.  The browser connection is still up either way, so the browser-level event survives the crash
// that silences the other one.  Whichever arrives first wins; handleEventTargetCrashed is idempotent.
//
// The event carries a TargetID because it is not scoped to a session, so it has to be filtered - every
// tab sees every tab's crash otherwise.
func (b *Biloba) listenForCrashOnTheBrowser() {
	chromedp.ListenBrowser(b.Context, func(ev any) {
		if crashed, ok := ev.(*target.EventTargetCrashed); ok && crashed.TargetID == b.targetID {
			b.handleEventTargetCrashed(nil)
		}
	})
}

func (b *Biloba) setUpListeners() {
	b.configureDownloadBehavior()
	b.enableCrashReporting()
	b.listenForCrashOnTheBrowser()

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

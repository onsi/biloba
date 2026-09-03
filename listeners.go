package biloba

import (
	"context"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"github.com/onsi/biloba/engine"
)

func (b *Biloba) configureDownloadBehaviorForAllTabsWithBrowserContextID(browserContextId cdp.BrowserContextID) {
	for _, tab := range b.AllTabs() {
		if tab.browserContextID == browserContextId {
			tab.configureDownloadBehavior()
		}
	}
}

func (b *Biloba) configureDownloadBehavior() {
	_ = b.runEngine("configure the download behavior", func(ctx context.Context) error {
		return engine.ConfigureDownloadsContext(ctx, b.browserContextID, b.root.downloadDir)
	})
}

// enableCrashReporting turns on the Inspector domain, which is one of the two ways Chrome announces a
// dead renderer.  chromedp enables Page/Runtime/Network/DOM on its own but not this one.
func (b *Biloba) enableCrashReporting() {
	_ = b.runEngine("enable crash reporting", func(ctx context.Context) error {
		return engine.EnableCrashReportingContext(ctx)
	})
}

// installBilobaOnEveryDocument hands biloba.js to Chrome once, to run at the start of every document
// this target creates from here on.  It is what makes "window._biloba exists on every page load" a
// property Chrome enforces rather than one Biloba keeps repairing: the script is in place before any
// of the page's own scripts run, so there is no window in which a command can arrive and find it
// missing.  Without it that window is real - a page that navigates itself between ensureBiloba's flag
// check and the evaluate leaves the command looking at a document that never had _biloba - and the
// only recovery is the "_biloba is not defined" retry in javascript.go, which repairs the symptom one
// command at a time.
//
// It covers every FRAME of the target, not just the main one, which is more than Biloba needs:
// selectors reach into a same-origin iframe from the main frame through contentDocument (see
// pierceRoot), so a child frame's own copy goes unused.  That is a deliberate trade - a few hundred
// microseconds of execution per frame for an invariant Chrome maintains - and it is the only shape
// CDP offers.
//
// The registration applies to documents created AFTER it, so the document already open when a tab is
// set up still needs ensureBiloba's eager install; that is the one job it keeps.
// Registration failing is not fatal: ensureBiloba's per-navigation reinstall is still there and still
// correct, so the tab records whether Chrome took the script and keeps the old behaviour when it did
// not.  Nothing about this may become load-bearing without a fallback - a silent failure would
// otherwise turn every command after a navigation into a "_biloba is not defined" round trip.
func (b *Biloba) installBilobaOnEveryDocument() {
	err := b.runEngine("install biloba.js on every new document", func(ctx context.Context) error {
		return engine.InstallScriptOnNewDocumentContext(ctx, bilobaJS)
	})
	b.lock.Lock()
	defer b.lock.Unlock()
	b.state.bilobaAutoInstalled = err == nil
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
	b.installBilobaOnEveryDocument()
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

package biloba

import (
	"context"
	"sync"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/inspector"
	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/onsi/biloba/engine"
)

/*
orderedCallbacks runs a tab's event work that calls the spec's own code - the matchers of request,
response, and dialog handlers - off the connection's reader goroutine and in the order the events
arrived.  The reader is shared by every tab on the connection and must never wait, and a matcher may
call back into Biloba (another tab's Run, say), whose reply only that reader can deliver.  push never
blocks; one goroutine drains the queue while it has work.
*/
type orderedCallbacks struct {
	mu      sync.Mutex
	queue   []func()
	running bool
}

func (o *orderedCallbacks) push(work func()) {
	o.mu.Lock()
	o.queue = append(o.queue, work)
	if o.running {
		o.mu.Unlock()
		return
	}
	o.running = true
	o.mu.Unlock()
	go o.drain()
}

func (o *orderedCallbacks) drain() {
	for {
		o.mu.Lock()
		if len(o.queue) == 0 {
			o.running = false
			o.mu.Unlock()
			return
		}
		work := o.queue[0]
		o.queue = o.queue[1:]
		o.mu.Unlock()
		work()
	}
}

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

// registerBilobaOnNewDocument is how installBilobaOnEveryDocument hands Chrome the script.
var registerBilobaOnNewDocument = engine.InstallScriptOnNewDocumentContext

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
	// (registerBilobaOnNewDocument is a variable so a spec can make the registration fail.)
	err := b.runEngine("install biloba.js on every new document", func(ctx context.Context) error {
		return registerBilobaOnNewDocument(ctx, bilobaJS)
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
func (b *Biloba) listenForCrashOnTheBrowser() error {
	return engine.ListenBrowser(b.Context, func(ev any) {
		if crashed, ok := ev.(*target.EventTargetCrashed); ok && crashed.TargetID == b.targetID {
			b.handleEventTargetCrashed(nil)
		}
	}, target.TargetCrashed.Method)
}

// tabEvents are the events setUpListeners handles, and the ones b.boundary has to see.
var tabEvents = []string{
	runtime.ExecutionContextCreated.Method, runtime.ExecutionContextDestroyed.Method, runtime.ExecutionContextsCleared.Method,
	page.JavascriptDialogOpening.Method,
	runtime.ConsoleAPICalled.Method, runtime.ExceptionThrown.Method, cdplog.EntryAdded.Method,
	page.FrameAttached.Method, page.FrameNavigated.Method, page.FrameDetached.Method,
	browser.DownloadWillBegin.Method, browser.DownloadProgress.Method,
	network.RequestWillBeSent.Method, network.ResponseReceived.Method, network.LoadingFinished.Method, network.LoadingFailed.Method,
	fetch.RequestPaused.Method,
	inspector.TargetCrashed.Method,
}

// setUpListeners wires the tab's event handlers.  They run on the connection's reader goroutine, in
// the order Chrome sent the events (see engine.ListenTarget), so none of them may wait on Chrome:
// the handlers that answer it (paused requests, dialogs) do so from a goroutine.
func (b *Biloba) setUpListeners() {
	b.configureDownloadBehavior()
	b.enableCrashReporting()
	b.installBilobaOnEveryDocument()
	if err := b.listenForCrashOnTheBrowser(); err != nil {
		b.gt.Fatalf("failed to listen for browser events: %s", err)
		return
	}

	// the previous spec's page can still raise events after Prepare: drop them, unprinted and unrecorded
	err := engine.ListenTarget(b.Context, func(ev any) {
		if b.boundary.Stale(ev) {
			return
		}
		switch ev := ev.(type) {
		case *page.EventJavascriptDialogOpening:
			b.handleEventJavascriptDialogOpening(ev)
		case *runtime.EventConsoleAPICalled:
			b.handleEventConsoleAPICalled(ev)
		case *runtime.EventExceptionThrown:
			b.handleEventExceptionThrown(ev)
		case *cdplog.EventEntryAdded:
			b.handleEventLogEntryAdded(ev)
		case *page.EventFrameAttached:
			b.handleEventFrameAttached(ev)
		case *page.EventFrameNavigated:
			b.handleEventFrameNavigated(ev)
		case *page.EventFrameDetached:
			b.handleEventFrameDetached(ev)
		case *browser.EventDownloadWillBegin:
			b.handleEventDownloadWillBegin(ev)
		case *browser.EventDownloadProgress:
			b.handleEventDownloadProgress(ev)
		case *network.EventRequestWillBeSent:
			b.handleEventRequestWillBeSent(ev)
		case *network.EventResponseReceived:
			b.handleEventResponseReceived(ev)
		case *network.EventLoadingFinished:
			b.handleEventLoadingFinished(ev)
		case *network.EventLoadingFailed:
			b.handleEventLoadingFailed(ev)
		case *fetch.EventRequestPaused:
			b.handleEventRequestPaused(ev)
		case *inspector.EventTargetCrashed:
			b.handleEventTargetCrashed(ev)
		}
	}, tabEvents...)
	if err != nil {
		b.gt.Fatalf("failed to listen for tab events: %s", err)
	}
}

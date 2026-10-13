package engine

import (
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// The Chrome-driven spec in observations_test.go exercises a real Prepare, but whether the old
// document's events reach the listener before or after Prepare returns is up to the scheduler.  These
// pin the ordering rules documentBoundary relies on, one event at a time.
var _ = ginkgo.Describe("documentBoundary", ginkgo.Label("no-browser"), func() {
	created := func(id runtime.ExecutionContextID) *runtime.EventExecutionContextCreated {
		return &runtime.EventExecutionContextCreated{Context: &runtime.ExecutionContextDescription{ID: id}}
	}
	request := func(loader, id string) *network.EventRequestWillBeSent {
		return &network.EventRequestWillBeSent{LoaderID: cdp.LoaderID(loader), RequestID: network.RequestID(id)}
	}

	ginkgo.It("treats a context that existed at the reset as stale until the listener sees it destroyed", func() {
		boundary := &documentBoundary{}
		boundary.observe(created(1))
		boundary.reset()
		boundary.observe(created(2))
		gomega.Expect(boundary.staleContext(1)).To(gomega.BeTrue())
		gomega.Expect(boundary.staleContext(2)).To(gomega.BeFalse())
		gomega.Expect(boundary.staleLogEntry(&cdplog.Entry{})).To(gomega.BeTrue(), "a log entry while the old document is alive is the old document's")

		boundary.observe(&runtime.EventExecutionContextDestroyed{ExecutionContextID: 1})
		gomega.Expect(boundary.staleContext(1)).To(gomega.BeFalse(), "Chrome may reuse the ID once the context is gone")
		gomega.Expect(boundary.staleLogEntry(&cdplog.Entry{})).To(gomega.BeFalse())

		boundary.observe(created(3))
		boundary.reset()
		boundary.observe(&runtime.EventExecutionContextsCleared{})
		gomega.Expect(boundary.staleContext(2)).To(gomega.BeFalse())
		gomega.Expect(boundary.staleContext(3)).To(gomega.BeFalse())
	})

	ginkgo.It("treats the previous spec's loaders and requests as stale until the next reset", func() {
		boundary := &documentBoundary{}
		boundary.observe(request("old-document", "1"))
		boundary.reset()
		boundary.observe(request("old-document", "2"))
		boundary.observe(request("new-document", "3"))
		gomega.Expect(boundary.staleRequest("old-document", "2")).To(gomega.BeTrue())
		gomega.Expect(boundary.staleRequest("", "1")).To(gomega.BeTrue(), "a redirect hop of an old request")
		gomega.Expect(boundary.staleRequest("new-document", "3")).To(gomega.BeFalse())
		gomega.Expect(boundary.staleLogEntry(&cdplog.Entry{NetworkRequestID: "1"})).To(gomega.BeTrue())
		gomega.Expect(boundary.staleLogEntry(&cdplog.Entry{NetworkRequestID: "3"})).To(gomega.BeFalse())

		boundary.reset()
		boundary.reset()
		gomega.Expect(boundary.staleRequest("new-document", "3")).To(gomega.BeFalse(), "two resets later nothing from that spec is still talking")
	})
	ginkgo.It("treats a request the old document starts after the reset as stale, through its log entry", func() {
		boundary := &documentBoundary{}
		boundary.observe(request("old-document", "1"))
		boundary.reset()
		boundary.observe(request("old-document", "2"))
		gomega.Expect(boundary.staleRequestID("2")).To(gomega.BeTrue())
		gomega.Expect(boundary.staleLogEntry(&cdplog.Entry{NetworkRequestID: "2"})).To(gomega.BeTrue())
	})

	ginkgo.It("lets no request without a loader make the next spec's loaderless requests stale", func() {
		boundary := &documentBoundary{}
		boundary.observe(request("", "worker-1"))
		boundary.reset()
		boundary.observe(request("", "worker-2"))
		gomega.Expect(boundary.staleRequest("", "worker-2")).To(gomega.BeFalse())
		gomega.Expect(boundary.staleRequestID("worker-1")).To(gomega.BeTrue())
	})

	ginkgo.Describe("DocumentBoundary.Stale, the Go client's entry point", func() {
		ginkgo.It("classifies each event it attributes, and observes as it goes", func() {
			boundary := &DocumentBoundary{}
			gomega.Expect(boundary.Stale(created(1))).To(gomega.BeFalse())
			gomega.Expect(boundary.Stale(request("old-document", "1"))).To(gomega.BeFalse())
			boundary.Reset()
			gomega.Expect(boundary.Stale(created(2))).To(gomega.BeFalse())

			gomega.Expect(boundary.Stale(&runtime.EventConsoleAPICalled{ExecutionContextID: 1})).To(gomega.BeTrue())
			gomega.Expect(boundary.Stale(&runtime.EventExceptionThrown{ExceptionDetails: &runtime.ExceptionDetails{ExecutionContextID: 1}})).To(gomega.BeTrue())
			gomega.Expect(boundary.Stale(&runtime.EventConsoleAPICalled{ExecutionContextID: 2})).To(gomega.BeFalse())
			gomega.Expect(boundary.Stale(&runtime.EventExceptionThrown{ExceptionDetails: &runtime.ExceptionDetails{ExecutionContextID: 2}})).To(gomega.BeFalse())

			gomega.Expect(boundary.Stale(request("old-document", "2"))).To(gomega.BeTrue())
			gomega.Expect(boundary.Stale(&network.EventResponseReceived{LoaderID: "old-document", RequestID: "1"})).To(gomega.BeTrue())
			gomega.Expect(boundary.Stale(&network.EventLoadingFinished{RequestID: "1"})).To(gomega.BeTrue())
			gomega.Expect(boundary.Stale(&network.EventLoadingFailed{RequestID: "2"})).To(gomega.BeTrue())
			gomega.Expect(boundary.Stale(request("new-document", "3"))).To(gomega.BeFalse())
			gomega.Expect(boundary.Stale(&network.EventLoadingFinished{RequestID: "3"})).To(gomega.BeFalse())

			gomega.Expect(boundary.Stale(&page.EventJavascriptDialogOpening{})).To(gomega.BeFalse(), "dialogs are not attributed")
			gomega.Expect(boundary.Stale(&fetch.EventRequestPaused{RequestID: "1"})).To(gomega.BeFalse(), "a paused request must still be answered")
		})
	})
})

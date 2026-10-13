package engine_test

import (
	"github.com/chromedp/cdproto/cdp"
	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("EventOrigins", Label("no-browser"), func() {
	var generation *engine.EventGeneration
	var origins *engine.EventOrigins
	BeforeEach(func() {
		generation = &engine.EventGeneration{}
		origins = engine.NewEventOrigins(generation)
	})

	created := func(id runtime.ExecutionContextID) *runtime.EventExecutionContextCreated {
		return &runtime.EventExecutionContextCreated{Context: &runtime.ExecutionContextDescription{ID: id}}
	}
	console := func(id runtime.ExecutionContextID) *runtime.EventConsoleAPICalled {
		return &runtime.EventConsoleAPICalled{ExecutionContextID: id}
	}
	thrown := func(id runtime.ExecutionContextID) *runtime.EventExceptionThrown {
		return &runtime.EventExceptionThrown{ExceptionDetails: &runtime.ExceptionDetails{ExecutionContextID: id}}
	}
	request := func(id network.RequestID, loader string) *network.EventRequestWillBeSent {
		return &network.EventRequestWillBeSent{RequestID: id, LoaderID: cdp.LoaderID(loader)}
	}

	It("drops console calls and exceptions from contexts created before the last Advance", func() {
		Ω(origins.Stale(created(1))).Should(BeFalse())
		Ω(origins.Stale(console(1))).Should(BeFalse())
		generation.Advance()
		Ω(origins.Stale(created(2))).Should(BeFalse())
		Ω(origins.Stale(console(1))).Should(BeTrue())
		Ω(origins.Stale(thrown(1))).Should(BeTrue())
		Ω(origins.Stale(console(2))).Should(BeFalse())
		Ω(origins.Stale(thrown(2))).Should(BeFalse())
		Ω(origins.Stale(console(99))).Should(BeFalse(), "a context it never saw created counts as current")
	})

	It("drops requests from an old document's loader, and everything that follows them", func() {
		Ω(origins.Stale(request("a", "old-loader"))).Should(BeFalse())
		generation.Advance()
		Ω(origins.Stale(request("b", "old-loader"))).Should(BeTrue(), "the old document starts a request")
		Ω(origins.Stale(&network.EventResponseReceived{RequestID: "a"})).Should(BeTrue())
		Ω(origins.Stale(&network.EventLoadingFinished{RequestID: "a"})).Should(BeTrue())
		Ω(origins.Stale(&network.EventLoadingFailed{RequestID: "b"})).Should(BeTrue())
		Ω(origins.Stale(&cdplog.EventEntryAdded{Entry: &cdplog.Entry{NetworkRequestID: "b"}})).Should(BeTrue())

		Ω(origins.Stale(request("c", "new-loader"))).Should(BeFalse())
		Ω(origins.Stale(request("c", "new-loader"))).Should(BeFalse(), "a redirect hop")
		Ω(origins.Stale(&network.EventLoadingFinished{RequestID: "c"})).Should(BeFalse())
		Ω(origins.Stale(&cdplog.EventEntryAdded{Entry: &cdplog.Entry{}})).Should(BeFalse())
	})

	It("never drops events it doesn't attribute", func() {
		generation.Advance()
		Ω(origins.Stale(&page.EventJavascriptDialogOpening{})).Should(BeFalse())
	})

	It("forgets what two generations back created", func() {
		origins.Stale(created(1))
		generation.Advance()
		generation.Advance()
		Ω(origins.Stale(console(1))).Should(BeFalse())
	})
})

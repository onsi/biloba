package engine_test

import (
	"errors"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

func suffix(value string) *engine.Expectation {
	return &engine.Expectation{Kind: engine.ExpectSuffix, Expected: value}
}

var _ = Describe("what a session observes", func() {
	var session *engine.Session

	BeforeEach(func(ctx SpecContext) {
		var err error
		session, err = browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
	})

	It("records each request's status and failure, a redirected hop's status included, and filters on them", func(ctx SpecContext) {
		_, err := session.EvaluateAsync(ctx, `Promise.all([fetch("/redirect").then(r => r.text()), fetch("/not-found").then(r => r.text()), fetch("http://127.0.0.1:1/unreachable").catch(() => null)]).then(() => true)`)
		Expect(err).NotTo(HaveOccurred())
		failed, err := session.WaitForRequest(ctx, engine.RequestQuery{Failed: true}, engine.PollPolicy{Timeout: 2 * time.Second})
		Expect(err).NotTo(HaveOccurred())
		Expect(failed.URL).To(HaveSuffix("/unreachable"))
		Expect(failed.Failure).To(HavePrefix("net::ERR_"))
		Expect(failed.Status).To(Equal(0))

		status := func(path string) int {
			matches := session.RequestsMatching(engine.RequestQuery{URL: suffix(path)})
			Expect(matches).To(HaveLen(1))
			return matches[0].Status
		}
		Eventually(func() []int { return []int{status("/redirect"), status("/slow"), status("/not-found")} }).Should(Equal([]int{302, 200, 404}))
		notFound := session.RequestsMatching(engine.RequestQuery{Status: &engine.Expectation{Kind: engine.ExpectEqual, Expected: 404}})
		Expect(notFound).To(HaveLen(1))
		Expect(notFound[0].URL).To(HaveSuffix("/not-found"))
		Expect(notFound[0].Failure).To(BeEmpty(), "an HTTP error status is a response, not a failure")
		Expect(session.RequestsMatching(engine.RequestQuery{URL: suffix("/not-found"), Failed: true})).To(BeEmpty())

		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.Requests()).To(BeEmpty())
	})

	It("names the requests still in flight when a wait for network idle times out", func(ctx SpecContext) {
		hold, err := session.HoldResponse(ctx, engine.Expectation{Kind: engine.ExpectSuffix, Expected: "/echo"})
		Expect(err).NotTo(HaveOccurred())
		_, err = session.Evaluate(ctx, `void fetch("/echo")`)
		Expect(err).NotTo(HaveOccurred())
		_, err = session.AwaitResponseHold(ctx, hold)
		Expect(err).NotTo(HaveOccurred())

		_, err = session.WaitForNetworkIdle(ctx, engine.PollPolicy{Timeout: 100 * time.Millisecond, Interval: 10 * time.Millisecond})
		var engineErr *engine.Error
		Expect(errors.As(err, &engineErr)).To(BeTrue())
		Expect(engineErr.Code).To(Equal(engine.CodeTimeout))
		Expect(err.Error()).To(MatchRegexp(`(?s)^wait for network idle: expected the session to be network idle, but it has 1 in-flight request\(s\)\n  GET http://\S+/echo \(Fetch, in flight for \d+ms\)$`))

		Expect(session.ReleaseResponseHold(ctx, hold)).To(Succeed())
		_, err = session.WaitForNetworkIdle(ctx, engine.PollPolicy{Timeout: 2 * time.Second})
		Expect(err).NotTo(HaveOccurred())
	})

	It("records uncaught exceptions and unhandled rejections as page errors, and delivers them live", func(ctx SpecContext) {
		subscription, err := session.SubscribePageErrors(8)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(subscription.Close)
		_, err = session.Evaluate(ctx, `setTimeout(function explode() { throw new TypeError("boom") }); Promise.reject(new Error("rejected"))`)
		Expect(err).NotTo(HaveOccurred())

		messages := func() []string {
			out := []string{}
			for _, pageError := range session.PageErrors() {
				out = append(out, pageError.Message)
			}
			return out
		}
		Eventually(messages).Should(ConsistOf("Uncaught TypeError: boom", "Uncaught (in promise) Error: rejected"))
		var live engine.PageError
		Eventually(subscription.Events()).Should(Receive(&live))
		Expect(live.Message).To(HavePrefix("Uncaught"))
		for _, pageError := range session.PageErrors() {
			if strings.Contains(pageError.Message, "boom") {
				Expect(pageError.Stack).NotTo(BeEmpty())
				Expect(pageError.Stack[0].FunctionName).To(Equal("explode"))
			}
		}

		Expect(session.Prepare(ctx)).To(Succeed())
		Expect(session.PageErrors()).To(BeEmpty())
	})

	It("records the messages Chrome logs itself in the console history, with their source", func(ctx SpecContext) {
		_, err := session.EvaluateAsync(ctx, `fetch("/not-found").then(() => true)`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(session.ConsoleMessages).Should(ContainElement(SatisfyAll(
			HaveField("Source", "network"),
			HaveField("Type", "error"),
			HaveField("Text", ContainSubstring("404")),
		)))
		_, err = session.Evaluate(ctx, `console.error("from the page")`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(session.ConsoleMessages).Should(ContainElement(SatisfyAll(HaveField("Source", ""), HaveField("Text", "from the page"))))
	})

	It("drops what the document a prepare left behind reports as it goes", func(ctx SpecContext) {
		// The page's fetches are still open when Prepare navigates away: the old document rejects them,
		// and Chrome can deliver the rejection and the failed loads after the reset.
		for range 3 {
			Expect(session.Navigate(ctx, server.URL)).To(Succeed())
			_, err := session.Evaluate(ctx, `fetch("/hang").then(r => r.text()); fetch("/hang?again"); void 0`)
			Expect(err).NotTo(HaveOccurred())
			Eventually(session.InflightRequestCount).Should(BeNumerically(">=", 2))
			Expect(session.Prepare(ctx)).To(Succeed())
			Consistently(func() int {
				return len(session.PageErrors()) + len(session.ConsoleMessages()) + len(session.Requests()) + session.InflightRequestCount()
			}, 300*time.Millisecond).Should(BeZero())
		}

		Expect(session.Navigate(ctx, server.URL)).To(Succeed())
		_, err := session.EvaluateAsync(ctx, `setTimeout(() => { throw new Error("next spec") }); fetch("/not-found").then(r => r.text())`)
		Expect(err).NotTo(HaveOccurred())
		Eventually(session.PageErrors).Should(ContainElement(HaveField("Message", "Uncaught Error: next spec")))
		Eventually(session.ConsoleMessages).Should(ContainElement(HaveField("Source", "network")))
		Expect(session.RequestsMatching(engine.RequestQuery{URL: suffix("/not-found")})).To(HaveLen(1))
	})
})

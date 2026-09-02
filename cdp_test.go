package biloba_test

import (
	"context"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// A Chrome that stops answering used to hang the goroutine that spoke to it, forever - no failing
// spec, just a suite that eventually died on its own timeout (issue #9).  Every command now runs
// under a backstop deadline, and these specs prove the two things that matter: it fails, and it says
// why.
//
// They must also be fast.  A spec that waits out a real 30s bound would be a bad spec in a suite
// whose whole point is that it runs in ~2s, so the bounds are shrunk to milliseconds through
// SetCDPTimeoutsForTest and the wedge itself is injected rather than real.  A genuinely wedged
// renderer (`while(true){}`) is reproducible but unrecoverable: it would keep a CPU pegged for the
// rest of the run, which under `make stress-test`'s 41 repeats is not a price worth paying for one
// assertion.  The one thing that IS exercised against real Chrome is the renderer crash, because
// there the tab recovers.
var _ = Describe("bounding the commands Biloba sends to Chrome", func() {
	Describe("when Chrome accepts a command and never answers", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/dom.html")
			Eventually("#hello").Should(b.Exist())
			DeferCleanup(biloba.SetCDPTimeoutsForTest(100*time.Millisecond, 300*time.Millisecond))
			DeferCleanup(biloba.SetWedgedCDPForTest(func() bool { return true }))
		})

		It("fails a one-shot mutation in bounded time, even though it accepts no poll-config knobs at all", func() {
			start := time.Now()
			b.Run("1+1")
			Expect(time.Since(start)).To(BeNumerically("<", time.Second))
			ExpectFailures(SatisfyAll(
				ContainSubstring("deadline_exceeded: Chrome did not evaluate JavaScript in the page within 100ms"),
				ContainSubstring("Chrome is wedged, badly overloaded, or the page is stuck in a long-running synchronous script"),
			))
		})

		It("fails a DOM interaction rather than blocking inside the matcher where Gomega cannot preempt it", func() {
			start := time.Now()
			b.Immediate().Click("#hello")
			Expect(time.Since(start)).To(BeNumerically("<", time.Second))
			ExpectFailures(ContainSubstring("deadline_exceeded: Chrome did not evaluate JavaScript in the page within 100ms"))
		})

		It("fails a polling method at its own deadline instead of polling against a dead browser", func() {
			start := time.Now()
			b.WithTimeout(50 * time.Millisecond).Click("#hello")
			Expect(time.Since(start)).To(BeNumerically("<", time.Second))
			ExpectFailures(ContainSubstring("deadline_exceeded"))
		})

		It("gives an awaited promise the longer bound - the page's own JavaScript, not Chrome, sets how long it takes", func() {
			b.RunAsync("return 1")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Chrome did not settle the promise the script awaited within 300ms"),
			))
		})

		It("does not let WithTimeout shorten the backstop: a poll bound and a liveness bound are different things", func() {
			// WithTimeout says how long to keep RETRYING - that is Eventually's business.  If it also
			// shortened each individual command, a healthy call that legitimately outran a tight
			// WithTimeout on a loaded machine would be cancelled mid-flight instead of completing.  So
			// the command still reports the backstop's own bound, not the caller's.
			DeferCleanup(biloba.SetCDPTimeoutsForTest(400*time.Millisecond, 400*time.Millisecond))
			b.WithTimeout(50 * time.Millisecond).Click("#hello")
			ExpectFailures(ContainSubstring("within 400ms"))
		})
	})

	Describe("when a tab's renderer crashes", func() {
		It("names the crash instead of reporting an unexplained timeout, and recovers on the next navigation", func() {
			tab := b.NewTab()
			tab.Navigate(fixtureServer + "/dom.html")
			Eventually("#hello").Should(tab.Exist())

			// Keep the fallback bound short: if a command on a crashed renderer blocks rather than
			// erroring, this spec should still finish in milliseconds.
			DeferCleanup(biloba.SetCDPTimeoutsForTest(500*time.Millisecond, 500*time.Millisecond))

			// Page.crash never answers - the renderer dies before it can reply - so it goes out on a
			// context we throw away rather than one we wait on.  The tab lives in its own browser
			// context, so no other tab (or parallel process) shares the renderer being killed.
			crashCtx, cancel := context.WithTimeout(tab.Context, 500*time.Millisecond)
			defer cancel()
			chromedp.Run(crashCtx, page.Crash())

			Eventually(tab.PageCrashedForTest).Should(BeTrue())

			_, err := tab.RunErr("1")
			Expect(err).To(MatchError(SatisfyAll(
				ContainSubstring("page_crashed: this tab's renderer crashed"),
				ContainSubstring("Navigate the tab again to get a fresh renderer"),
			)))

			By("a navigation gives the target a fresh renderer, which clears the crash")
			tab.Navigate(fixtureServer + "/dom.html")
			Expect(tab.PageCrashedForTest()).To(BeFalse())
			Eventually("#hello").Should(tab.Exist())
		})
	})
})

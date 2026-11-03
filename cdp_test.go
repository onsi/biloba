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
// assertion.  A real renderer crash is exercised at the bottom of this Describe, on a tab of its own.
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

	// A real renderer crash, with two things learned the hard way built in.
	//
	// Chrome announces a crash promptly on macOS and several seconds later on Linux.  An earlier
	// version of this spec sent Page.crash and navigated before Linux had announced the crash, and the
	// tab never came back: every later command on it failed, including the ones Prepare issued during
	// teardown, and those failures landed in the next spec (CI run 33589422597, both lanes).  The
	// TypeScript crash e2e kills the renderer, waits for PAGE_CRASHED, then navigates, and it recovers
	// on Linux.  So this spec waits for the announcement before navigating.
	//
	// The crash itself is a navigation to chrome://kill, which has the browser kill the tab's renderer
	// process: the in-browser counterpart of the e2e's SIGKILL.  The e2e can SIGKILL every renderer
	// because it owns its Chrome; this suite shares one Chrome across parallel processes, and CDP does
	// not say which process renders which tab.  The tab is a b.NewTab(), whose own browser context
	// gives it a renderer no other tab uses, so nothing else is touched.  If the tab does not come back,
	// the spec fails naming the cause and closes the tab, so the next spec's Prepare never sees it.
	Describe("when a tab's renderer crashes", func() {
		It("fails fast with page_crashed once Chrome announces it, and the tab recovers on the next navigation", func() {
			tab := b.NewTab()
			recovered := false
			DeferCleanup(func() {
				if !recovered {
					Expect(tab.Close()).To(Succeed())
				}
			})
			tab.Navigate(fixtureServer + "/dom.html")
			Eventually("#hello").Should(tab.Exist())

			By("leaving a command waiting on the renderer when it dies")
			inFlight := make(chan error, 1)
			go func() {
				defer GinkgoRecover()
				_, err := tab.RunErrAsync("window.waiting = true; await new Promise(() => {}); return 1")
				inFlight <- err
			}()
			Eventually(func() any { waiting, _ := tab.RunErr("window.waiting"); return waiting }).Should(Equal(true))

			// chrome://kill never answers: the renderer it would answer from is gone.  So it goes out on
			// a context that is thrown away rather than waited on.
			killCtx, cancel := context.WithTimeout(tab.Context, time.Second)
			chromedp.Call(killCtx, page.Navigate, page.NavigateParams{URL: "chrome://kill"})
			cancel()

			By("waiting for Chrome to announce the crash")
			Eventually(tab.PageCrashedForTest).WithTimeout(30*time.Second).Should(BeTrue(), "Chrome never announced the crash")
			Eventually(inFlight).Should(Receive(MatchError(ContainSubstring("page_crashed: this tab's renderer crashed"))),
				"the waiting command is interrupted by the announcement rather than left to its two-minute bound")

			By("failing later commands at once, without sending them")
			start := time.Now()
			_, err := tab.RunErr("1+1")
			Expect(time.Since(start)).To(BeNumerically("<", time.Second))
			Expect(err).To(MatchError(ContainSubstring("page_crashed: this tab's renderer crashed, so Chrome could not evaluate JavaScript in the page")))

			By("navigating, which gives the tab a fresh renderer")
			tab.Navigate(fixtureServer + "/dom.html")
			navigationFailures := gt.failures
			gt.failures = []string{}
			Expect(navigationFailures).To(BeEmpty(), "the crashed tab did not recover on navigation")
			Expect(tab.PageCrashedForTest()).To(BeFalse())
			Eventually("#hello").Should(tab.Exist())
			Expect(tab.Run("1+1")).To(BeEquivalentTo(2))
			Expect(gt.failures).To(BeEmpty(), "the recovered tab still fails commands")
			recovered = true
		})
	})
})

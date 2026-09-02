package biloba_test

import (
	"time"

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

	// There is deliberately no spec here that crashes a real renderer, and the reason is worth keeping.
	//
	// Two things about a crashed tab turn out to be Chrome's behaviour rather than Biloba's, and they
	// differ by platform.  Chrome announces the crash late on Linux and promptly on macOS - late enough
	// that a three-second sample sees nothing.  And the tab does not come back: on Linux a navigation
	// after the crash does not get a fresh renderer, so every later command on that tab fails, including
	// the ones Prepare issues during teardown.  That last part is what rules the spec out.  A tab that
	// cannot be recovered poisons the harness it was created in - gt.failures picks up the teardown
	// failures and the next spec inherits them - which is precisely the class of suite-wide flake the
	// rest of this session went to fix.
	//
	// So the crash path is covered where it can be covered honestly and cheaply: diagnoseCDPError's
	// vocabulary in cdp_internal_test.go, where the flag is set directly and the verdict is identical on
	// every platform, and the bounded-failure behaviour by the injected wedge above, which exercises the
	// same runCDP deadline end-to-end against real Chrome without leaving a corpse behind.
})

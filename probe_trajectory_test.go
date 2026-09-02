package biloba_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The poll trajectory failure artifact", func() {
	var g Gomega

	BeforeEach(func() {
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())
		b.ResetPollDiagnosticsForTest()
		// gt captures Fatalf instead of aborting, so a Gomega bound to it lets a spec drive an
		// assertion all the way to its timeout and then read the artifact that timeout produced.
		g = NewWithT(gt)
	})

	seed := func(html string) {
		GinkgoHelper()
		b.Run(`document.body.insertAdjacentHTML("beforeend", ` + "`" + html + "`" + `)`)
	}

	Describe("recording DOM value reads", func() {
		It("records the trajectory of a value matcher that times out", func() {
			seed(`<div id="probe-x">pending</div>`)
			g.Eventually("#probe-x").WithTimeout(200 * time.Millisecond).WithPolling(20 * time.Millisecond).Should(b.HaveTextContent("Saved"))
			ExpectFailures(ContainSubstring("Timed out"))

			trajectory := b.PollTrajectoryForTest()
			Ω(trajectory).Should(ContainSubstring("Probe: HaveProperty:textContent #probe-x"))
			Ω(trajectory).Should(ContainSubstring(`"pending"`))
			Ω(trajectory).Should(ContainSubstring("flat (value never changed"))
		})

		It("records the values a value matcher saw change along the way", func() {
			seed(`<div id="probe-tick">0</div>`)
			b.Run(`window.__tick = setInterval(() => {
				const el = document.getElementById("probe-tick")
				el.textContent = String(Number(el.textContent) + 1)
			}, 20)`)
			DeferCleanup(func() { b.Run(`clearInterval(window.__tick)`) })

			g.Eventually("#probe-tick").WithTimeout(300 * time.Millisecond).WithPolling(20 * time.Millisecond).Should(b.HaveTextContent("nope"))
			ExpectFailures(ContainSubstring("Timed out"))

			trajectory := b.PollTrajectoryForTest()
			Ω(trajectory).Should(ContainSubstring("Probe: HaveProperty:textContent #probe-tick"))
			Ω(trajectory).ShouldNot(ContainSubstring("flat"))
		})

		It("records the trajectory of a count that never arrives", func() {
			seed(`<div class="probe-card"></div><div class="probe-card"></div>`)
			g.Eventually(".probe-card").WithTimeout(200 * time.Millisecond).WithPolling(20 * time.Millisecond).Should(b.HaveCount(5))
			ExpectFailures(ContainSubstring("Timed out"))

			Ω(b.PollTrajectoryForTest()).Should(ContainSubstring("Probe: HaveCount .probe-card"))
			Ω(b.PollTrajectoryForTest()).Should(ContainSubstring("+0.00s  2"))
		})

		It("records the trajectory of a failed ShouldNot as well", func() {
			seed(`<div id="probe-stuck">busy</div>`)
			g.Eventually("#probe-stuck").WithTimeout(200 * time.Millisecond).WithPolling(20 * time.Millisecond).ShouldNot(b.HaveTextContent("busy"))
			ExpectFailures(ContainSubstring("Timed out"))

			Ω(b.PollTrajectoryForTest()).Should(ContainSubstring("Probe: HaveProperty:textContent #probe-stuck"))
			Ω(b.PollTrajectoryForTest()).Should(ContainSubstring(`"busy"`))
		})

		It("still records geometry matchers", func() {
			g.Eventually("#hello").WithTimeout(200 * time.Millisecond).WithPolling(20 * time.Millisecond).Should(b.HaveComputedStyleNumeric("width", BeNumerically(">", 100000)))
			ExpectFailures(ContainSubstring("Timed out"))

			Ω(b.PollTrajectoryForTest()).Should(ContainSubstring("Probe: HaveComputedStyleNumeric:width #hello"))
		})

		It("records EvaluateTo - the polling form for an arbitrary expression", func() {
			b.Run(`window.__probeCount = 3`)
			g.Eventually("window.__probeCount").WithTimeout(200 * time.Millisecond).WithPolling(20 * time.Millisecond).Should(b.EvaluateTo(BeNumerically(">", 10)))
			ExpectFailures(ContainSubstring("Timed out"))

			Ω(b.PollTrajectoryForTest()).Should(ContainSubstring("Probe: EvaluateTo window.__probeCount"))
			Ω(b.PollTrajectoryForTest()).Should(ContainSubstring("+0.00s  3"))
		})
	})

	Describe("attributing a trajectory to the failure it belongs to", func() {
		It("does not attach a read that PASSED to a later, unrelated failure", func() {
			seed(`<div id="probe-y">pending</div>`)
			b.Run(`window.__probeReady = true`)
			Eventually(b.GetJSValue("window.__probeReady")).Should(BeTrue()) // passes, and records

			b.WithTimeout(100 * time.Millisecond).WithPolling(20 * time.Millisecond).Click("#probe-never-exists")
			ExpectFailures(ContainSubstring("Timed out"))

			Ω(b.PollTrajectoryForTest()).Should(BeEmpty())
		})

		It("does not attach a b.Run setup line to a later failure", func() {
			seed(`<div id="probe-z">pending</div>`)
			b.WithTimeout(100 * time.Millisecond).WithPolling(20 * time.Millisecond).Click("#probe-never-exists")
			ExpectFailures(ContainSubstring("Timed out"))

			Ω(b.PollTrajectoryForTest()).Should(BeEmpty())
		})

		It("attaches the failing read's series, not the series of a read that already passed", func() {
			seed(`<div id="probe-a">done</div><div id="probe-b">pending</div>`)
			Eventually("#probe-a").Should(b.HaveTextContent("done")) // passes, and records

			g.Eventually("#probe-b").WithTimeout(200 * time.Millisecond).WithPolling(20 * time.Millisecond).Should(b.HaveTextContent("Saved"))
			ExpectFailures(ContainSubstring("Timed out"))

			trajectory := b.PollTrajectoryForTest()
			Ω(trajectory).Should(ContainSubstring("#probe-b"))
			Ω(trajectory).ShouldNot(ContainSubstring("probe-a"))
		})

		It("does not attach a trajectory to a failure that had no polled read behind it at all", func() {
			seed(`<div id="probe-c">pending</div>`)
			Eventually("#probe-c").Should(b.HaveTextContent("pending")) // passes, and records
			Ω(b.PollTrajectoryForTest()).Should(BeEmpty())
		})
	})
})

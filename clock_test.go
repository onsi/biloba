package biloba_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba"
)

var _ = Describe("Controlling time", func() {
	t0 := time.Date(2024, 2, 2, 8, 0, 0, 0, time.UTC)
	ms := func(t time.Time) float64 { return float64(t.UnixMilli()) }
	pausedNote := "This tab's page clock is paused at 2024-02-02T08:00:00.000Z; timers and animation frames won't run until you call RunFor, FastForward, or Resume."
	var clock *biloba.Clock

	onRealTime := func() {
		GinkgoHelper()
		Ω(`typeof window._bilobaClock`).Should(b.EvaluateTo("undefined"))
		Ω(`Date.now()`).Should(b.EvaluateTo(BeNumerically("~", float64(time.Now().UnixMilli()), 5000)))
	}

	Describe("with the clock paused before the page loads", func() {
		BeforeEach(func() {
			clock = b.InstallClock(t0)
			clock.PauseAt(t0)
			b.Navigate(fixtureServer + "/clock.html")
			Eventually("#countdown").Should(b.Exist())
		})

		It("starts the page at the paused time and runs none of its timers", func() {
			Ω(b.GetInnerText("#now")).Should(Equal("2024-02-02T08:00:00.000Z"))
			Ω(`Date.now()`).Should(b.EvaluateTo(ms(t0)))
			Consistently("#countdown", 200*time.Millisecond).Should(b.HaveInnerText("10"))
			Ω(b.GetInnerText("#frames")).Should(Equal("0"))
		})

		It("runs every timer and animation frame that falls due with RunFor, promise callbacks included", func() {
			clock.RunFor(300 * time.Millisecond)
			Ω(b.GetInnerText("#chain")).Should(Equal("one two three"))
			Ω(b.GetInnerText("#frames")).Should(Equal("18"), "one frame every 16ms")
			Ω(b.GetInnerText("#later")).Should(BeEmpty())

			clock.RunFor(4700 * time.Millisecond)
			Ω(b.GetInnerText("#countdown")).Should(Equal("5"))
			Ω(b.GetInnerText("#later")).Should(Equal("later"))
			Ω(`Date.now()`).Should(b.EvaluateTo(ms(t0.Add(5 * time.Second))))

			clock.RunFor(10 * time.Second)
			Ω(b.GetInnerText("#countdown")).Should(Equal("0"))
		})

		It("fires each due timer once with FastForward", func() {
			clock.FastForward(time.Hour)
			Ω(b.GetInnerText("#countdown")).Should(Equal("9"), "an interval that fell due 3600 times fires once")
			Ω(b.GetInnerText("#later")).Should(Equal("later"))
			Ω(b.GetInnerText("#ago")).Should(Equal("60 minutes ago"))
			Ω(`Date.now()`).Should(b.EvaluateTo(ms(t0.Add(time.Hour))))
		})

		It("runs a debounce only once its delay has passed", func() {
			b.Click("#debounced")
			clock.RunFor(200 * time.Millisecond)
			b.Click("#debounced")
			clock.RunFor(299 * time.Millisecond)
			Ω(b.GetInnerText("#saved")).Should(BeEmpty())
			clock.RunFor(time.Millisecond)
			Ω(b.GetInnerText("#saved")).Should(Equal("saved at 2024-02-02T08:00:00.500Z"))
		})

		It("pins Date with SetFixedTime while timers keep running, and moves it with SetSystemTime", func() {
			fixed := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			clock.SetFixedTime(fixed)
			clock.RunFor(5 * time.Second)
			Ω(b.GetInnerText("#later")).Should(Equal("later"))
			Ω(`[Date.now(), new Date().getTime()]`).Should(b.EvaluateTo([]any{ms(fixed), ms(fixed)}))

			clock.SetSystemTime(t0.Add(-time.Hour))
			Ω(`Date.now()`).Should(b.EvaluateTo(ms(t0.Add(-time.Hour))), "SetSystemTime unpins Date")
			Ω(b.GetInnerText("#countdown")).Should(Equal("5"), "and fires nothing")
			clock.RunFor(999 * time.Millisecond)
			Ω(b.GetInnerText("#countdown")).Should(Equal("5"))
			clock.RunFor(time.Millisecond)
			Ω(b.GetInnerText("#countdown")).Should(Equal("4"), "a pending interval keeps the delay it had left")
		})

		It("keeps performance.now in step with the clock", func() {
			var before, after float64
			b.Run(`performance.now()`, &before)
			clock.RunFor(1500 * time.Millisecond)
			b.Run(`performance.now()`, &after)
			Ω(after - before).Should(BeNumerically("~", 1500, 0.001))
		})

		It("runs idle callbacks", func() {
			b.Run(`requestIdleCallback(() => window.idled = true)`)
			clock.RunFor(100 * time.Millisecond)
			Ω(`window.idled`).Should(b.EvaluateTo(true))
		})

		It("lets time flow again with Resume", func() {
			clock.Resume()
			Eventually("#countdown").WithTimeout(3 * time.Second).Should(b.HaveInnerText("9"))
			Eventually(func() int {
				frames, _ := strconv.Atoi(b.GetInnerText("#frames"))
				return frames
			}).Should(BeNumerically(">", 5))
		})

		It("gives every document the tab loads the clock where it is", func() {
			clock.RunFor(time.Minute)
			b.Navigate(fixtureServer + "/clock.html")
			Eventually("#countdown").Should(b.Exist())
			Ω(b.GetInnerText("#now")).Should(Equal("2024-02-02T08:01:00.000Z"))
			Ω(b.GetInnerText("#countdown")).Should(Equal("10"), "still paused")
		})

		It("returns the installed clock from Clock(), acting through the view", func() {
			b.Clock().RunFor(time.Second)
			Ω(b.GetInnerText("#countdown")).Should(Equal("9"))
			b.WithTimeout(time.Minute).Clock().RunFor(time.Second)
			Ω(b.GetInnerText("#countdown")).Should(Equal("8"))
		})

		Describe("Biloba itself", func() {
			It("keeps running on real time", func() {
				Eventually("#countdown").Should(b.HaveInnerText("10"))
				Ω(b.GetInnerText("#countdown")).Should(Equal("10"))
				b.Realistic().Click("#debounced")
				clock.RunFor(300 * time.Millisecond)
				Ω(b.GetInnerText("#saved")).Should(Equal("saved at 2024-02-02T08:00:00.300Z"))

				root := GinkgoT().TempDir()
				DeferCleanup(b.SetVisualDirsForTest(filepath.Join(root, "baselines"), filepath.Join(root, "artifacts")))
				func() {
					defer b.SetUpdateScreenshotsForTest(true)()
					Eventually(b).Should(b.HaveScreenshot("paused-clock"))
				}()
				_, err := os.Stat(filepath.Join(root, "baselines", "paused-clock.png"))
				Ω(err).ShouldNot(HaveOccurred())
				Eventually(b).Should(b.HaveScreenshot("paused-clock"))
			})

			It("says the clock is paused when a poll times out", func() {
				b.WithTimeout(100 * time.Millisecond).Click("#not-there")
				ExpectFailures(SatisfyAll(ContainSubstring("Timed out after"), ContainSubstring(pausedNote)))
				Ω(b.PausedClockReportForTest()).Should(BeEmpty(), "the failure already says it")
			})

			It("puts the note in the failure report when the spec's own Eventually times out", func() {
				g := NewWithT(gt)
				g.Eventually("#countdown").WithTimeout(100 * time.Millisecond).Should(b.HaveInnerText("9"))
				ExpectFailures(SatisfyAll(ContainSubstring("Timed out after"), Not(ContainSubstring("page clock"))))
				Ω(b.PausedClockReportForTest()).Should(Equal(pausedNote))
			})

			It("leaves the note out once time flows, and for Immediate", func() {
				b.Immediate().Click("#not-there")
				ExpectFailures(Not(ContainSubstring("page clock")))
				clock.Resume()
				b.WithTimeout(100 * time.Millisecond).Click("#not-there")
				ExpectFailures(SatisfyAll(ContainSubstring("Timed out after"), Not(ContainSubstring("page clock"))))
				Ω(b.PausedClockReportForTest()).Should(BeEmpty())
			})
		})
	})

	It("says in a failed spec's report that the clock was paused, as the clock is removed", func() {
		// cleanups run last-registered first, so this one runs after the clock's
		DeferCleanup(func() {
			Ω(gt.reportEntries).Should(ContainElement("Page clock paused"))
			gt.failed = false
		})
		b.InstallClock(t0).PauseAt(t0)
		gt.failed = true
	})

	It("starts flowing from t0", func() {
		b.InstallClock(t0)
		b.Navigate(fixtureServer + "/clock.html")
		Eventually("#countdown").Should(b.Exist())
		Ω(b.GetInnerText("#now")).Should(HavePrefix("2024-02-02T08:00:0"))
		Eventually("#countdown").WithTimeout(3 * time.Second).Should(b.HaveInnerText("9"))
		Ω(`Date.now()`).Should(b.EvaluateTo(BeNumerically("~", ms(t0.Add(time.Second)), 1000)))
	})

	It("installs in a page that has already loaded", func() {
		b.Navigate(fixtureServer + "/clock.html")
		Eventually("#countdown").Should(b.Exist())
		var before float64
		b.Run(`performance.now()`, &before)
		clock = b.InstallClock(t0)
		clock.PauseAt(t0)
		Ω(`Date.now()`).Should(b.EvaluateTo(ms(t0)))
		Ω(`performance.now()`).Should(b.EvaluateTo(BeNumerically(">=", before)), "performance.now does not go backwards")
		b.Run(`setTimeout(() => window.fired = true, 1000)`)
		clock.RunFor(999 * time.Millisecond)
		Ω(`window.fired`).Should(b.EvaluateTo(BeNil()))
		clock.RunFor(time.Millisecond)
		Ω(`window.fired`).Should(b.EvaluateTo(true))
	})

	It("moves the clock back without firing anything when PauseAt is behind it", func() {
		clock = b.InstallClock(t0)
		b.Navigate(fixtureServer + "/clock.html")
		Eventually("#countdown").WithTimeout(3 * time.Second).Should(b.HaveInnerText("9"))
		clock.PauseAt(t0)
		Ω(`Date.now()`).Should(b.EvaluateTo(ms(t0)))
		Ω(b.GetInnerText("#countdown")).Should(Equal("9"))
	})

	It("is per tab", func() {
		b.Navigate(fixtureServer + "/clock.html")
		tab := b.NewTab()
		tab.InstallClock(t0).PauseAt(t0)
		tab.Navigate(fixtureServer + "/clock.html")
		Eventually("#countdown").Should(tab.Exist())
		Ω(tab.GetInnerText("#now")).Should(Equal("2024-02-02T08:00:00.000Z"))
		onRealTime()
	})

	It("bounds RunFor by real time and says how far it got", func() {
		clock = b.InstallClock(t0)
		clock.PauseAt(t0)
		b.Navigate(fixtureServer + "/clock.html")
		Eventually("#countdown").Should(b.Exist())
		b.Run(`setInterval(() => {}, 1)`)
		b.WithTimeout(50 * time.Millisecond).Clock().RunFor(time.Hour)
		ExpectFailures(SatisfyAll(ContainSubstring("RunFor(1h0m0s) did not finish within 50ms"), ContainSubstring("FastForward skips ahead")))
	})

	Describe("frames", func() {
		var frame *biloba.Biloba
		find := func() {
			GinkgoHelper()
			frame = b.Frame(b.FrameMatching().WithURL(ContainSubstring("/clock.html")).WithDOMElement("#countdown"))
		}

		It("drives the tab's frames with it", func() {
			clock = b.InstallClock(t0)
			clock.PauseAt(t0)
			b.Navigate(fixtureServer + "/frames.html?child=" + crossOriginFixtureServer + "/clock.html")
			find()
			Ω(frame.GetInnerText("#now")).Should(Equal("2024-02-02T08:00:00.000Z"))
			clock.RunFor(5 * time.Second)
			Ω(frame.GetInnerText("#countdown")).Should(Equal("5"))
			Ω(frame.GetInnerText("#later")).Should(Equal("later"))
		})

		It("installs in frames that have already loaded, and ones added later", func() {
			b.Navigate(fixtureServer + "/frames.html?child=" + crossOriginFixtureServer + "/clock.html")
			find()
			clock = b.InstallClock(t0)
			clock.PauseAt(t0)
			Ω(`Date.now()`).Should(frame.EvaluateTo(ms(t0)))
			clock.RunFor(time.Minute)
			b.Run(`const late = document.createElement("iframe"); late.id = "late"; late.src = "/clock.html"; document.body.appendChild(late)`)
			Eventually("#late >>> #now").Should(b.HaveInnerText("2024-02-02T08:01:00.000Z"))
		})
	})

	Describe("misuse", func() {
		It("fails a second InstallClock, and methods with no clock installed", func() {
			b.Clock()
			ExpectFailures(ContainSubstring("Clock: this tab has no page clock - call InstallClock first"))
			clock = b.InstallClock(t0)
			b.InstallClock(t0)
			ExpectFailures(ContainSubstring("InstallClock: this tab's page clock is already installed"))
			clock.RunFor(-time.Second)
			clock.FastForward(-time.Second)
			ExpectFailures(
				"RunFor: the duration must not be negative, got -1s",
				"FastForward: the duration must not be negative, got -1s",
			)
		})

		It("rejects the poll knobs, except WithTimeout and WithContext on RunFor", func() {
			b.WithTimeout(time.Second).InstallClock(t0)
			ExpectFailures("InstallClock does not support WithTimeout")
			b.Immediate().Clock().PauseAt(t0)
			b.WithTimeout(time.Second).Clock().Resume()
			b.WithPolling(time.Second).Clock().FastForward(time.Second)
			b.WithTimeout(time.Second).Clock().SetFixedTime(t0)
			b.WithContext(context.Background()).Clock().SetSystemTime(t0)
			b.WithPolling(time.Second).Clock().RunFor(time.Second)
			b.Immediate().Clock().RunFor(time.Second)
			ExpectFailures(
				"PauseAt does not support Immediate",
				"Resume does not support WithTimeout",
				"FastForward does not support WithPolling",
				"SetFixedTime does not support WithTimeout",
				"SetSystemTime does not support WithContext",
				"RunFor does not support WithPolling",
				"RunFor does not support Immediate",
			)
			b.WithTimeout(time.Minute).WithContext(context.Background()).Clock().RunFor(time.Second)
		})
	})

	Describe("cleaning up", Ordered, func() {
		It("installs a clock", func() {
			b.InstallClock(t0).PauseAt(t0)
			b.Navigate(fixtureServer + "/clock.html")
			Eventually("#countdown").Should(b.Exist())
			Ω(`Date.now()`).Should(b.EvaluateTo(ms(t0)))
		})

		It("is gone when the spec ends, from the current page and the next one", func() {
			onRealTime()
			b.Navigate(fixtureServer + "/clock.html")
			Eventually("#countdown").Should(b.Exist())
			onRealTime()
			b.InstallClock(t0)
		})
	})

	It("is removed by Prepare", func() {
		b.InstallClock(t0).PauseAt(t0)
		b.Navigate(fixtureServer + "/clock.html")
		Eventually("#countdown").Should(b.Exist())
		b.Prepare()
		b.Navigate(fixtureServer + "/clock.html")
		Eventually("#countdown").Should(b.Exist())
		onRealTime()
		b.InstallClock(t0)
	})
})

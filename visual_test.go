package biloba_test

import (
	"bytes"
	"context"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
)

// Baselines are never committed: pixel output varies by machine, OS, Chrome build and font stack, so
// a checked-in PNG would fail on someone else's laptop.  Every spec below generates the baseline it
// asserts against into a per-spec TempDir, which exercises the whole machinery (capture, mask,
// compare, diagnose, artifact writing) without depending on cross-machine pixel stability.
var _ = Describe("Visual assertions", func() {
	var baselinesDir, artifactsDir string
	var failure string
	var g Gomega

	BeforeEach(func() {
		root := GinkgoT().TempDir()
		baselinesDir, artifactsDir = filepath.Join(root, "baselines"), filepath.Join(root, "artifacts")
		DeferCleanup(b.SetVisualDirsForTest(baselinesDir, artifactsDir))
		failure = ""
		g = NewGomega(func(message string, callerSkip ...int) { failure = message })
	})

	baseline := func(name string) string { return filepath.Join(baselinesDir, name+".png") }
	artifact := func(name string) string { return filepath.Join(artifactsDir, name+".png") }

	// generate stores the subject's current rendering as the baseline for name, the way
	// BILOBA_UPDATE_SCREENSHOTS would.
	generate := func(subject any, name string, options ...biloba.ScreenshotOption) {
		GinkgoHelper()
		defer b.SetUpdateScreenshotsForTest(true)()
		Eventually(subject).Should(b.HaveScreenshot(name, options...))
	}

	readPNG := func(path string) []byte {
		GinkgoHelper()
		raw, err := os.ReadFile(path)
		Ω(err).ShouldNot(HaveOccurred())
		_, _, err = image.Decode(bytes.NewBuffer(raw))
		Ω(err).ShouldNot(HaveOccurred(), "%s is not a decodable PNG", path)
		return raw
	}

	saysPath := func(label string, path string) OmegaMatcher {
		return MatchRegexp(regexp.QuoteMeta(label) + `\s+` + regexp.QuoteMeta(path))
	}

	Describe("the happy path", func() {
		BeforeEach(func() {
			b.SetWindowSize(600, 400)
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#box").Should(b.Exist())
		})

		It("passes against a baseline of one element", func() {
			generate("#box", "box")
			Ω(baseline("box")).Should(BeAnExistingFile())
			Eventually("#box").Should(b.HaveScreenshot("box"))
		})

		It("applies to the tab itself to compare the whole page", func() {
			generate(b, "page")
			Ω(baseline("page")).Should(BeAnExistingFile())
			Eventually(b).Should(b.HaveScreenshot("page"))
		})

		It("organises baselines into subdirectories", func() {
			generate("#box", "cards/box")
			Ω(filepath.Join(baselinesDir, "cards", "box.png")).Should(BeAnExistingFile())
			Eventually("#box").Should(b.HaveScreenshot("cards/box"))
		})

		It("composes with other matchers", func() {
			generate("#box", "box")
			Eventually("#box").Should(SatisfyAll(b.BeInViewport(b.Fully()), b.HaveScreenshot("box")))
		})

		It("treats a mask selector that matches nothing as a no-op", func() {
			// the no-op mask is present when the baseline is generated too, so it is exercised on both
			// sides of the comparison - and #dot is masked for real alongside it, then repainted, which
			// is what makes this fail if the whole Mask option were being ignored
			generate("#box", "box", b.Mask("#not-on-this-page", "#dot"))
			b.Run("document.getElementById('box').classList.add('spot')")
			Eventually("#box").Should(b.HaveScreenshot("box", b.Mask("#not-on-this-page", "#dot")))
		})
	})

	Describe("the poll is the comparison", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual_dynamic.html")
			Eventually("#settling.settled").Should(b.Exist())
			generate("#settling", "settling")
		})

		It("does not match the unsettled rendering", func() {
			b.Run("resetSettling(true)")
			Eventually("#settling").ShouldNot(b.HaveScreenshot("settling"))
		})

		It("waits out an element that settles on a timer", func() {
			b.Run("resetSettling(false)")
			start := time.Now()
			Eventually("#settling").Should(b.HaveScreenshot("settling"))
			// the fixture settles 600ms after the reset, so a single-shot capture could not have
			// matched: the assertion can only have passed by re-capturing and re-comparing.
			Ω(time.Since(start)).Should(BeNumerically(">", 300*time.Millisecond))
		})
	})

	Describe("a missing baseline", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#box").Should(b.Exist())
		})

		It("fails loudly and immediately, and writes the actual but not the baseline", func() {
			start := time.Now()
			g.Eventually("#box").WithTimeout(30 * time.Second).WithPolling(50 * time.Millisecond).Should(b.HaveScreenshot("box"))
			Ω(time.Since(start)).Should(BeNumerically("<", 5*time.Second), "a missing baseline must not burn the Eventually timeout")

			Ω(failure).Should(SatisfyAll(
				ContainSubstring("There is no screenshot baseline for box"),
				ContainSubstring(baseline("box")),
				ContainSubstring("BILOBA_UPDATE_SCREENSHOTS=1"),
				ContainSubstring(artifact("box.actual")),
			))
			readPNG(artifact("box.actual"))
			Ω(baseline("box")).ShouldNot(BeAnExistingFile())
		})
	})

	Describe("update mode", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#box").Should(b.Exist())
		})

		It("writes the baseline and passes", func() {
			defer b.SetUpdateScreenshotsForTest(true)()
			Eventually("#box").Should(b.HaveScreenshot("box"))
			readPNG(baseline("box"))
			Ω(gt.buffer).Should(gbytes.Say("Wrote a new screenshot baseline for box:"))
		})

		It("leaves a baseline that a subsequent normal run passes against", func() {
			generate("#box", "box")
			Eventually("#box").Should(b.HaveScreenshot("box"))
		})

		It("reports in words what it changed when it overwrites a baseline", func() {
			generate("#box", "box")
			b.Run("document.getElementById('box').classList.add('spot')")
			Eventually("#box").ShouldNot(b.HaveScreenshot("box"))

			defer b.SetUpdateScreenshotsForTest(true)()
			Eventually("#box").Should(b.HaveScreenshot("box"))
			Ω(gt.buffer).Should(gbytes.Say("Updated the screenshot baseline for box:"))
			Ω(gt.buffer).Should(gbytes.Say(`screenshot "box" updated — [\d,]+ of [\d,]+ pixels changed`))
		})
	})

	Describe("update mode settles before it writes", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual_dynamic.html")
			Eventually("#churn").Should(b.Exist())
		})

		printed := func() string { return string(gt.buffer.Contents()) }

		It("waits out a page that is still changing and writes the settled rendering", func() {
			b.Run("churnFor(400)")
			generate("#churn", "churn")
			Ω(printed()).ShouldNot(ContainSubstring("never settled"))

			// the fixture is demonstrably done changing now, so a single-shot comparison can only
			// pass if the baseline update mode wrote is this settled rendering rather than one of
			// the frames it was cycling through while it was written
			Eventually("#churn.settled").Should(b.Exist())
			Ω("#churn").Should(b.HaveScreenshot("churn"))
		})

		It("still writes a baseline when the page never settles, and says so loudly", func() {
			generate("#counter-card", "card")
			readPNG(baseline("card"))
			Ω(printed()).Should(SatisfyAll(
				ContainSubstring("The screenshot for card never settled: no 3 captures in a row matched, across 8 captures over"),
				ContainSubstring("Biloba wrote the last one"),
				ContainSubstring("Mask the changing region with b.Mask(...)"),
			))
		})

		It("is not fooled by a page that repeats on a period", func() {
			// #periodic repeats every 160ms, which is what a fixed 100ms capture interval samples at
			// once the capture round trip is counted: every pair of captures matches and the element
			// reads as settled.  The gaps between captures grow by a different amount each time, and
			// each of those amounts is less than a full period, so no two captures in a row can land on
			// the same shade - never mind three.
			b.Run("startPeriodicChurn()")
			generate("#periodic", "periodic")
			Ω(printed()).Should(ContainSubstring("The screenshot for periodic never settled"))
		})

		It("settles once the perpetually changing region is masked out", func() {
			generate("#counter-card", "card", b.Mask("#counter"))
			Ω(printed()).ShouldNot(ContainSubstring("never settled"))
			Ω("#counter-card").Should(b.HaveScreenshot("card", b.Mask("#counter")))
		})
	})

	// These pin the settle schedule described on screenshotSettleStreak in visual.go: three matching
	// captures, not two, and a growing gap between them.  visual_sampling.html reacts to the captures
	// themselves, which is what makes the assertions deterministic rather than a question of whether
	// this machine's capture cadence happens to hit the fixture's period.
	Describe("how update mode samples the page", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual_sampling.html")
			Eventually("#adversary").Should(b.Exist())
		})

		It("does not settle for two matching captures in a row", func() {
			// the fixture renders the same for two captures out of every three, so a settle check that
			// stopped at two would write a baseline here
			generate("#adversary", "adversary")
			Ω(string(gt.buffer.Contents())).Should(ContainSubstring("The screenshot for adversary never settled"))
		})

		It("waits a different, growing amount before each capture", func() {
			// asserted on the schedule rather than on observed timings: a capture round trip costs
			// hundreds of milliseconds under load and varies by nearly as much, which drowns out the
			// differences that matter here
			schedule := biloba.ScreenshotSettleScheduleForTest()
			Ω(schedule).Should(HaveLen(7))
			for i := 1; i < len(schedule); i++ {
				Ω(schedule[i]).Should(BeNumerically(">", schedule[i-1]), "gap %d is not longer than gap %d", i, i-1)
			}
			for i := 2; i < len(schedule); i++ {
				Ω(schedule[i]-schedule[i-1]).Should(BeNumerically(">", schedule[i-1]-schedule[i-2]), "the step into gap %d is not bigger than the step before it", i)
			}
		})

		It("applies that schedule to the page it is capturing", func() {
			generate("#adversary", "adversary")

			var captureTimes []float64
			b.Run("window.captureTimes", &captureTimes)
			Ω(captureTimes).Should(HaveLen(8), "a page that never settles is captured the full number of times")

			gaps := []float64{}
			for i := 1; i < len(captureTimes); i++ {
				gaps = append(gaps, captureTimes[i]-captureTimes[i-1])
			}
			// the last three gaps are nominally 1080ms longer in total than the first three; compared in
			// threes so the jitter of a single capture round trip cannot decide the outcome
			early, late := gaps[0]+gaps[1]+gaps[2], gaps[4]+gaps[5]+gaps[6]
			Ω(late).Should(BeNumerically(">", early+300), "the captures did not slow down: gaps were %v", gaps)
		})
	})

	Describe("a real mismatch", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#box").Should(b.Exist())
			generate("#box", "box")
		})

		It("diagnoses what changed and writes the actual and diff artifacts", func() {
			b.Run("document.getElementById('box').classList.add('spot')")
			g.Eventually("#box").WithTimeout(2 * time.Second).WithPolling(200 * time.Millisecond).Should(b.HaveScreenshot("box"))

			Ω(failure).Should(SatisfyAll(
				ContainSubstring(`screenshot "box" differs from baseline`),
				MatchRegexp(`[\d,]+ of [\d,]+ pixels differ \(\d+\.\d+%\), max channel delta \d+`),
				ContainSubstring("changed region: one box,"),
				saysPath("baseline:", baseline("box")),
				saysPath("actual:", artifact("box.actual")),
				saysPath("diff:", artifact("box.diff")),
			))
			readPNG(artifact("box.actual"))
			readPNG(artifact("box.diff"))
		})

		It("writes no artifacts when the comparison passes", func() {
			Eventually("#box").Should(b.HaveScreenshot("box"))
			Ω(artifact("box.diff")).ShouldNot(BeAnExistingFile())
			Ω(artifact("box.actual")).ShouldNot(BeAnExistingFile())
		})

		It("reports a size change as a size change, with no diff image to point at", func() {
			// an element that got taller because its content changed is the likeliest real-world
			// failure, and the one path where there is no per-pixel story to tell: the two images
			// cannot be compared pixel by pixel at all, so there is no diff PNG and no diff: line
			b.Run("document.getElementById('box').classList.add('taller')")
			g.Eventually("#box").WithTimeout(2 * time.Second).WithPolling(200 * time.Millisecond).Should(b.HaveScreenshot("box"))

			Ω(failure).Should(SatisfyAll(
				ContainSubstring(`screenshot "box" differs from baseline`),
				MatchRegexp(`baseline is \d+x\d+, actual is \d+x\d+ \(\d+px taller\)`),
				saysPath("baseline:", baseline("box")),
				saysPath("actual:", artifact("box.actual")),
			))
			Ω(failure).ShouldNot(ContainSubstring("pixels differ"))
			Ω(failure).ShouldNot(ContainSubstring("diff:"))
			readPNG(artifact("box.actual"))
			Ω(artifact("box.diff")).ShouldNot(BeAnExistingFile())
		})
	})

	Describe("masking", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual_dynamic.html")
			Eventually("#counter").Should(b.Exist())
		})

		It("fails without a mask over a region that changes on every tick", func() {
			generate("#counter-card", "card")
			Eventually("#counter-card").ShouldNot(b.HaveScreenshot("card"))
		})

		It("passes with the changing region masked out", func() {
			generate("#counter-card", "card", b.Mask("#counter"))
			ticks := b.GetInnerText("#counter")
			Eventually("#counter").ShouldNot(b.HaveInnerText(ticks))
			Ω("#counter-card").Should(b.HaveScreenshot("card", b.Mask("#counter")))
		})
	})

	Describe("tolerance", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#box").Should(b.Exist())
			generate("#box", "box")
		})

		It("Tolerance admits a small enough fraction of differing pixels", func() {
			// #dot is 144 of #box's 24,000 pixels: 0.6%
			b.Run("document.getElementById('box').classList.add('spot')")
			Eventually("#box").ShouldNot(b.HaveScreenshot("box"))
			Ω("#box").Should(b.HaveScreenshot("box", b.Tolerance(0.01)))
		})

		It("ChannelTolerance absorbs a small uniform colour shift", func() {
			// .shifted moves every channel by 3
			b.Run("document.getElementById('box').classList.add('shifted')")
			Eventually("#box").ShouldNot(b.HaveScreenshot("box"))
			Ω("#box").Should(b.HaveScreenshot("box", b.ChannelTolerance(6)))
		})
	})

	Describe("colour schemes", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#scheme").Should(b.Exist())
		})

		It("writes and compares one baseline per scheme", func() {
			generate("#scheme", "scheme", b.InColorSchemes("light", "dark"))
			Ω(baseline("scheme-light")).Should(BeAnExistingFile())
			Ω(baseline("scheme-dark")).Should(BeAnExistingFile())
			Ω(baseline("scheme")).ShouldNot(BeAnExistingFile())
			Eventually("#scheme").Should(b.HaveScreenshot("scheme", b.InColorSchemes("light", "dark")))
		})

		It("catches a change that is only visible in dark mode", func() {
			generate("#scheme", "scheme", b.InColorSchemes("light", "dark"))
			b.Run(`document.documentElement.style.setProperty("--dark-bg", "#004488")`)
			Ω("#scheme").Should(b.HaveScreenshot("scheme", b.InColorSchemes("light")))
			Ω("#scheme").ShouldNot(b.HaveScreenshot("scheme", b.InColorSchemes("light", "dark")))
		})

		It("tears the emulation down again", func() {
			generate("#scheme", "ambient")
			generate("#scheme", "scheme", b.InColorSchemes("light", "dark"))
			// the fixture really does render differently under emulation, so the ambient assertion
			// below would notice a leaked override
			Ω(readPNG(baseline("scheme-light"))).ShouldNot(Equal(readPNG(baseline("scheme-dark"))))

			Ω("#scheme").Should(b.HaveScreenshot("scheme", b.InColorSchemes("dark")))
			Ω("#scheme").Should(b.HaveScreenshot("ambient"))
		})

		It("clears an override that leaked past its teardown on the next Prepare", func() {
			generate("#scheme", "ambient")

			// Leak whichever scheme this browser is NOT already in: chrome-headless-shell renders
			// light, while the full-chrome lane follows the OS, so a hardcoded "dark" would be a no-op
			// on a machine in dark mode and the assertions below would say nothing.
			var prefersDark bool
			b.Run(`window.matchMedia("(prefers-color-scheme: dark)").matches`, &prefersDark)
			leaked := "dark"
			if prefersDark {
				leaked = "light"
			}

			// SetEmulatedMedia is a TARGET-level override: unlike the freeze stylesheet it survives a
			// navigation, so a capture whose deferred clear never landed (an expired capture timeout, a
			// cancelled context) would leave every later spec in this process rendering in the wrong
			// scheme.
			Ω(b.EmulateColorSchemeForTest(leaked)).Should(Succeed())
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#scheme").Should(b.Exist())
			Ω("#scheme").ShouldNot(b.HaveScreenshot("ambient")) // it really did outlive the navigation

			b.Prepare()
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#scheme").Should(b.Exist())
			Ω("#scheme").Should(b.HaveScreenshot("ambient"))
		})
	})

	Describe("the rendering freeze", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual_animated.html")
			Eventually("#animated").Should(b.Exist())
			generate("#animated", "frozen")
			generate("#animated", "running", b.Animated())
		})

		It("freezes animations by default, and Animated opts out", func() {
			Ω("#animated").Should(b.HaveScreenshot("frozen"))
			Ω("#animated").Should(b.HaveScreenshot("running", b.Animated()))
			Ω("#animated").ShouldNot(b.HaveScreenshot("running"))
			Ω("#animated").ShouldNot(b.HaveScreenshot("frozen", b.Animated()))
		})

		It("removes the freeze stylesheet after a passing assertion", func() {
			Ω("#animated").Should(b.HaveScreenshot("frozen"))
			Ω(b.HasElement("#_biloba-freeze")).Should(BeFalse())
		})

		It("removes the freeze stylesheet after a failing assertion", func() {
			g.Expect("#animated").Should(b.HaveScreenshot("running"))
			Ω(failure).Should(ContainSubstring("differs from baseline"))
			Ω(b.HasElement("#_biloba-freeze")).Should(BeFalse())
		})
	})

	Describe("the rendering freeze reaches open shadow roots", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual_shadow.html")
			Eventually("#widget >>> .flashing").Should(b.Exist())
		})

		freezeLog := func() string { return b.Run("window.freezeLog.join(',')").(string) }

		It("captures an animation inside a shadow root deterministically", func() {
			// the shadow animation cycles through three colours; without the freeze inside the root
			// no two captures agree, so update mode would never settle and the comparison below
			// would be comparing against whichever frame it gave up on
			generate("#widget", "widget")
			Ω(string(gt.buffer.Contents())).ShouldNot(ContainSubstring("never settled"))
			Ω("#widget").Should(b.HaveScreenshot("widget"))
		})

		It("injects the freeze stylesheet into the shadow root and takes it out again", func() {
			generate("#widget", "widget")
			Ω(freezeLog()).Should(ContainSubstring("added,removed"))
			Ω(freezeLog()).Should(HaveSuffix("removed"))
			Ω(b.HasElement("#widget >>> #_biloba-freeze")).Should(BeFalse())
		})

		It("takes the shadow-root stylesheet out again after a failing assertion", func() {
			generate("#widget", "widget")
			b.Run("document.getElementById('widget').classList.add('outlined')")
			g.Expect("#widget").Should(b.HaveScreenshot("widget"))
			Ω(failure).Should(ContainSubstring("differs from baseline"))
			Ω(freezeLog()).Should(HaveSuffix("removed"))
			Ω(b.HasElement("#widget >>> #_biloba-freeze")).Should(BeFalse())
		})
	})

	Describe("configuring the matcher is a hard error", func() {
		It("rejects WithTimeout", func() {
			b.WithTimeout(time.Second).HaveScreenshot("x")
			ExpectFailures(ContainSubstring("HaveScreenshot(...) returns a matcher - configure the Eventually/Expect that polls it, not HaveScreenshot with WithTimeout"))
		})

		It("rejects WithPolling", func() {
			b.WithPolling(time.Second).HaveScreenshot("x")
			ExpectFailures(ContainSubstring("not HaveScreenshot with WithPolling"))
		})

		It("rejects Immediate", func() {
			b.Immediate().HaveScreenshot("x")
			ExpectFailures(ContainSubstring("not HaveScreenshot with Immediate"))
		})

		It("rejects WithContext", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			b.WithContext(ctx).HaveScreenshot("x")
			ExpectFailures(ContainSubstring("not HaveScreenshot with WithContext"))
		})
	})

	Describe("suite-wide configuration", func() {
		// setEnv sets (or, with an empty value, unsets) an environment knob for the duration of the spec.
		setEnv := func(name string, value string) {
			GinkgoHelper()
			prev, had := os.LookupEnv(name)
			DeferCleanup(func() {
				if had {
					os.Setenv(name, prev)
				} else {
					os.Unsetenv(name)
				}
			})
			if value == "" {
				os.Unsetenv(name)
			} else {
				os.Setenv(name, value)
			}
		}

		Describe("the baselines directory", Label("no-browser"), func() {
			BeforeEach(func() { setEnv("BILOBA_SCREENSHOT_BASELINES_DIR", "") })

			It("defaults to ./biloba-baselines", func() {
				Ω(biloba.ConnectToChrome(gt).VisualBaselinesDirForTest()).Should(Equal("./biloba-baselines"))
			})

			It("is set by the option or the environment, and the option wins", func() {
				fromOption, fromEnv := GinkgoT().TempDir(), GinkgoT().TempDir()

				Ω(biloba.ConnectToChrome(gt, biloba.BilobaConfigScreenshotBaselinesDir(fromOption)).VisualBaselinesDirForTest()).Should(Equal(fromOption))

				setEnv("BILOBA_SCREENSHOT_BASELINES_DIR", fromEnv)
				Ω(biloba.ConnectToChrome(gt).VisualBaselinesDirForTest()).Should(Equal(fromEnv))
				Ω(biloba.ConnectToChrome(gt, biloba.BilobaConfigScreenshotBaselinesDir(fromOption)).VisualBaselinesDirForTest()).Should(Equal(fromOption))
			})
		})

		Describe("the suite-wide tolerance", func() {
			var bTol *biloba.Biloba

			BeforeEach(func() {
				bTol = biloba.ConnectToChrome(gt,
					biloba.BilobaConfigScreenshotTolerance(0.01),
					biloba.BilobaConfigScreenshotChannelTolerance(6))
				DeferCleanup(bTol.SetVisualDirsForTest(baselinesDir, artifactsDir))
				bTol.Navigate(fixtureServer + "/visual.html")
				Eventually("#box").Should(bTol.Exist())

				func() {
					defer bTol.SetUpdateScreenshotsForTest(true)()
					Eventually("#box").Should(bTol.HaveScreenshot("box"))
				}()
			})

			It("applies to every assertion, and a per-assertion option overrides it", func() {
				fraction, channel := bTol.ScreenshotToleranceForTest()
				Ω(fraction).Should(Equal(0.01))
				Ω(channel).Should(Equal(6))

				// #dot is 144 of #box's 24,000 pixels: 0.6%, inside the configured 1%
				bTol.Run("document.getElementById('box').classList.add('spot')")
				Ω("#box").Should(bTol.HaveScreenshot("box"))
				Ω("#box").ShouldNot(bTol.HaveScreenshot("box", bTol.Tolerance(0)))

				// .shifted moves every channel by 3, inside the configured 6
				bTol.Run("document.getElementById('box').classList.remove('spot')")
				bTol.Run("document.getElementById('box').classList.add('shifted')")
				Ω("#box").Should(bTol.HaveScreenshot("box"))
				Ω("#box").ShouldNot(bTol.HaveScreenshot("box", bTol.ChannelTolerance(0)))
			})
		})

		Describe("BILOBA_UPDATE_SCREENSHOTS", Label("no-browser"), func() {
			BeforeEach(func() { setEnv("BILOBA_UPDATE_SCREENSHOTS", "") })

			It("accepts the common truthy and falsy spellings", func() {
				for _, value := range []string{"1", "t", "true", "TRUE", "y", "yes", "Yes", "on", " true "} {
					setEnv("BILOBA_UPDATE_SCREENSHOTS", value)
					Ω(b.TruthyEnvForTest("BILOBA_UPDATE_SCREENSHOTS")).Should(BeTrue(), "%q should read as on", value)
				}
				for _, value := range []string{"", "0", "f", "false", "n", "no", "off", "OFF"} {
					setEnv("BILOBA_UPDATE_SCREENSHOTS", value)
					Ω(b.TruthyEnvForTest("BILOBA_UPDATE_SCREENSHOTS")).Should(BeFalse(), "%q should read as off", value)
				}
				Ω(string(gt.buffer.Contents())).ShouldNot(ContainSubstring("does not recognise"))
			})

			It("warns instead of silently doing nothing when it is set to something unrecognised", func() {
				setEnv("BILOBA_UPDATE_SCREENSHOTS", "ja")
				Ω(b.TruthyEnvForTest("BILOBA_UPDATE_SCREENSHOTS")).Should(BeFalse())
				Ω(gt.buffer).Should(gbytes.Say(`BILOBA_UPDATE_SCREENSHOTS is set to "ja", which Biloba does not recognise as a boolean - treating it as off`))
			})

			It("puts a suite into update mode from the environment", func() {
				setEnv("BILOBA_UPDATE_SCREENSHOTS", "yes")
				Ω(biloba.ConnectToChrome(gt).UpdateScreenshotsForTest()).Should(BeTrue())
			})
		})
	})

	Describe("bad input", func() {
		It("explains what the matcher can be applied to", func() {
			_, err := b.HaveScreenshot("box").Match(42)
			Ω(err).Should(MatchError(SatisfyAll(
				ContainSubstring("HaveScreenshot takes either a selector (a CSS string, an XPath, or a Locator) to capture one element, or the tab itself to capture the whole page"),
				ContainSubstring("<int>: 42"),
			)))
		})

		It("fails fast on a wrong-typed subject instead of polling one that can never resolve", func() {
			start := time.Now()
			g.Eventually(42).WithTimeout(30 * time.Second).WithPolling(50 * time.Millisecond).Should(b.HaveScreenshot("box"))
			Ω(time.Since(start)).Should(BeNumerically("<", 5*time.Second), "no amount of polling turns an int into a selector")
			Ω(failure).Should(ContainSubstring("HaveScreenshot takes either a selector"))
		})

		It("refuses a baseline name that is not a relative filename", func() {
			refuses := func(name string, expected string) {
				GinkgoHelper()
				_, err := b.HaveScreenshot(name).Match("#box")
				Ω(err).Should(MatchError(ContainSubstring(expected)))
			}
			refuses("", "HaveScreenshot needs a name to look its baseline up by")
			refuses("   ", "HaveScreenshot needs a name to look its baseline up by")
			refuses("/tmp/box", `the screenshot name "/tmp/box" must be relative to the baselines directory`)
			refuses("../box", `may not contain empty, "." or ".." path segments`)
			refuses("cards//box", `may not contain empty, "." or ".." path segments`)
		})
	})
})

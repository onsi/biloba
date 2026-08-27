package biloba_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

	// centerPixelOf is how a spec asserts a capture actually contains its subject.  Comparing a capture
	// against a baseline generated the same way cannot tell you that: two blank images match.
	centerPixelOf := func(img []byte) string {
		GinkgoHelper()
		decoded, _, err := image.Decode(bytes.NewReader(img))
		Ω(err).ShouldNot(HaveOccurred())
		bounds := decoded.Bounds()
		r, g, bl, _ := decoded.At(bounds.Dx()/2, bounds.Dy()/2).RGBA()
		return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, bl>>8)
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

		// "there is no baseline yet" is a different verdict from "it changed" - the response is to
		// generate baselines, not to investigate a regression - so it gets an entry rather than a
		// silence.  Recording nothing would leave a consumer unable to tell it apart from a spec that
		// asserted nothing visually.
		It("records a comparison saying the baseline is missing", func() {
			g.Eventually("#box").WithTimeout(time.Second).Should(b.HaveScreenshot("box"))
			Ω(failure).ShouldNot(BeEmpty())

			Ω(b.VisualComparisons()).Should(HaveLen(1))
			c := b.VisualComparisons()[0]
			Ω(c.MissingBaseline).Should(BeTrue())
			Ω(c.Match).Should(BeFalse())
			Ω(c.Name).Should(Equal("box"))
			Ω(c.BaselinePath).Should(Equal(baseline("box")))
			Ω(c.ActualPath).Should(Equal(artifact("box.actual")))
			Ω(c.DiffPath).Should(BeEmpty())
			// nothing was compared, so every measurement stays zero
			Ω(c.TotalPixels).Should(Equal(0))
			Ω(c.DifferingPixels).Should(Equal(0))
			Ω(c.DimensionMismatch).Should(BeFalse())
		})

		It("records the schemes it got to, when only one of them lacks a baseline", func() {
			// give light a baseline but not dark, so light compares and passes before dark stops the poll
			generate("#scheme", "scheme", b.InColorSchemes("light"))
			Ω(baseline("scheme-light")).Should(BeAnExistingFile())
			Ω(baseline("scheme-dark")).ShouldNot(BeAnExistingFile())
			g.Eventually("#scheme").WithTimeout(time.Second).Should(b.HaveScreenshot("scheme", b.InColorSchemes("light", "dark")))
			Ω(failure).Should(ContainSubstring("There is no screenshot baseline"))

			comparisons := b.VisualComparisons()
			Ω(comparisons).Should(HaveLen(2))
			Ω(comparisons[0].ColorScheme).Should(Equal("light"))
			Ω(comparisons[0].Match).Should(BeTrue())
			Ω(comparisons[0].MissingBaseline).Should(BeFalse())
			Ω(comparisons[1].ColorScheme).Should(Equal("dark"))
			Ω(comparisons[1].MissingBaseline).Should(BeTrue())
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

	// A page baseline is of the whole document by default.  ViewportOnly() makes it a baseline of the
	// fold, which is a different (and often more stable) assertion.
	Describe("ViewportOnly", func() {
		BeforeEach(func() {
			b.SetWindowSize(600, 400)
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#box").Should(b.Exist())
		})

		It("compares a baseline of the fold rather than of the whole document", func() {
			generate(b.ViewportOnly(), "fold")
			Eventually(b.ViewportOnly()).Should(b.HaveScreenshot("fold"))

			cfg, _, err := image.DecodeConfig(bytes.NewReader(readPNG(baseline("fold"))))
			Ω(err).ShouldNot(HaveOccurred())
			Ω(cfg.Height).Should(Equal(400))
		})

		It("refuses to capture an element, rather than silently ignoring the flag", func() {
			generate("#box", "box")
			g.Eventually("#box").WithTimeout(time.Second).Should(b.ViewportOnly().HaveScreenshot("box"))
			Ω(failure).Should(ContainSubstring("HaveScreenshot cannot capture an element through a ViewportOnly() view"))
		})
	})

	// b.VisualComparisons() is the structured twin of the prose diagnosis: the same measurements, as
	// data, for a reporter that wants to record the numbers rather than print the sentences.
	Describe("VisualComparisons", func() {
		BeforeEach(func() {
			b.SetWindowSize(600, 400)
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#box").Should(b.Exist())
			generate("#box", "box")
		})

		It("records nothing for a spec that asserted nothing visually - and nothing for update mode", func() {
			// generate() ran in the BeforeEach and wrote a baseline; writing a baseline is not comparing
			Ω(b.VisualComparisons()).Should(BeEmpty())
		})

		It("records a passing comparison, with the tolerances it ran under", func() {
			Eventually("#box").Should(b.HaveScreenshot("box"))

			comparisons := b.VisualComparisons()
			Ω(comparisons).Should(HaveLen(1))
			c := comparisons[0]
			Ω(c.Name).Should(Equal("box"))
			Ω(c.Label).Should(Equal("box"))
			Ω(c.ColorScheme).Should(BeEmpty())
			Ω(c.Match).Should(BeTrue())
			Ω(c.BaselinePath).Should(Equal(baseline("box")))
			Ω(c.ActualPath).Should(BeEmpty())
			Ω(c.DiffPath).Should(BeEmpty())
			Ω(c.DifferingPixels).Should(Equal(0))
			Ω(c.TotalPixels).Should(BeNumerically(">", 0))
			Ω(c.BaselineSize).Should(Equal(c.ActualSize))
			Ω(c.DimensionMismatch).Should(BeFalse())
			// the defaults, since this assertion set neither
			Ω(c.Tolerance).Should(BeNumerically(">=", 0))
			Ω(c.ChannelTolerance).Should(BeNumerically(">=", 0))
		})

		It("records what a failing comparison measured, alongside the files it wrote", func() {
			b.Run("document.getElementById('box').classList.add('spot')")
			g.Eventually("#box").WithTimeout(2 * time.Second).WithPolling(200 * time.Millisecond).Should(b.HaveScreenshot("box"))
			Ω(failure).ShouldNot(BeEmpty())

			comparisons := b.VisualComparisons()
			Ω(comparisons).Should(HaveLen(1))
			c := comparisons[0]
			Ω(c.Match).Should(BeFalse())
			Ω(c.DifferingPixels).Should(BeNumerically(">", 0))
			Ω(c.Fraction).Should(BeNumerically("~", float64(c.DifferingPixels)/float64(c.TotalPixels), 0.0001))
			Ω(c.MaxChannelDelta).Should(BeNumerically(">", 0))
			Ω(c.BaselinePath).Should(Equal(baseline("box")))
			Ω(c.ActualPath).Should(Equal(artifact("box.actual")))
			Ω(c.DiffPath).Should(Equal(artifact("box.diff")))
			// the same numbers the prose reports, which is the whole point
			Ω(failure).Should(ContainSubstring(fmt.Sprintf("of %s pixels differ", biloba.WithThousandsForTest(c.TotalPixels))))
			// a single repainted dot is one region, not scattered, and not a whole-image shift
			Ω(c.RegionCount).Should(Equal(1))
			Ω(c.Regions).Should(HaveLen(1))
			Ω(c.Regions[0].DifferingPixels).Should(BeNumerically(">", 0))
			Ω(c.Regions[0].Bounds.Dx()).Should(BeNumerically(">", 0))
			Ω(c.Scattered).Should(BeFalse())
			Ω(c.Shifted).Should(BeFalse())
		})

		It("reports a dimension mismatch as one, with no per-pixel numbers to report", func() {
			b.Run("document.getElementById('box').classList.add('taller')")
			g.Eventually("#box").WithTimeout(2 * time.Second).WithPolling(200 * time.Millisecond).Should(b.HaveScreenshot("box"))

			Ω(b.VisualComparisons()).Should(HaveLen(1))
			c := b.VisualComparisons()[0]
			Ω(c.DimensionMismatch).Should(BeTrue())
			Ω(c.Match).Should(BeFalse())
			Ω(c.ActualSize.Y).Should(BeNumerically(">", c.BaselineSize.Y))
			Ω(c.DifferingPixels).Should(Equal(0))
			Ω(c.DiffPath).Should(BeEmpty())
		})

		It("carries the tolerances the assertion actually set", func() {
			Eventually("#box").Should(b.HaveScreenshot("box", b.Tolerance(0.25), b.ChannelTolerance(9)))
			Ω(b.VisualComparisons()[0].Tolerance).Should(Equal(0.25))
			Ω(b.VisualComparisons()[0].ChannelTolerance).Should(Equal(9))
		})

		It("is cleared by Prepare", func() {
			Eventually("#box").Should(b.HaveScreenshot("box"))
			Ω(b.VisualComparisons()).Should(HaveLen(1))
			b.Prepare()
			Ω(b.VisualComparisons()).Should(BeEmpty())
		})

		It("hands back a copy, so a consumer cannot corrupt the list", func() {
			Eventually("#box").Should(b.HaveScreenshot("box"))
			comparisons := b.VisualComparisons()
			comparisons[0].Name = "clobbered"
			Ω(b.VisualComparisons()[0].Name).Should(Equal("box"))
		})

		It("is a snapshot: it rejects the poll-config knobs", func() {
			b.WithTimeout(time.Second).VisualComparisons()
			ExpectFailures(ContainSubstring("VisualComparisons does not support WithTimeout"))

			b.WithPolling(time.Second).VisualComparisons()
			ExpectFailures(ContainSubstring("VisualComparisons does not support WithPolling"))

			b.Immediate().VisualComparisons()
			ExpectFailures(ContainSubstring("VisualComparisons does not support Immediate"))
		})
	})

	// The diagnosis has two halves and they are not equals.  The words are the half everything can
	// read - an agent, a CI log, a terminal with no image support - so they are printed unconditionally;
	// the drawn diff rides along underneath only when a human is at a terminal that can render it.
	Describe("drawing the diff into the terminal", func() {
		var origInline, origTermProgram string

		BeforeEach(func() {
			origInline, origTermProgram = os.Getenv("BILOBA_INLINE_SCREENSHOTS"), os.Getenv("TERM_PROGRAM")
			os.Unsetenv("TERM_PROGRAM") // so only BILOBA_INLINE_SCREENSHOTS decides, wherever this suite runs
			DeferCleanup(func() {
				os.Setenv("BILOBA_INLINE_SCREENSHOTS", origInline)
				os.Setenv("TERM_PROGRAM", origTermProgram)
			})
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#box").Should(b.Exist())
			generate("#box", "box")
		})

		// mismatch makes #box differ from the baseline it was just given, then runs the comparison that
		// fails - leaving the rendered diagnosis in failure.
		mismatch := func(mutation string) {
			GinkgoHelper()
			b.Run("document.getElementById('box').classList.add('" + mutation + "')")
			g.Eventually("#box").WithTimeout(2 * time.Second).WithPolling(200 * time.Millisecond).Should(b.HaveScreenshot("box"))
			Ω(failure).Should(ContainSubstring(`screenshot "box" differs from baseline`))
		}

		It("draws the diff underneath the written diagnosis", func() {
			os.Setenv("BILOBA_INLINE_SCREENSHOTS", "iterm")
			mismatch("spot")
			Ω(failure).Should(SatisfyAll(
				ContainSubstring("changed region: one box,"),
				saysPath("diff:", artifact("box.diff")),
				// the image comes after the words and the paths, never instead of them
				MatchRegexp(`(?s)diff:.*\033\]1337`),
			))
		})

		It("speaks only words when the terminal cannot draw images", func() {
			os.Setenv("BILOBA_INLINE_SCREENSHOTS", "none")
			mismatch("spot")
			Ω(failure).Should(SatisfyAll(
				ContainSubstring("changed region: one box,"),
				saysPath("diff:", artifact("box.diff")),
			))
			Ω(failure).ShouldNot(ContainSubstring("\033"))
			readPNG(artifact("box.diff")) // the artifact is still written; only the drawing is skipped
		})

		It("speaks only words when an agent is driving, however capable the terminal", func() {
			os.Setenv("BILOBA_INLINE_SCREENSHOTS", "iterm")
			DeferCleanup(b.SetInlineScreenshotsForTest(false))
			mismatch("spot")
			Ω(failure).Should(saysPath("diff:", artifact("box.diff")))
			Ω(failure).ShouldNot(ContainSubstring("\033"))
		})

		It("draws nothing when there is no diff image to draw", func() {
			// a size change has no per-pixel diff to render, so there is nothing to put on the screen
			os.Setenv("BILOBA_INLINE_SCREENSHOTS", "iterm")
			mismatch("taller")
			Ω(failure).Should(MatchRegexp(`baseline is \d+x\d+, actual is \d+x\d+`))
			Ω(failure).ShouldNot(ContainSubstring("\033"))
		})
	})

	// The settle defeats anything PERIODIC - it is built to - but it cannot see a one-shot change that
	// has not started yet.  Three captures 100/140ms apart all agree while a cold font fetch is still in
	// flight, and the swap lands afterwards.  The assert path rides that out by re-capturing every poll
	// attempt; the WRITE path has no second chance, and it is the one producing the artifact everything
	// else is compared against.
	Describe("a one-shot change that lands after the settle would have finished", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual_fonts.html")
			Eventually("#hero").Should(b.Exist())
		})

		It("waits for web fonts before writing a baseline", func() {
			generate("#hero", "hero")
			// Wait for the swap before comparing.  Without this the assertion runs while the page is
			// still pre-swap and matches a pre-swap baseline - the spec would pass for the wrong reason
			// and prove nothing.  Once the swap has landed, a baseline written from the earlier frame is
			// a different SIZE and cannot match.
			Eventually("#hero.swapped").Should(b.Exist())
			Eventually("#hero").Should(b.HaveScreenshot("hero"))
		})
	})

	// A capture can only contain what the browser painted.  captureBeyondViewport expands the main
	// frame's viewport, so a subject below the document fold captures fine - but a subject scrolled out
	// of an INNER overflow:auto pane was never painted and comes back as flat pane background.  That
	// blank capture is perfectly stable, so as a baseline it would pass forever while comparing nothing.
	Describe("a subject clipped out of its own capture", func() {
		printed := func() string { return string(gt.buffer.Contents()) }

		BeforeEach(func() {
			b.SetWindowSize(600, 400)
			b.Navigate(fixtureServer + "/visual_scroller.html")
			Eventually("#pane").Should(b.Exist())
		})

		It("refuses to compare a subject that is entirely outside its scroll container", func() {
			g.Eventually("#offscreen-card").WithTimeout(300 * time.Millisecond).WithPolling(150 * time.Millisecond).Should(b.HaveScreenshot("offscreen"))
			Ω(failure).Should(SatisfyAll(
				ContainSubstring("NONE of the element matching #offscreen-card was painted"),
				ContainSubstring("div#pane"),
				ContainSubstring("b.WithinScroller"),
			))
		})

		It("refuses to WRITE a baseline for it, rather than banking a blank one", func() {
			// the assert path can retry into an element that is still scrolling in; update mode has no
			// poll to save it, so this is the one that has to stop dead
			func() {
				defer b.SetUpdateScreenshotsForTest(true)()
				g.Eventually("#offscreen-card").Should(b.HaveScreenshot("offscreen"))
			}()
			Ω(failure).Should(SatisfyAll(
				ContainSubstring("Refusing to write a screenshot baseline for offscreen - the capture would be blank"),
				ContainSubstring("NONE of the element matching #offscreen-card was painted"),
			))
			Ω(baseline("offscreen")).ShouldNot(BeAnExistingFile())
		})

		It("compares it happily once it has been scrolled into the container's band", func() {
			b.ScrollIntoView("#offscreen-card", b.WithinScroller("#pane"))
			Eventually("#offscreen-card").Should(b.BeInViewport(b.Fully()))
			generate("#offscreen-card", "offscreen")
			Eventually("#offscreen-card").Should(b.HaveScreenshot("offscreen"))
			Ω(printed()).ShouldNot(ContainSubstring("was painted"))
		})

		It("warns but still compares when the container clips only part of the subject", func() {
			generate("#straddling-card", "straddling")
			Eventually("#straddling-card").Should(b.HaveScreenshot("straddling"))
			Ω(printed()).Should(SatisfyAll(
				ContainSubstring("Only part of the element matching #straddling-card was painted"),
				ContainSubstring("div#pane"),
			))
		})

		It("says nothing about a subject that is inside its container's band", func() {
			generate("#visible-card", "visible")
			Eventually("#visible-card").Should(b.HaveScreenshot("visible"))
			Ω(printed()).ShouldNot(ContainSubstring("was painted"))
		})

		It("says nothing about an overflow:hidden ancestor that is not clipping the subject", func() {
			// a rounded card clips its corners and nothing else - reporting it would fire on half the
			// components in a real app
			generate("#rounded-card", "rounded")
			Eventually("#rounded-card").Should(b.HaveScreenshot("rounded"))
			Ω(printed()).ShouldNot(ContainSubstring("was painted"))
		})

		It("says nothing about a subject below the document fold, and captures it for real", func() {
			// The case captureBeyondViewport already handles, and the one this check must never break.
			// Assert on the PIXELS, not just on generate-then-compare: a capture that came back blank
			// would be compared against an equally blank baseline and pass while proving nothing - which
			// is the very failure this Describe is about.
			Ω(centerPixelOf(b.CaptureScreenshotOf("#below-fold"))).Should(Equal("#663399"))
			generate("#below-fold", "below-fold")
			Eventually("#below-fold").Should(b.HaveScreenshot("below-fold"))
			Ω(printed()).ShouldNot(ContainSubstring("was painted"))
		})

		It("warns rather than fails when the manual capture hits a clipped element", func() {
			// CaptureScreenshotOf is a debugging tool - it hands back the honest blank PNG with a note
			img := b.CaptureScreenshotOf("#offscreen-card")
			Ω(img).ShouldNot(BeEmpty())
			Ω(printed()).Should(ContainSubstring("NONE of the element matching #offscreen-card was painted"))
		})
	})

	// Reaching content outside the viewport means expanding the viewport, and a responsive page
	// OBSERVES that: matchMedia flips, resize fires, and an app that renders off its breakpoint can
	// unmount the subtree being captured - so the capture destroys its own subject and the spec's NEXT
	// assertion polls for an element that no longer exists.  A subject already in view needs none of
	// that expansion, and the fixture counts what the page was told.
	Describe("what a capture does to the page it is capturing", func() {
		// expectUndisturbed asserts the page was never told its viewport moved.  mediaFlips is the one
		// that matters most: a resize handler is opt-in, but an app reading its breakpoint through
		// matchMedia re-renders off exactly this.
		expectUndisturbed := func() {
			GinkgoHelper()
			var resizes, mediaFlips int
			b.Run("window.resizeEvents", &resizes)
			b.Run("window.mediaFlips", &mediaFlips)
			Ω(resizes).Should(Equal(0), "the page observed a resize during a capture")
			Ω(mediaFlips).Should(Equal(0), "the page's media queries flipped during a capture")
		}
		resizeCount := func() int {
			GinkgoHelper()
			var resizes int
			b.Run("window.resizeEvents", &resizes)
			return resizes
		}

		BeforeEach(func() {
			b.SetWindowSize(600, 400)
			b.Navigate(fixtureServer + "/visual_resize.html")
			Eventually("#box").Should(b.Exist())
		})

		It("leaves the viewport alone when the subject is already in view", func() {
			b.Run("resetCaptureProbe()")
			b.CaptureScreenshotOf("#box")
			expectUndisturbed()

			b.Run("resetCaptureProbe()")
			generate("#box", "box")
			Eventually("#box").Should(b.HaveScreenshot("box"))
			expectUndisturbed()
		})

		It("leaves the viewport alone for a full-page capture of a document that fits it", func() {
			// the app-shell shape: the document never scrolls because an inner pane does
			b.Run("document.getElementById('tall').remove(); document.documentElement.style.overflow='hidden'; document.body.style.cssText='margin:0;padding:0;width:600px;height:400px;overflow:hidden'")
			b.Run("resetCaptureProbe()")
			b.CaptureScreenshot()
			expectUndisturbed()

			b.Run("resetCaptureProbe()")
			generate(b, "page")
			Eventually(b).Should(b.HaveScreenshot("page"))
			expectUndisturbed()
		})

		It("still reaches a subject below the fold, which is what the expansion is for", func() {
			// the honest cost: a subject outside the viewport cannot be captured without expanding it.
			// The pixels are what matter here - this is the guarantee the fix must not trade away.
			b.Run("resetCaptureProbe()")
			Ω(centerPixelOf(b.CaptureScreenshotOf("#tall"))).ShouldNot(BeEmpty())
			Ω(resizeCount()).Should(BeNumerically(">", 0), "a below-the-fold capture legitimately expands the viewport")
		})

		It("says so when the capture removed its own subject", func() {
			// the diagnosis for the residual case: a page that unmounts on a breakpoint change, with a
			// subject outside the viewport, so the expansion is unavoidable
			b.Run("armSelfDestruct()")
			b.CaptureScreenshotOf("#doomed")
			Ω(string(gt.buffer.Contents())).Should(SatisfyAll(
				ContainSubstring("was present before this capture and gone after it"),
				ContainSubstring("b.BeInViewport(b.Fully())"),
			))
		})
	})

	Describe("two colour schemes that render identically", func() {
		printed := func() string { return string(gt.buffer.Contents()) }

		BeforeEach(func() {
			b.Navigate(fixtureServer + "/visual.html")
			Eventually("#scheme").Should(b.Exist())
		})

		It("warns: both baselines pass, but only one rendering was ever exercised", func() {
			// #box ignores prefers-color-scheme entirely, so light and dark capture the same pixels -
			// which is what a suite with a pinned theme override looks like from in here
			generate("#box", "box", b.InColorSchemes("light", "dark"))
			Ω(printed()).Should(SatisfyAll(
				ContainSubstring(`"box" captured byte-identical images under prefers-color-scheme`),
				ContainSubstring("only one rendering was ever exercised"),
			))
		})

		It("stays quiet when the two schemes really do render differently", func() {
			generate("#scheme", "scheme", b.InColorSchemes("light", "dark"))
			Ω(printed()).ShouldNot(ContainSubstring("byte-identical"))
		})

		It("warns once per assertion, however many times it is evaluated", func() {
			// the warning diagnoses the SETUP, so it reads the same on every evaluation - said once per
			// attempt it would bury whatever the assertion is actually reporting
			generate("#box", "box", b.InColorSchemes("light", "dark"))
			gt.buffer.Clear()
			matcher := b.HaveScreenshot("box", b.InColorSchemes("light", "dark"))
			Eventually("#box").Should(matcher)
			Eventually("#box").Should(matcher)
			Ω(strings.Count(printed(), "byte-identical")).Should(Equal(1))
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

		It("records one VisualComparison per scheme, each labelled with its scheme", func() {
			generate("#scheme", "scheme", b.InColorSchemes("light", "dark"))
			Eventually("#scheme").Should(b.HaveScreenshot("scheme", b.InColorSchemes("light", "dark")))

			comparisons := b.VisualComparisons()
			Ω(comparisons).Should(HaveLen(2))
			schemes, labels := []string{}, []string{}
			for _, c := range comparisons {
				schemes = append(schemes, c.ColorScheme)
				labels = append(labels, c.Label)
				Ω(c.Name).Should(Equal("scheme"))
				Ω(c.Match).Should(BeTrue())
			}
			Ω(schemes).Should(ConsistOf("light", "dark"))
			Ω(labels).Should(ConsistOf("scheme (prefers-color-scheme: light)", "scheme (prefers-color-scheme: dark)"))
		})

		// The bug this pins: matchScheme used to record a PASSING scheme inline, at the moment it
		// measured it, while the failing scheme was recorded later from FailureMessage.  So a polling
		// two-scheme assertion re-recorded its passing scheme on every tick - one real light+dark
		// assertion polling ~3s produced 63 identical "light" entries and one "dark".  Measuring and
		// recording answer to different clocks; the attempt buffer is what keeps them apart.
		It("records one entry per scheme even when the assertion polls to its deadline", func() {
			generate("#scheme", "scheme", b.InColorSchemes("light", "dark"))
			// light still matches its baseline; dark no longer does, so this polls until it times out
			b.Run(`document.documentElement.style.setProperty("--dark-bg", "#004488")`)
			g.Eventually("#scheme").WithTimeout(time.Second).WithPolling(50 * time.Millisecond).
				Should(b.HaveScreenshot("scheme", b.InColorSchemes("light", "dark")))
			Ω(failure).ShouldNot(BeEmpty())

			comparisons := b.VisualComparisons()
			byScheme := map[string][]biloba.VisualComparison{}
			for _, c := range comparisons {
				byScheme[c.ColorScheme] = append(byScheme[c.ColorScheme], c)
			}
			// exactly one per scheme - not one per poll attempt
			Ω(comparisons).Should(HaveLen(2), "got %d comparisons: %v", len(comparisons), byScheme)
			Ω(byScheme["light"]).Should(HaveLen(1))
			Ω(byScheme["dark"]).Should(HaveLen(1))
			Ω(byScheme["light"][0].Match).Should(BeTrue())
			Ω(byScheme["dark"][0].Match).Should(BeFalse())
			// the failing scheme's entry carries the artifact paths FailureMessage wrote
			Ω(byScheme["dark"][0].ActualPath).Should(Equal(artifact("scheme-dark.actual")))
			Ω(byScheme["dark"][0].DiffPath).Should(Equal(artifact("scheme-dark.diff")))
			Ω(byScheme["light"][0].ActualPath).Should(BeEmpty())
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

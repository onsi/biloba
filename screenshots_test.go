package biloba_test

import (
	"bytes"
	"image"
	"image/color"
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

var _ = Describe("Screenshots", func() {
	Describe("it can take screenshots", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/screenshots.html")
			Eventually(`body`).Should(b.Exist())
		})

		It("can take screenshots", func() {
			b.SetWindowSize(50, 40)
			data := b.CaptureScreenshot()
			img, _, err := image.Decode(bytes.NewBuffer(data))
			Ω(err).ShouldNot(HaveOccurred())

			Ω(img.Bounds().Max.X).Should(Equal(50))
			Ω(img.Bounds().Max.Y).Should(Equal(40))
			Ω(img.At(10, 5)).Should(Equal(color.RGBA{0, 0, 255, 255}))
		})
	})

	Describe("CaptureScreenshotToFile", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/screenshots.html")
			Eventually(`body`).Should(b.Exist())
		})

		It("writes a valid PNG file and returns its absolute path", func() {
			dir := GinkgoT().TempDir()
			path := filepath.Join(dir, "shot.png")
			returned := b.CaptureScreenshotToFile(path)

			Ω(returned).Should(Equal(path))

			raw, err := os.ReadFile(returned)
			Ω(err).ShouldNot(HaveOccurred())
			img, _, err := image.Decode(bytes.NewBuffer(raw))
			Ω(err).ShouldNot(HaveOccurred())
			Ω(img.Bounds().Dx()).Should(BeNumerically(">", 0))
		})

		It("creates missing intermediate directories", func() {
			dir := GinkgoT().TempDir()
			path := filepath.Join(dir, "nested", "dir", "shot.png")
			returned := b.CaptureScreenshotToFile(path)

			Ω(returned).Should(Equal(path))
			Ω(returned).Should(BeAnExistingFile())
		})

		It("prints the path to test output", func() {
			dir := GinkgoT().TempDir()
			path := filepath.Join(dir, "shot.png")
			b.CaptureScreenshotToFile(path)

			Ω(gt.buffer).Should(gbytes.Say("Screenshot written to:"))
			Ω(gt.buffer).Should(gbytes.Say(regexp.QuoteMeta(path)))
		})
	})

	Describe("inlineImagesSupported", Label("no-browser"), func() {
		// detection reads several terminal environment variables; save and clear the
		// full set so each spec controls exactly the inputs it sets.
		detectionEnvKeys := []string{
			"TERM", "TERM_PROGRAM", "KITTY_WINDOW_ID", "LC_TERMINAL", "KONSOLE_VERSION",
			"BILOBA_INLINE_SCREENSHOTS", "BILOBA_PROBE_TERMINAL",
		}
		var origEnv map[string]string

		BeforeEach(func() {
			origEnv = map[string]string{}
			for _, k := range detectionEnvKeys {
				origEnv[k] = os.Getenv(k)
				os.Unsetenv(k)
			}
		})

		AfterEach(func() {
			for _, k := range detectionEnvKeys {
				if origEnv[k] == "" {
					os.Unsetenv(k)
				} else {
					os.Setenv(k, origEnv[k])
				}
			}
		})

		It("returns true when TERM_PROGRAM=iTerm.app", func() {
			os.Setenv("TERM_PROGRAM", "iTerm.app")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeTrue())
		})

		It("returns true when TERM_PROGRAM=vscode", func() {
			os.Setenv("TERM_PROGRAM", "vscode")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeTrue())
		})

		It("returns true when TERM_PROGRAM=WezTerm", func() {
			os.Setenv("TERM_PROGRAM", "WezTerm")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeTrue())
		})

		It("returns true when TERM_PROGRAM=ghostty (kitty protocol)", func() {
			os.Setenv("TERM_PROGRAM", "ghostty")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeTrue())
		})

		It("returns true when KITTY_WINDOW_ID is set", func() {
			os.Setenv("KITTY_WINDOW_ID", "1")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeTrue())
		})

		It("returns true when LC_TERMINAL=iTerm2 (e.g. forwarded over ssh)", func() {
			os.Setenv("LC_TERMINAL", "iTerm2")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeTrue())
		})

		It("returns false for an unknown TERM_PROGRAM", func() {
			os.Setenv("TERM_PROGRAM", "xterm")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeFalse())
		})

		It("returns false when no terminal env vars are set", func() {
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeFalse())
		})

		It("returns false when BILOBA_INLINE_SCREENSHOTS=none regardless of TERM_PROGRAM", func() {
			os.Setenv("TERM_PROGRAM", "iTerm.app")
			os.Setenv("BILOBA_INLINE_SCREENSHOTS", "none")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeFalse())
		})

		It("returns true when BILOBA_INLINE_SCREENSHOTS=iterm regardless of TERM_PROGRAM", func() {
			os.Setenv("BILOBA_INLINE_SCREENSHOTS", "iterm")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeTrue())
		})

		It("returns true when BILOBA_INLINE_SCREENSHOTS=sixel", func() {
			os.Setenv("BILOBA_INLINE_SCREENSHOTS", "sixel")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeTrue())
		})

		It("falls back to terminal auto-detection for an unrecognized BILOBA_INLINE_SCREENSHOTS value", func() {
			os.Setenv("TERM_PROGRAM", "iTerm.app")
			os.Setenv("BILOBA_INLINE_SCREENSHOTS", "bogus")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeTrue())
		})
	})

	Describe("BilobaConfigInlineScreenshots", Label("no-browser"), func() {
		var origTermProgram, origInline string
		var restoreDetector func()

		BeforeEach(func() {
			origTermProgram = os.Getenv("TERM_PROGRAM")
			origInline = os.Getenv("BILOBA_INLINE_SCREENSHOTS")
			// keep these specs in interactive mode so automation doesn't pre-disable inline
			restoreDetector = biloba.SetAutomationDetectedForTest(func() bool { return false })
		})

		AfterEach(func() {
			os.Setenv("TERM_PROGRAM", origTermProgram)
			os.Setenv("BILOBA_INLINE_SCREENSHOTS", origInline)
			restoreDetector()
		})

		It("disables inline screenshots even when iTerm2 is detected", func() {
			os.Setenv("TERM_PROGRAM", "iTerm.app")
			os.Unsetenv("BILOBA_INLINE_SCREENSHOTS")
			bDisabled := biloba.ConnectToChrome(gt, biloba.BilobaConfigInlineScreenshots(false))
			Ω(bDisabled.InlineScreenshotsEnabledForTest()).Should(BeFalse())
		})

		It("still uses inline screenshots when not configured and iTerm2 is detected", func() {
			os.Setenv("TERM_PROGRAM", "iTerm.app")
			os.Unsetenv("BILOBA_INLINE_SCREENSHOTS")
			bEnabled := biloba.ConnectToChrome(gt)
			Ω(bEnabled.InlineScreenshotsEnabledForTest()).Should(BeTrue())
		})
	})

	Describe("imgcat suppression in safeAllTabScreenshots", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/screenshots.html")
			Eventually(`body`).Should(b.Exist())
		})

		var origInline, origTermProgram string
		BeforeEach(func() {
			origTermProgram = os.Getenv("TERM_PROGRAM")
			origInline = os.Getenv("BILOBA_INLINE_SCREENSHOTS")
			os.Unsetenv("TERM_PROGRAM")
		})
		AfterEach(func() {
			os.Setenv("TERM_PROGRAM", origTermProgram)
			os.Setenv("BILOBA_INLINE_SCREENSHOTS", origInline)
		})

		Context("when BILOBA_INLINE_SCREENSHOTS=none", func() {
			BeforeEach(func() { os.Setenv("BILOBA_INLINE_SCREENSHOTS", "none") })

			It("does not include an inline blob in the screenshot", func() {
				shots := b.SafeAllTabScreenshotsForTest(0, 0)
				Ω(shots).ShouldNot(BeEmpty())
				Ω(shots[0].ImgcatScreenshot).Should(BeEmpty())
			})
		})

		Context("when BILOBA_INLINE_SCREENSHOTS=iterm", func() {
			BeforeEach(func() { os.Setenv("BILOBA_INLINE_SCREENSHOTS", "iterm") })

			It("includes an iTerm2 blob in the screenshot", func() {
				shots := b.SafeAllTabScreenshotsForTest(0, 0)
				Ω(shots).ShouldNot(BeEmpty())
				Ω(shots[0].ImgcatScreenshot).ShouldNot(BeEmpty())
				Ω(shots[0].ImgcatScreenshot).Should(HavePrefix("\033]1337"))
			})
		})

		Context("when BILOBA_INLINE_SCREENSHOTS=kitty", func() {
			BeforeEach(func() { os.Setenv("BILOBA_INLINE_SCREENSHOTS", "kitty") })

			It("emits a kitty graphics sequence", func() {
				shots := b.SafeAllTabScreenshotsForTest(0, 0)
				Ω(shots).ShouldNot(BeEmpty())
				Ω(shots[0].ImgcatScreenshot).Should(HavePrefix("\033_G"))
			})
		})

		Context("when BILOBA_INLINE_SCREENSHOTS=sixel", func() {
			BeforeEach(func() { os.Setenv("BILOBA_INLINE_SCREENSHOTS", "sixel") })

			It("emits a sixel sequence", func() {
				shots := b.SafeAllTabScreenshotsForTest(0, 0)
				Ω(shots).ShouldNot(BeEmpty())
				Ω(shots[0].ImgcatScreenshot).Should(HavePrefix("\033P"))
			})
		})
	})

	Describe("sanitizeForFilename", Label("no-browser"), func() {
		It("replaces non-filename characters with underscores and collapses them", func() {
			Ω(biloba.SanitizeForFilenameForTest("My Suite/some spec")).Should(Equal("My_Suite_some_spec"))
			Ω(biloba.SanitizeForFilenameForTest("  leading  ")).Should(Equal("leading"))
			Ω(biloba.SanitizeForFilenameForTest("a b  c")).Should(Equal("a_b_c"))
			Ω(biloba.SanitizeForFilenameForTest(strings.Repeat("x", 100))).Should(HaveLen(80))
			Ω(biloba.SanitizeForFilenameForTest("valid-name_v1.0")).Should(Equal("valid-name_v1.0"))
		})
	})

	Describe("BilobaConfigScreenshotsToDir", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/screenshots.html")
			Eventually(`body`).Should(b.Exist())
		})

		It("writes PNG files to the configured directory when safeAllTabScreenshots is called", func() {
			dir := GinkgoT().TempDir()
			bWithDir := biloba.ConnectToChrome(gt, biloba.BilobaConfigScreenshotsToDir(dir))

			bWithDir.Navigate(fixtureServer + "/screenshots.html")
			Eventually(`body`).Should(bWithDir.Exist())

			// Under heavy parallel load a single screenshot capture can exceed
			// safeAllTabScreenshots' internal 1s per-tab timeout; retry until the
			// capture succeeds, then assert on the file it wrote.
			// A single capture pass internally caps at ~1s per tab, so give the
			// retry a budget well beyond Gomega's 1s default to absorb contention.
			var shots []biloba.TabScreenshotForTest
			Eventually(func() string {
				shots = bWithDir.SafeAllTabScreenshotsForTest(0, 0)
				if len(shots) == 0 {
					return "no screenshots returned"
				}
				return shots[0].Failure
			}).WithTimeout(30 * time.Second).WithPolling(time.Second).Should(BeEmpty())

			Ω(shots[0].FilePath).ShouldNot(BeEmpty())

			// The file must exist and be a valid PNG.
			raw, err := os.ReadFile(shots[0].FilePath)
			Ω(err).ShouldNot(HaveOccurred())
			_, _, err = image.Decode(bytes.NewBuffer(raw))
			Ω(err).ShouldNot(HaveOccurred())

			// The filename must be inside the configured dir.
			Ω(shots[0].FilePath).Should(HavePrefix(dir))
			// The filename must start with "screenshot-" and end with ".png".
			Ω(filepath.Base(shots[0].FilePath)).Should(HavePrefix("screenshot-"))
			Ω(filepath.Base(shots[0].FilePath)).Should(HaveSuffix(".png"))
		})
	})

	Describe("on-failure artifact configuration", func() {
		// Pin the detector per-spec so these assertions don't depend on whether the suite itself
		// happens to be running under CI or an AI agent.  Default each spec to interactive ("human")
		// mode; automation specs flip it on locally.
		var restoreDetector func()
		var origScreenshotsDir string
		var hadScreenshotsDir bool
		BeforeEach(func() {
			restoreDetector = biloba.SetAutomationDetectedForTest(func() bool { return false })
			origScreenshotsDir, hadScreenshotsDir = os.LookupEnv("BILOBA_SCREENSHOTS_DIR")
			os.Unsetenv("BILOBA_SCREENSHOTS_DIR")
		})
		AfterEach(func() {
			restoreDetector()
			if hadScreenshotsDir {
				os.Setenv("BILOBA_SCREENSHOTS_DIR", origScreenshotsDir)
			} else {
				os.Unsetenv("BILOBA_SCREENSHOTS_DIR")
			}
		})

		Context("the interactive (human) default", func() {
			It("keeps inline screenshots on, outlines off, and writes nothing to disk", func() {
				bWith := biloba.ConnectToChrome(gt)
				outlines, screenshots, inline, dir := bWith.FailureArtifactConfigForTest()
				Ω(outlines).Should(BeFalse())
				Ω(screenshots).Should(BeTrue())
				Ω(inline).Should(BeTrue())
				Ω(dir).Should(BeEmpty())
			})

			It("still writes screenshots to disk when BILOBA_SCREENSHOTS_DIR is set", func() {
				dir := GinkgoT().TempDir()
				os.Setenv("BILOBA_SCREENSHOTS_DIR", dir)
				bWith := biloba.ConnectToChrome(gt)
				outlines, _, inline, gotDir := bWith.FailureArtifactConfigForTest()
				Ω(gotDir).Should(Equal(dir))
				Ω(outlines).Should(BeFalse()) // env dir alone does not flip the rest of the policy
				Ω(inline).Should(BeTrue())
			})
		})

		Context("the automation (CI / AI agent) default", func() {
			BeforeEach(func() {
				restoreDetector()
				restoreDetector = biloba.SetAutomationDetectedForTest(func() bool { return true })
			})

			It("turns outlines on, inline blobs off, and writes screenshots to the default dir", func() {
				bWith := biloba.ConnectToChrome(gt)
				outlines, screenshots, inline, dir := bWith.FailureArtifactConfigForTest()
				Ω(outlines).Should(BeTrue())
				Ω(inline).Should(BeFalse())
				Ω(screenshots).Should(BeTrue()) // screenshots stay on, just written to files
				Ω(dir).Should(Equal(biloba.DefaultAutomationScreenshotsDirForTest()))
			})

			It("honors BILOBA_SCREENSHOTS_DIR for the screenshots location", func() {
				dir := GinkgoT().TempDir()
				os.Setenv("BILOBA_SCREENSHOTS_DIR", dir)
				bWith := biloba.ConnectToChrome(gt)
				_, _, _, gotDir := bWith.FailureArtifactConfigForTest()
				Ω(gotDir).Should(Equal(dir))
			})
		})

		Context("explicit suite configuration always wins (per knob)", func() {
			It("keeps an explicit screenshots dir even under automation", func() {
				restoreDetector()
				restoreDetector = biloba.SetAutomationDetectedForTest(func() bool { return true })

				suiteDir := GinkgoT().TempDir()
				bWith := biloba.ConnectToChrome(gt, biloba.BilobaConfigScreenshotsToDir(suiteDir))
				outlines, _, inline, gotDir := bWith.FailureArtifactConfigForTest()
				Ω(gotDir).Should(Equal(suiteDir)) // explicit dir wins over the automation default...
				Ω(outlines).Should(BeTrue())      // ...but the other automation knobs still apply
				Ω(inline).Should(BeFalse())
			})

			It("lets a human suite force outlines on without affecting screenshots", func() {
				bWith := biloba.ConnectToChrome(gt, biloba.BilobaConfigFailureOutlines())
				outlines, _, inline, dir := bWith.FailureArtifactConfigForTest()
				Ω(outlines).Should(BeTrue())
				Ω(inline).Should(BeTrue()) // inline still on - only outlines were opted into
				Ω(dir).Should(BeEmpty())
			})

			It("lets a suite force outlines OFF and inline ON under automation", func() {
				restoreDetector()
				restoreDetector = biloba.SetAutomationDetectedForTest(func() bool { return true })

				bWith := biloba.ConnectToChrome(gt,
					biloba.BilobaConfigFailureOutlines(false),
					biloba.BilobaConfigInlineScreenshots(true),
				)
				outlines, _, inline, _ := bWith.FailureArtifactConfigForTest()
				Ω(outlines).Should(BeFalse()) // explicit false beats automation's auto-on
				Ω(inline).Should(BeTrue())    // explicit true beats automation's auto-off
			})
		})
	})
})

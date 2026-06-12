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
		var origTermProgram, origNoImgcat, origImgcat string

		BeforeEach(func() {
			origTermProgram = os.Getenv("TERM_PROGRAM")
			origNoImgcat = os.Getenv("BILOBA_NO_IMGCAT")
			origImgcat = os.Getenv("BILOBA_IMGCAT")
		})

		AfterEach(func() {
			os.Setenv("TERM_PROGRAM", origTermProgram)
			os.Setenv("BILOBA_NO_IMGCAT", origNoImgcat)
			os.Setenv("BILOBA_IMGCAT", origImgcat)
		})

		It("returns true when TERM_PROGRAM=iTerm.app", func() {
			os.Setenv("TERM_PROGRAM", "iTerm.app")
			os.Unsetenv("BILOBA_NO_IMGCAT")
			os.Unsetenv("BILOBA_IMGCAT")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeTrue())
		})

		It("returns false for an unknown TERM_PROGRAM", func() {
			os.Setenv("TERM_PROGRAM", "xterm")
			os.Unsetenv("BILOBA_NO_IMGCAT")
			os.Unsetenv("BILOBA_IMGCAT")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeFalse())
		})

		It("returns false when TERM_PROGRAM is unset", func() {
			os.Unsetenv("TERM_PROGRAM")
			os.Unsetenv("BILOBA_NO_IMGCAT")
			os.Unsetenv("BILOBA_IMGCAT")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeFalse())
		})

		It("returns false when BILOBA_NO_IMGCAT=true regardless of TERM_PROGRAM", func() {
			os.Setenv("TERM_PROGRAM", "iTerm.app")
			os.Setenv("BILOBA_NO_IMGCAT", "true")
			os.Unsetenv("BILOBA_IMGCAT")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeFalse())
		})

		It("returns true when BILOBA_IMGCAT=true regardless of TERM_PROGRAM", func() {
			os.Unsetenv("TERM_PROGRAM")
			os.Unsetenv("BILOBA_NO_IMGCAT")
			os.Setenv("BILOBA_IMGCAT", "true")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeTrue())
		})

		It("BILOBA_NO_IMGCAT=true wins over BILOBA_IMGCAT=true", func() {
			os.Setenv("BILOBA_NO_IMGCAT", "true")
			os.Setenv("BILOBA_IMGCAT", "true")
			Ω(biloba.InlineImagesSupportedForTest()).Should(BeFalse())
		})
	})

	Describe("BilobaConfigDisableInlineScreenshots", Label("no-browser"), func() {
		var origTermProgram, origNoImgcat, origImgcat string

		BeforeEach(func() {
			origTermProgram = os.Getenv("TERM_PROGRAM")
			origNoImgcat = os.Getenv("BILOBA_NO_IMGCAT")
			origImgcat = os.Getenv("BILOBA_IMGCAT")
		})

		AfterEach(func() {
			os.Setenv("TERM_PROGRAM", origTermProgram)
			os.Setenv("BILOBA_NO_IMGCAT", origNoImgcat)
			os.Setenv("BILOBA_IMGCAT", origImgcat)
		})

		It("disables inline screenshots even when iTerm2 is detected", func() {
			os.Setenv("TERM_PROGRAM", "iTerm.app")
			os.Unsetenv("BILOBA_NO_IMGCAT")
			os.Unsetenv("BILOBA_IMGCAT")
			bDisabled := biloba.ConnectToChrome(gt, biloba.BilobaConfigDisableInlineScreenshots())
			Ω(bDisabled.InlineScreenshotsEnabledForTest()).Should(BeFalse())
		})

		It("still uses inline screenshots when not configured and iTerm2 is detected", func() {
			os.Setenv("TERM_PROGRAM", "iTerm.app")
			os.Unsetenv("BILOBA_NO_IMGCAT")
			os.Unsetenv("BILOBA_IMGCAT")
			bEnabled := biloba.ConnectToChrome(gt)
			Ω(bEnabled.InlineScreenshotsEnabledForTest()).Should(BeTrue())
		})
	})

	Describe("imgcat suppression in safeAllTabScreenshots", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/screenshots.html")
			Eventually(`body`).Should(b.Exist())
		})

		Context("when BILOBA_NO_IMGCAT=true", func() {
			var origNoImgcat, origImgcat, origTermProgram string

			BeforeEach(func() {
				origTermProgram = os.Getenv("TERM_PROGRAM")
				origNoImgcat = os.Getenv("BILOBA_NO_IMGCAT")
				origImgcat = os.Getenv("BILOBA_IMGCAT")
				os.Setenv("BILOBA_NO_IMGCAT", "true")
				os.Unsetenv("BILOBA_IMGCAT")
				os.Unsetenv("TERM_PROGRAM")
			})

			AfterEach(func() {
				os.Setenv("TERM_PROGRAM", origTermProgram)
				os.Setenv("BILOBA_NO_IMGCAT", origNoImgcat)
				os.Setenv("BILOBA_IMGCAT", origImgcat)
			})

			It("does not include an imgcat blob in the screenshot", func() {
				shots := b.SafeAllTabScreenshotsForTest(0, 0)
				Ω(shots).ShouldNot(BeEmpty())
				Ω(shots[0].ImgcatScreenshot).Should(BeEmpty())
			})
		})

		Context("when BILOBA_IMGCAT=true", func() {
			var origNoImgcat, origImgcat, origTermProgram string

			BeforeEach(func() {
				origTermProgram = os.Getenv("TERM_PROGRAM")
				origNoImgcat = os.Getenv("BILOBA_NO_IMGCAT")
				origImgcat = os.Getenv("BILOBA_IMGCAT")
				os.Setenv("BILOBA_IMGCAT", "true")
				os.Unsetenv("BILOBA_NO_IMGCAT")
				os.Unsetenv("TERM_PROGRAM")
			})

			AfterEach(func() {
				os.Setenv("TERM_PROGRAM", origTermProgram)
				os.Setenv("BILOBA_NO_IMGCAT", origNoImgcat)
				os.Setenv("BILOBA_IMGCAT", origImgcat)
			})

			It("includes an imgcat blob in the screenshot", func() {
				shots := b.SafeAllTabScreenshotsForTest(0, 0)
				Ω(shots).ShouldNot(BeEmpty())
				Ω(shots[0].ImgcatScreenshot).ShouldNot(BeEmpty())
				Ω(shots[0].ImgcatScreenshot).Should(HavePrefix("\033]1337"))
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
})

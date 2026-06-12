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

			shots := bWithDir.SafeAllTabScreenshotsForTest(0, 0)
			Ω(shots).ShouldNot(BeEmpty())
			Ω(shots[0].Failure).Should(BeEmpty())
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

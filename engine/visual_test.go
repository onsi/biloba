package engine_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("runner-neutral screenshots", func() {
	It("captures page and element pixels and bounds the returned binary", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL)).To(Succeed())

		pageShot, err := session.CapturePageScreenshot(ctx, engine.ScreenshotCaptureOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(bytes.HasPrefix(pageShot.PNG, []byte("\x89PNG\r\n\x1a\n"))).To(BeTrue())
		Expect(pageShot.Width).To(BeNumerically(">", 100))
		Expect(pageShot.Height).To(BeNumerically(">", 100))

		elementShot, err := session.CaptureElementScreenshot(ctx, engine.TestID("drag-source"), engine.ScreenshotCaptureOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(elementShot.Width).To(BeNumerically("~", 40, 1))
		Expect(elementShot.Height).To(BeNumerically("~", 40, 1))
		Expect(pixelAt(elementShot.PNG, 10, 10)).To(Equal(color.NRGBA{R: 255, A: 255}))

		_, err = session.CapturePageScreenshot(ctx, engine.ScreenshotCaptureOptions{MaxBytes: 16})
		Expect(err).To(MatchError(ContainSubstring("exceeds the 16-byte limit")))
	})

	It("masks observable pixels and restores color-scheme emulation", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,"+`<style>body{margin:0}.box{width:40px;height:40px;background:white}@media(prefers-color-scheme:dark){.box{background:black}}</style><div class=box><i style="display:block;width:10px;height:10px;background:red"></i></div>`)).To(Succeed())
		before, err := session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())

		shot, err := session.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{
			ColorScheme: "dark",
			Masks:       []engine.Selector{engine.CSS("i")},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(pixelAt(shot.PNG, 5, 5)).To(Equal(color.NRGBA{R: 128, G: 128, B: 128, A: 255}))
		Expect(pixelAt(shot.PNG, 20, 20)).To(Equal(color.NRGBA{A: 255}))
		after, err := session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(Equal(before))
	})

	It("freezes rendering by default and permits animated capture only when requested", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,"+`<style>body{margin:0}.box{width:20px;height:20px;background:red;animation:shade 100000s linear infinite;animation-delay:-50000s}@keyframes shade{from{background:red}to{background:blue}}</style><div class=box></div>`)).To(Succeed())

		frozen, err := session.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{})
		Expect(err).NotTo(HaveOccurred())
		animated, err := session.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{Animated: true})
		Expect(err).NotTo(HaveOccurred())

		Expect(pixelAt(frozen.PNG, 10, 10)).To(Equal(color.NRGBA{R: 255, A: 255}))
		Expect(pixelAt(animated.PNG, 10, 10)).NotTo(Equal(pixelAt(frozen.PNG, 10, 10)))
	})

	It("refuses an element capture that an overflow ancestor did not paint", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<div style='width:50px;height:50px;overflow:hidden'><div class=box style='margin-top:100px;width:20px;height:20px;background:red'></div></div>")).To(Succeed())

		_, err = session.CaptureElementScreenshot(ctx, engine.CSS(".box"), engine.ScreenshotCaptureOptions{})

		Expect(err).To(MatchError(ContainSubstring("not painted because it is clipped")))
	})
})

var _ = Describe("runner-neutral visual comparison", func() {
	It("validates baseline names without allowing escape from the baseline directory", func() {
		path, err := engine.ScreenshotBaselinePath("checkout/receipt", "dark")
		Expect(err).NotTo(HaveOccurred())
		Expect(path).To(Equal(filepath.Join("checkout", "receipt-dark.png")))

		for _, name := range []string{"", "../receipt", "checkout/../receipt", "/tmp/receipt"} {
			_, err := engine.ScreenshotBaselinePath(name, "")
			Expect(err).To(HaveOccurred(), name)
		}
	})

	It("fails on a missing baseline while preserving the captured actual", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<style>body{margin:0;background:rgb(2,4,8)}</style>missing")).To(Succeed())
		root := GinkgoT().TempDir()

		result, err := session.CompareScreenshot(ctx, "pages/missing", engine.PageScreenshotTarget(), engine.VisualOptions{
			BaselineDir: filepath.Join(root, "baselines"),
			ArtifactDir: filepath.Join(root, "artifacts"),
		})

		Expect(engine.IsFatal(err)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("no screenshot baseline")))
		Expect(result.Schemes).To(HaveLen(1))
		Expect(result.Schemes[0].ActualPath).To(BeAnExistingFile())
		Expect(result.Schemes[0].DiffPath).To(BeEmpty())
		Expect(filepath.Base(result.Schemes[0].ActualPath)).To(Equal("pages_missing.actual.png"))
	})

	It("settles before update mode writes, then compares within pixel and channel tolerances", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,<style>body{margin:0}.box{width:10px;height:10px;background:red}</style><div class=box></div><script>setTimeout(()=>document.querySelector('.box').style.background='rgb(10,20,30)',5)</script>")).To(Succeed())
		root := GinkgoT().TempDir()
		opts := engine.VisualOptions{
			BaselineDir: filepath.Join(root, "baselines"), ArtifactDir: filepath.Join(root, "artifacts"),
			Update: true, SettleAttempts: 4, SettleStreak: 2, SettleInterval: 10 * time.Millisecond,
		}
		updated, err := session.CompareScreenshot(ctx, "box", engine.ElementScreenshotTarget(engine.CSS(".box")), opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(updated.Match).To(BeTrue())
		Expect(updated.Updated).To(BeTrue())
		Expect(updated.Schemes[0].BaselinePath).To(BeAnExistingFile())
		baseline, err := os.ReadFile(updated.Schemes[0].BaselinePath)
		Expect(err).NotTo(HaveOccurred())
		Expect(pixelAt(baseline, 5, 5)).To(Equal(color.NRGBA{R: 10, G: 20, B: 30, A: 255}))

		_, err = session.Evaluate(ctx, `document.querySelector('.box').style.background='rgb(14,20,30)'`)
		Expect(err).NotTo(HaveOccurred())
		opts.Update = false
		opts.Tolerance = engine.ScreenshotTolerance{ChannelDelta: 4}
		matched, err := session.CompareScreenshot(ctx, "box", engine.ElementScreenshotTarget(engine.CSS(".box")), opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(matched.Match).To(BeTrue())

		opts.Tolerance = engine.ScreenshotTolerance{ChannelDelta: 3, PixelFraction: 0.01}
		different, err := session.CompareScreenshot(ctx, "box", engine.ElementScreenshotTarget(engine.CSS(".box")), opts)
		Expect(err).NotTo(HaveOccurred())
		Expect(different.Match).To(BeFalse())
		Expect(different.Schemes[0].Diff.DifferingPixels).To(Equal(100))
		Expect(different.Schemes[0].ActualPath).To(BeAnExistingFile())
		Expect(different.Schemes[0].DiffPath).To(BeAnExistingFile())
		Expect(different.Schemes[0].Diagnosis).To(And(
			ContainSubstring("100 of 100 pixels differ"),
			ContainSubstring("actual:"),
			ContainSubstring("diff:"),
		))
	})

	It("captures every color scheme into a distinct baseline and restores the original scheme", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, "data:text/html,"+`<style>body{margin:0}.box{width:10px;height:10px;background:white}@media(prefers-color-scheme:dark){.box{background:black}}</style><div class=box></div>`)).To(Succeed())
		before, err := session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())
		root := GinkgoT().TempDir()

		result, err := session.CompareScreenshot(ctx, "theme", engine.ElementScreenshotTarget(engine.CSS(".box")), engine.VisualOptions{
			BaselineDir: filepath.Join(root, "baselines"), ArtifactDir: filepath.Join(root, "artifacts"), Update: true,
			ColorSchemes: []string{"light", "dark"}, SettleAttempts: 3, SettleStreak: 2, SettleInterval: time.Millisecond,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Schemes).To(HaveLen(2))
		light, err := os.ReadFile(filepath.Join(root, "baselines", "theme-light.png"))
		Expect(err).NotTo(HaveOccurred())
		dark, err := os.ReadFile(filepath.Join(root, "baselines", "theme-dark.png"))
		Expect(err).NotTo(HaveOccurred())
		Expect(bytes.Equal(light, dark)).To(BeFalse())
		after, err := session.Evaluate(ctx, `matchMedia('(prefers-color-scheme: dark)').matches`)
		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(Equal(before))
	})
})

var _ = Describe("screenshot diff", func() {
	It("returns structured dimensions, regions, and an observable PNG diff", func() {
		baseline := solidPNG(8, 8, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		actualImage := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				actualImage.SetNRGBA(x, y, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
			}
		}
		actualImage.SetNRGBA(6, 2, color.NRGBA{R: 255, A: 255})
		actual := encodePNG(actualImage)

		diff, diffPNG, err := engine.CompareScreenshotPNGs(baseline, actual, engine.ScreenshotTolerance{})
		Expect(err).NotTo(HaveOccurred())
		Expect(diff.Match).To(BeFalse())
		Expect(diff.DifferingPixels).To(Equal(1))
		Expect(diff.MaxChannelDelta).To(Equal(255))
		Expect(diff.Regions).To(ContainElement(engine.ScreenshotRegion{Rect: image.Rect(6, 2, 7, 3), Count: 1}))
		Expect(bytes.HasPrefix(diffPNG, []byte("\x89PNG\r\n\x1a\n"))).To(BeTrue())
		Expect(pixelAt(diffPNG, 6, 2)).NotTo(Equal(pixelAt(actual, 6, 2)))
	})

	It("rejects malformed and oversized PNG artifacts", func() {
		path := filepath.Join(GinkgoT().TempDir(), "shot.png")
		Expect(engine.WriteScreenshotPNG(path, []byte("not-png"), 1024)).To(MatchError(ContainSubstring("decode PNG")))
		Expect(engine.WriteScreenshotPNG(path, solidPNG(2, 2, color.NRGBA{A: 255}), 4)).To(MatchError(ContainSubstring("exceeds the 4-byte limit")))
		_, err := os.Stat(path)
		Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue())
	})
})

func solidPNG(width, height int, fill color.NRGBA) []byte {
	GinkgoHelper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	return encodePNG(img)
}

func encodePNG(img image.Image) []byte {
	GinkgoHelper()
	var out bytes.Buffer
	Expect(png.Encode(&out, img)).To(Succeed())
	return out.Bytes()
}

func pixelAt(data []byte, x, y int) color.NRGBA {
	GinkgoHelper()
	img, err := png.Decode(bytes.NewReader(data))
	Expect(err).NotTo(HaveOccurred())
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

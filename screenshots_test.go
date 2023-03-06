package biloba_test

import (
	"bytes"
	"image"
	"image/color"
	_ "image/png"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Screenshots", func() {
	It("has a default window size", func() {
		width, height := b.WindowSize()
		Ω(width).Should(Equal(1024))
		Ω(height).Should(Equal(768))
		Ω(`window.innerWidth`).Should(b.EvaluateTo(1024.))
		Ω(`window.innerHeight`).Should(b.EvaluateTo(768.))
	})

	Describe("overriding window size and resetting it", Ordered, func() {
		It("can override the window size", func() {
			b.SetWindowSize(800, 1000)
			Ω(`window.innerWidth`).Should(b.EvaluateTo(800.))
			Ω(`window.innerHeight`).Should(b.EvaluateTo(1000.))

			b.SetWindowSize(200, 100)
			Ω(`window.innerWidth`).Should(b.EvaluateTo(200.))
			Ω(`window.innerHeight`).Should(b.EvaluateTo(100.))
		})

		It("resets it for the next spec", func() {
			width, height := b.WindowSize()
			Ω(width).Should(Equal(1024))
			Ω(height).Should(Equal(768))
			Ω(`window.innerWidth`).Should(b.EvaluateTo(1024.))
			Ω(`window.innerHeight`).Should(b.EvaluateTo(768.))
		})
	})

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
			Ω(img.At(10, 5)).Should(Equal(color.NRGBA{0, 0, 255, 255}))
		})
	})
})

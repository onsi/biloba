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

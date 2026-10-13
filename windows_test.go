package biloba_test

import (
	"bytes"
	"image"
	_ "image/png"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Windows", func() {
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

	Describe("emulating a device", Ordered, func() {
		// The orientation the tab starts in depends on the lane: full Chrome sizes its emulated screen to the
		// 1024x768 viewport, which is landscape.
		var startingOrientation any
		BeforeAll(func() {
			b.Navigate(fixtureServer + "/dom.html")
			Eventually("#hello").Should(b.Exist())
			startingOrientation = b.Run(`screen.orientation.type`)
		})

		BeforeEach(func() {
			b.Navigate(fixtureServer + "/dom.html")
			Eventually("#hello").Should(b.Exist())
		})

		It("sets the device scale factor, touch screen, and orientation", func() {
			b.SetWindowSize(400, 300, b.DeviceScaleFactor(2), b.TouchScreen(), b.Landscape())
			Ω(`window.devicePixelRatio`).Should(b.EvaluateTo(2.))
			Ω(`navigator.maxTouchPoints > 0`).Should(b.EvaluateTo(true))
			Ω(`matchMedia("(pointer: coarse)").matches`).Should(b.EvaluateTo(true))
			Ω(`screen.orientation.type`).Should(b.EvaluateTo("landscape-primary"))

			img, _, err := image.Decode(bytes.NewReader(b.CaptureScreenshot()))
			Ω(err).ShouldNot(HaveOccurred())
			Ω(img.Bounds().Dx()).Should(Equal(800), "the capture is in device pixels")
		})

		It("lays a page with no viewport meta tag out like a phone does when Mobile", func() {
			b.Navigate(fixtureServer + "/no_viewport_meta.html")
			Eventually("#content").Should(b.Exist())
			b.SetWindowSize(400, 300, b.Mobile())
			Eventually(`window.innerWidth`).Should(b.EvaluateTo(980.))
		})

		It("turns the options off again for the next spec", func() {
			Ω(`window.devicePixelRatio`).Should(b.EvaluateTo(1.))
			Ω(`navigator.maxTouchPoints`).Should(b.EvaluateTo(0.))
			Ω(`screen.orientation.type`).Should(b.EvaluateTo(startingOrientation))
			Ω(`window.innerWidth`).Should(b.EvaluateTo(1024.))
		})
	})
})

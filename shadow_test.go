package biloba_test

import (
	"bytes"
	"image"
	_ "image/png"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Piercing shadow DOM and same-origin iframes with >>>", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/shadow.html")
		Eventually("#hello").Should(b.Exist())
	})

	Describe("open shadow roots", func() {
		It("finds, reads, and acts on elements inside a shadow root", func() {
			Eventually("my-widget >>> .shadow-btn").Should(b.Exist())
			Expect("my-widget >>> .label").To(b.HaveInnerText("Shadow Label"))

			b.Click("my-widget >>> .shadow-btn")
			Eventually("my-widget >>> .shadow-btn").Should(b.HaveInnerText("Clicked"))
		})

		It("supports the *Each / count forms across the boundary", func() {
			Expect("my-widget >>> .item").To(b.HaveCount(2))
			Expect(b.InnerTextForEach("my-widget >>> .item")).To(Equal([]string{"A", "B"}))
		})

		It("reports the full selector when a boundary can't be crossed", func() {
			// #hello is a plain element with no shadow root, so the boundary can't be crossed
			b.Click("#hello >>> .nope")
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #hello >>> .nope"))
		})

		It("clicks an element inside an open shadow root in realistic mode", func() {
			// the realistic hittability check must pierce the shadow boundary: elementFromPoint
			// retargets to the host, so a naive topmost check would call the inner button obscured
			Eventually("my-widget >>> .shadow-btn").Should(b.Realistic().Click())
			Eventually("my-widget >>> .shadow-btn").Should(b.HaveInnerText("Clicked"))
		})

		It("reports BeClickable for an element inside an open shadow root", func() {
			Eventually("my-widget >>> .shadow-btn").Should(b.BeClickable())
		})
	})

	Describe("same-origin iframes", func() {
		It("finds and acts on elements inside the iframe document", func() {
			Eventually("#inner >>> #iframe-btn").Should(b.Exist())

			b.Click("#inner >>> #iframe-btn")
			Eventually("#inner >>> #iframe-btn").Should(b.HaveInnerText("Iframe Clicked"))
		})

		It("supports counting across the iframe boundary", func() {
			Eventually("#inner >>> .cell").Should(b.HaveCount(2))
		})

		It("screenshots an element inside the iframe, translated to top-level coordinates", func() {
			Eventually("#inner >>> #iframe-btn").Should(b.Exist())
			data := b.CaptureScreenshotOf("#inner >>> #iframe-btn")
			Ω(data).ShouldNot(BeEmpty())
			// a valid (non-empty, correctly-clipped) PNG only results if the iframe-local
			// bounding box was translated into top-level page coordinates - a bad translation
			// would clip off-page and yield an error or an empty/degenerate image.
			cfg, _, err := image.DecodeConfig(bytes.NewBuffer(data))
			Ω(err).ShouldNot(HaveOccurred())
			Ω(cfg.Width).Should(BeNumerically(">", 0))
			Ω(cfg.Height).Should(BeNumerically(">", 0))
		})
	})
})

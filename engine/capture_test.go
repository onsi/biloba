package engine_test

import (
	"github.com/chromedp/cdproto/page"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("capture geometry", Label("no-browser"), func() {
	Describe("EnclosingClip", func() {
		It("rounds a fractional box outward to whole CSS pixels", func() {
			Ω(engine.EnclosingClip(&page.Viewport{X: 10.5, Y: 20.25, Width: 100.5, Height: 50.5, Scale: 1})).
				Should(Equal(&page.Viewport{X: 10, Y: 20, Width: 101, Height: 51, Scale: 1}))
		})

		It("leaves a whole-pixel box alone, floating-point noise included", func() {
			Ω(engine.EnclosingClip(&page.Viewport{X: 10, Y: 20.0000001, Width: 99.9999999, Height: 50, Scale: 1})).
				Should(Equal(&page.Viewport{X: 10, Y: 20, Width: 100, Height: 50, Scale: 1}))
		})
	})

	Describe("ViewportRegionClip", func() {
		It("offsets a viewport region by the viewport's page origin", func() {
			Ω(engine.ViewportRegionClip(&page.Viewport{X: 5, Y: 10, Width: 50, Height: 40}, 0, 300, 400, 300)).
				Should(Equal(&page.Viewport{X: 5, Y: 310, Width: 50, Height: 40, Scale: 1}))
		})

		It("refuses an empty region or one that reaches outside the viewport", func() {
			_, err := engine.ViewportRegionClip(&page.Viewport{X: 0, Y: 0, Width: 0, Height: 40}, 0, 0, 400, 300)
			Ω(err).Should(MatchError(ContainSubstring("positive width and height")))
			_, err = engine.ViewportRegionClip(&page.Viewport{X: 380, Y: 0, Width: 40, Height: 40}, 0, 0, 400, 300)
			Ω(err).Should(MatchError(ContainSubstring("reaches outside the 400 x 300 viewport")))
		})
	})
})

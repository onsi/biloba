package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Geometry getters and matchers", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/geometry.html")
		Eventually("#hero").Should(b.Exist())
	})

	Describe("BoundingBox", func() {
		It("returns the viewport-relative box of the first match", func() {
			box := b.BoundingBox("#hero")
			Ω(box.Top).Should(BeNumerically("~", 400, 1))
			Ω(box.Left).Should(BeNumerically("~", 30, 1))
			Ω(box.Width).Should(BeNumerically("~", 100, 1))
			Ω(box.Height).Should(BeNumerically("~", 80, 1))
			Ω(box.Bottom).Should(BeNumerically("~", 480, 1))
			Ω(box.Right).Should(BeNumerically("~", 130, 1))
			Ω(box.CenterX).Should(BeNumerically("~", 80, 1))
			Ω(box.CenterY).Should(BeNumerically("~", 440, 1))
		})

		It("polls until the element is actually laid out (non-degenerate box)", func() {
			// #late has height:0 until layoutLate() runs - the getter must wait for a real box.
			b.Run("setTimeout(layoutLate, 100)")
			box := b.BoundingBox("#late")
			Ω(box.Height).Should(BeNumerically("~", 40, 1))
		})

		It("fails fast under Immediate() when the element has a zero-area box", func() {
			b.Immediate().BoundingBox("#late")
			ExpectFailures(ContainSubstring("be present and laid out"))
		})
	})

	Describe("HaveBoundingBox", func() {
		It("matches once the element is laid out and the box satisfies the matcher", func() {
			Eventually("#hero").Should(b.HaveBoundingBox(HaveField("Width", BeNumerically("==", 100))))
		})

		It("keeps polling through layout", func() {
			b.Run("setTimeout(layoutLate, 100)")
			Eventually("#late").Should(b.HaveBoundingBox(HaveField("Height", BeNumerically(">", 0))))
		})

		It("reports the box in its failure message", func() {
			match, err := b.HaveBoundingBox(HaveField("Width", BeNumerically("==", 999))).Match("#hero")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(match).Should(BeFalse())
			Ω(b.HaveBoundingBox(HaveField("Width", BeNumerically("==", 999))).FailureMessage("#hero")).Should(ContainSubstring("HaveBoundingBox"))
		})
	})

	Describe("ScrollOffset", func() {
		It("reports the container's scroll position and scrollable range", func() {
			offset := b.ScrollOffset(".scroller")
			Ω(offset.Top).Should(BeNumerically("==", 0))
			// three 150px sections = 450px of content in a 200px viewport
			Ω(offset.MaxTop).Should(BeNumerically("~", 250, 1))
		})

		It("tracks the scroll position as it changes", func() {
			b.Run("scrollContainerTo(120)")
			Eventually(".scroller").Should(b.HaveScrollOffset(HaveField("Top", BeNumerically("==", 120))))
		})
	})

	Describe("OffsetTopWithin", func() {
		It("returns how far the element's top sits below the container's top", func() {
			// section 2 is the third 150px section, so 300px down inside the unscrolled container
			Ω(b.OffsetTopWithin("#s2", ".scroller")).Should(BeNumerically("~", 300, 1))
		})

		It("settles toward the top of the pane as the container scrolls", func() {
			// scrolling to the container's max offset (250: 450px of content in a 200px pane) brings
			// section 2 from 300px down to 50px below the top
			b.Run("scrollContainerTo(250)")
			Eventually("#s2").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("~", 50, 2)))
		})

		It("fails fast under Immediate() when the container is missing", func() {
			b.Immediate().OffsetTopWithin("#s2", ".nope")
			ExpectFailures(ContainSubstring("be present and laid out within its container"))
		})
	})

	Describe("OffsetLeftWithin", func() {
		It("returns the horizontal offset within the container", func() {
			Ω(b.OffsetLeftWithin("#s0", ".scroller")).Should(BeNumerically("~", 0, 1))
		})
	})
})

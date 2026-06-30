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

	Describe("GetBoundingBox", func() {
		It("returns the viewport-relative box of the first match", func() {
			box := b.GetBoundingBox("#hero")
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
			box := b.GetBoundingBox("#late")
			Ω(box.Height).Should(BeNumerically("~", 40, 1))
		})

		It("fails fast under Immediate() when the element has a zero-area box", func() {
			b.Immediate().GetBoundingBox("#late")
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

	Describe("GetScrollOffset", func() {
		It("reports the container's scroll position and scrollable range", func() {
			offset := b.GetScrollOffset(".scroller")
			Ω(offset.Top).Should(BeNumerically("==", 0))
			// three 150px sections = 450px of content in a 200px viewport
			Ω(offset.MaxTop).Should(BeNumerically("~", 250, 1))
		})

		It("tracks the scroll position as it changes", func() {
			b.Run("scrollContainerTo(120)")
			Eventually(".scroller").Should(b.HaveScrollOffset(HaveField("Top", BeNumerically("==", 120))))
		})
	})

	Describe("GetOffsetTopWithin", func() {
		It("returns how far the element's top sits below the container's top", func() {
			// section 2 is the third 150px section, so 300px down inside the unscrolled container
			Ω(b.GetOffsetTopWithin("#s2", ".scroller")).Should(BeNumerically("~", 300, 1))
		})

		It("settles toward the top of the pane as the container scrolls", func() {
			// scrolling to the container's max offset (250: 450px of content in a 200px pane) brings
			// section 2 from 300px down to 50px below the top
			b.Run("scrollContainerTo(250)")
			Eventually("#s2").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("~", 50, 2)))
		})

		It("fails fast under Immediate() when the container is missing", func() {
			b.Immediate().GetOffsetTopWithin("#s2", ".nope")
			ExpectFailures(ContainSubstring("be present and laid out within its container"))
		})
	})

	Describe("GetOffsetLeftWithin", func() {
		It("returns the horizontal offset within the container", func() {
			Ω(b.GetOffsetLeftWithin("#s0", ".scroller")).Should(BeNumerically("~", 0, 1))
		})
	})

	Describe("pairwise geometry matchers", func() {
		It("asserts vertical relationships", func() {
			Eventually("#above").Should(b.BeAbove("#below"))
			Eventually("#below").Should(b.BeBelow("#above"))
			Ω("#below").ShouldNot(b.BeAbove("#above"))
		})

		It("asserts horizontal relationships", func() {
			Eventually("#leftbox").Should(b.BeLeftOf("#rightbox"))
			Eventually("#rightbox").Should(b.BeRightOf("#leftbox"))
			Ω("#rightbox").ShouldNot(b.BeLeftOf("#leftbox"))
		})

		It("asserts enclosure", func() {
			Eventually("#frame").Should(b.Encloses("#enclosed"))
			Ω("#enclosed").ShouldNot(b.Encloses("#frame"))
		})

		It("asserts overlap", func() {
			Eventually("#ovA").Should(b.Overlaps("#ovB"))
			Ω("#above").ShouldNot(b.Overlaps("#below"))
		})

		It("polls until both elements are laid out", func() {
			// #late lays out late; once it does, #hero sits above it (hero bottom 480 <= late top 600)
			b.Run("setTimeout(layoutLate, 100)")
			Eventually("#hero").Should(b.BeAbove("#late"))
		})

		It("reports both boxes in its failure message", func() {
			msg := b.BeAbove("#above").FailureMessage("#below")
			Ω(msg).Should(ContainSubstring("be above"))
			Ω(msg).Should(ContainSubstring("subject box"))
			Ω(msg).Should(ContainSubstring("other box"))
		})
	})

	Describe("GetGapBetween / HaveGapBetween", func() {
		It("returns the per-field delta between two elements", func() {
			// #span is centered within #card (same centerX), 20px lower, and 100px narrower
			delta := b.GetGapBetween("#span", "#card")
			Ω(delta.CenterX).Should(BeNumerically("~", 0, 1))
			Ω(delta.Top).Should(BeNumerically("~", 20, 1))
			Ω(delta.Width).Should(BeNumerically("~", -100, 1))
		})

		It("matches once the delta satisfies the sub-matcher", func() {
			Eventually("#span").Should(b.HaveGapBetween("#card", HaveField("CenterX", BeNumerically("~", 0, 1))))
		})

		It("reports the delta in its failure message", func() {
			m := b.HaveGapBetween("#card", HaveField("CenterX", BeNumerically("==", 999)))
			match, err := m.Match("#span")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(match).Should(BeFalse())
			Ω(m.FailureMessage("#span")).Should(ContainSubstring("HaveGapBetween"))
		})

		It("fails fast under Immediate() when the other element is missing", func() {
			b.Immediate().GetGapBetween("#span", ".nope")
			ExpectFailures(ContainSubstring("be present and laid out alongside the other element"))
		})
	})

	Describe("BeInViewport", func() {
		It("passes for an on-screen element", func() {
			Eventually("#vp-on").Should(b.BeInViewport())
		})

		It("fails for an element scrolled far below the window", func() {
			Ω("#vp-below").ShouldNot(b.BeInViewport())
		})

		It("fails for an element above the top of the window", func() {
			Ω("#vp-above").ShouldNot(b.BeInViewport())
		})

		It("reports the element and viewport in its failure message", func() {
			Ω(b.BeInViewport().FailureMessage("#vp-below")).Should(ContainSubstring("be within the viewport"))
		})
	})

	Describe("document-order matchers", func() {
		It("asserts precedes / follows once the nodes are inserted", func() {
			b.Run("appendOrdered()")
			Eventually("#o-second").Should(b.BePrecededBy("#o-first"))
			Eventually("#o-first").Should(b.BeFollowedBy("#o-second"))
			Ω("#o-first").ShouldNot(b.BePrecededBy("#o-second"))
		})

		It("polls until both nodes exist", func() {
			b.Run("setTimeout(appendOrdered, 100)")
			Eventually("#o-third").Should(b.BePrecededBy("#o-first"))
		})

		It("reports the relationship in its failure message", func() {
			b.Run("appendOrdered()")
			Eventually("#o-first").Should(b.Exist())
			Ω(b.BePrecededBy("#o-second").FailureMessage("#o-first")).Should(ContainSubstring("be preceded by"))
		})
	})

	Describe("GetComputedStyle", func() {
		It("returns resolved standard properties", func() {
			Ω(b.GetComputedStyle("#rail", "color")).Should(Equal("rgb(10, 20, 30)"))
			Ω(b.GetComputedStyle("#rail", "z-index")).Should(Equal("7"))
		})

		It("resolves CSS custom properties", func() {
			Ω(b.GetComputedStyle("#rail", "--stage")).Should(ContainSubstring("DCE4E1"))
		})

		It("backs HaveComputedStyle's custom-property resolution too", func() {
			Ω("#rail").Should(b.HaveComputedStyle("--stage", ContainSubstring("DCE4E1")))
		})

		It("fails fast under Immediate() when the element is missing", func() {
			b.Immediate().GetComputedStyle("#nope", "color")
			ExpectFailures(ContainSubstring("be present"))
		})
	})
})

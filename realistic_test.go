package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BeClickable and realistic interactions", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/realistic.html")
		Eventually("#heading").Should(b.Exist())
	})

	Describe("BeClickable", func() {
		It("passes for a visible, enabled, unobscured element", func() {
			Eventually("#plain-btn").Should(b.BeClickable())
		})

		It("fails for a disabled element", func() {
			// a disabled element fails the enabled guard; like Biloba's other multi-guard
			// matchers that surfaces as a non-match (with the guard message as the error)
			success, err := b.BeClickable().Match("#disabled-btn")
			Expect(success).To(BeFalse())
			Expect(err).To(MatchError(ContainSubstring("not enabled")))
		})

		It("fails for an element that is visible but obscured by an overlay", func() {
			Expect("#covered-btn").To(b.BeVisible()) // it IS visible (non-zero offset)...
			matcher := b.BeClickable()
			success, err := matcher.Match("#covered-btn")
			Expect(err).NotTo(HaveOccurred())
			Expect(success).To(BeFalse()) // ...but not clickable
			Expect(matcher.FailureMessage("#covered-btn")).To(ContainSubstring("obscured"))
		})

		It("fails for an element whose center is scrolled out of the viewport", func() {
			Expect("#below-btn").NotTo(b.BeClickable())
		})

		It("becomes clickable once the obscuring overlay is removed", func() {
			Expect("#covered-btn").NotTo(b.BeClickable())
			b.Run(`document.getElementById('cover').remove()`)
			Eventually("#covered-btn").Should(b.BeClickable())
		})
	})

	Describe("realistic Click", func() {
		It("clicks a plain element with real CDP input", func() {
			b.Realistic().Click("#plain-btn")
			Eventually("#plain-result").Should(b.HaveInnerText("clicked"))
		})

		It("supports the matcher form", func() {
			Eventually("#plain-btn").Should(b.Realistic().Click())
			Eventually("#plain-result").Should(b.HaveInnerText("clicked"))
		})

		It("scrolls an off-screen element into view before clicking it", func() {
			b.Realistic().Click("#below-btn")
			Eventually("#scroll-result").Should(b.HaveInnerText("clicked"))
		})

		It("refuses to click through an occluding overlay - unlike plain Click", func() {
			// plain Click fires el.click() directly and so clicks straight through the overlay
			b.Click("#covered-btn")
			Eventually("#covered-result").Should(b.HaveInnerText("clicked"))

			// realistic Click dispatches a real mouse event, which would land on the overlay,
			// so it refuses and fails the spec rather than clicking the hidden button
			b.Run(`document.getElementById('covered-result').textContent = 'reset'`)
			b.Realistic().Click("#covered-btn")
			ExpectFailures(ContainSubstring("not clickable"))
			Expect("#covered-result").To(b.HaveInnerText("reset"))
		})
	})

	Describe("realistic Hover", func() {
		It("activates real CSS :hover, revealing a submenu - unlike plain Hover", func() {
			// plain Hover fires synthetic events but does not activate CSS :hover
			b.Hover("#menu")
			Expect("#submenu").NotTo(b.BeVisible())

			// realistic Hover moves the real mouse, which activates CSS :hover
			b.Realistic().Hover("#menu")
			Eventually("#submenu").Should(b.BeVisible())
		})

		It("hovers then clicks a submenu item exposed only by :hover", func() {
			rb := b.Realistic()
			rb.Hover("#menu")
			Eventually("#submenu-item").Should(b.BeVisible())
			rb.Click("#submenu-item")
			Eventually("#hover-result").Should(b.HaveInnerText("selected"))
		})
	})
})

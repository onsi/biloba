package biloba_test

import (
	"github.com/onsi/biloba"
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

		It("clicks an element larger than the viewport at a visible point", func() {
			// #big is taller than the viewport, so its geometric center is off-screen; the click
			// point is clamped to the visible intersection
			b.Realistic().Click("#big")
			Eventually("#big-result").Should(b.HaveInnerText("clicked"))
		})

		It("waits for a moving element to settle before clicking it", func() {
			// #moving-btn transitions into place on load; the stability wait clicks its settled spot
			b.Realistic().Click("#moving-btn")
			Eventually("#moving-result").Should(b.HaveInnerText("clicked"))
		})

		It("moves the pointer before pressing, so hover-gated clicks register", func() {
			// #gated-btn only counts a click if a mouseover preceded it
			b.Realistic().Click("#gated-btn")
			Eventually("#gated-result").Should(b.HaveInnerText("clicked"))
		})

		It("does not register the hover-gated click via plain (synthetic) Click", func() {
			// plain Click fires el.click() with no preceding pointer movement
			b.Click("#gated-btn")
			Expect("#gated-result").To(b.HaveInnerText("no"))
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

	Describe("realistic ClickEach", func() {
		It("clicks every matching element with real input", func() {
			b.Realistic().ClickEach(".each-btn")
			Eventually(".each-btn").Should(b.EachHaveInnerText("clicked", "clicked", "clicked"))
		})
	})

	Describe("realistic DblClick", func() {
		It("dispatches a real double-click (two clicks plus dblclick)", func() {
			b.Realistic().DblClick("#dbl-btn")
			Eventually("#dbl-result").Should(b.HaveInnerText("double"))
			Expect("#dbl-clicks").To(b.HaveInnerText("2"))
		})

		It("scrolls an off-screen element into view before double-clicking", func() {
			Eventually("#dbl-btn").Should(b.Realistic().DblClick())
			Eventually("#dbl-result").Should(b.HaveInnerText("double"))
		})
	})

	Describe("realistic RightClick", func() {
		It("dispatches a real right-button click that fires contextmenu", func() {
			b.Realistic().RightClick("#ctx-btn")
			Eventually("#ctx-result").Should(b.HaveInnerText("menu"))
		})
	})

	Describe("realistic DragTo", func() {
		It("drags the source onto the target with real pointer input", func() {
			b.Realistic().DragTo("#drag-src", "#drop-zone")
			Eventually("#drop-result").Should(b.HaveInnerText("dropped"))
		})
	})

	Describe("realistic ScrollWheel", func() {
		It("scrolls the element with real wheel input", func() {
			Expect(b.GetProperty("#scroll-box", "scrollTop")).To(BeEquivalentTo(0))
			b.Realistic().ScrollWheel("#scroll-box", 0, 200)
			Eventually("#wheel-result").Should(b.HaveInnerText("wheeled"))
			Eventually(func() float64 {
				return b.GetProperty("#scroll-box", "scrollTop").(float64)
			}).Should(BeNumerically(">", 0))
		})
	})

	Describe("realistic MiddleClick", func() {
		It("dispatches a real middle-button click that fires auxclick", func() {
			b.Realistic().MiddleClick("#aux-btn")
			Eventually("#aux-result").Should(b.HaveInnerText("middle"))
		})
	})

	Describe("realistic ClickWith", func() {
		It("dispatches a real click carrying the modifier", func() {
			b.Realistic().ClickWith("#mod-btn", biloba.ModShift)
			Eventually("#mod-result").Should(b.HaveInnerText("shift"))
		})
	})

	Describe("realistic Tap", func() {
		It("dispatches a real touch that fires touchend", func() {
			b.Realistic().Tap("#tap-btn")
			Eventually("#tap-result").Should(b.HaveInnerText("tapped"))
		})
	})

	Describe("realistic Type", func() {
		It("scrolls an off-screen input into view before typing into it", func() {
			b.Realistic().Type("#below-input", "typed")
			Expect("#below-input").To(b.HaveValue("typed"))
		})
	})

	Describe("realistic SetValue", func() {
		It("types a real value into a text input (and fires change on blur)", func() {
			b.Realistic().SetValue("#text-input", "hello")
			Expect("#text-input").To(b.HaveValue("hello"))
			Eventually("#text-changed").Should(b.HaveInnerText("yes"))
		})

		It("toggles a checkbox with a real click when it isn't in the desired state", func() {
			Expect(b.GetValue("#check-input")).To(BeFalse())
			b.Realistic().SetValue("#check-input", true)
			Expect("#check-input").To(b.BeChecked())
			Eventually("#check-changed").Should(b.HaveInnerText("yes"))
		})

		It("leaves a checkbox untouched when it is already in the desired state", func() {
			b.Realistic().SetValue("#check-input", false) // already unchecked
			Expect("#check-input").NotTo(b.BeChecked())
			Expect("#check-changed").To(b.HaveInnerText("no")) // no click => no change event
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

	Describe("realistic Click across a same-origin iframe boundary", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/shadow.html")
			Eventually("#hello").Should(b.Exist())
		})

		It("translates iframe-local coordinates to top-level viewport coordinates", func() {
			// the button lives inside #inner (an iframe positioned well below the top-left),
			// so its in-iframe coordinates must be translated by the iframe's offset or the
			// real mouse click lands on the wrong spot
			Eventually("#inner >>> #iframe-btn").Should(b.Exist())
			b.Realistic().Click("#inner >>> #iframe-btn")
			Eventually("#inner >>> #iframe-btn").Should(b.HaveInnerText("Iframe Clicked"))
		})
	})
})

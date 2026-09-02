package biloba_test

import (
	"strconv"
	"strings"
	"time"

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
			b.Realistic().WithTimeout(time.Millisecond * 60).Click("#covered-btn")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("to be clickable"),
			))
			Expect("#covered-result").To(b.HaveInnerText("reset"))
		})
	})

	Describe("realistic ClickEachImmediately", func() {
		It("clicks every matching element with real input", func() {
			b.Realistic().ClickEachImmediately(".each-btn")
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

		It("scrolls a target pinned near the bottom of the viewport with real wheel input", func() {
			// #bottom-scroll-box sits at the document's end, so scrollToStablePoint cannot center it -
			// its centroid stays near the viewport bottom, where full ("new") headless Chrome used to
			// silently drop trusted wheel input (the emulated layout viewport was taller than the real
			// compositor input surface).  This guards that the layout-vs-compositor mismatch is fixed.
			Expect(b.GetProperty("#bottom-scroll-box", "scrollTop")).To(BeEquivalentTo(0))
			b.Realistic().ScrollWheel("#bottom-scroll-box", 0, 200)
			Eventually("#bottom-wheel-result").Should(b.HaveInnerText("wheeled"))
			Eventually(func() float64 {
				return b.GetProperty("#bottom-scroll-box", "scrollTop").(float64)
			}).Should(BeNumerically(">", 0))
		})
	})

	Describe("realistic Click with b.At(offset)", func() {
		It("clicks at the requested offset from the element's top-left corner with real input", func() {
			b.Realistic().Click("#click-pad", b.At(30, 40))
			var x, y int
			Eventually(func() string {
				return b.GetProperty("#click-pad-result", "innerText").(string)
			}).Should(SatisfyAll(
				ContainSubstring(","),
				WithTransform(func(s string) bool {
					parts := strings.Split(s, ",")
					if len(parts) != 2 {
						return false
					}
					var err error
					if x, err = strconv.Atoi(parts[0]); err != nil {
						return false
					}
					y, err = strconv.Atoi(parts[1])
					return err == nil
				}, BeTrue()),
			))
			Expect(x).To(BeNumerically("~", 30, 2))
			Expect(y).To(BeNumerically("~", 40, 2))
		})
	})

	Describe("realistic MiddleClick", func() {
		It("dispatches a real middle-button click that fires auxclick", func() {
			b.Realistic().MiddleClick("#aux-btn")
			Eventually("#aux-result").Should(b.HaveInnerText("middle"))
		})
	})

	Describe("realistic Click with modifiers", func() {
		It("dispatches a real click carrying the modifier", func() {
			b.Realistic().Click("#mod-btn", b.Shift())
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

// A realistic drag has to press at one point and release at another, and both points have to be live
// at the same moment.  Measuring them one at a time cannot do that inside a scroll container:
// scrolling the target into view moves the source, so the source coordinate names whatever row slid
// into that spot - and when both endpoints get centered in turn, the two coordinates land on top of
// each other and the press and release arrive together.  The page receives that as a click on the
// target, which is a wrong action rather than a failed one, so every spec here asserts WHERE the drag
// landed rather than that DragTo returned.
var _ = Describe("realistic DragTo inside a scroll container", func() {
	BeforeEach(func() {
		// pinned so the pane's visible band, and therefore which pairs of rows can share it, is the
		// same on every machine (SetWindowSize registers its own reset)
		b.SetWindowSize(800, 600)
		b.Navigate(fixtureServer + "/realistic_dragging.html")
		Eventually("#row-1").Should(b.Exist())
	})

	It("drags between two rows that are both already in the pane's band", func() {
		// #list is scrolled to 80, so its band is 80..320: row-4 (120..160) and row-7 (240..280) are
		// both in it and nothing needs to scroll at all.  Centering them in turn - the old behaviour -
		// puts both coordinates at the pane's center, which degrades into a click on row-7.
		b.Realistic().DragTo("#row-4", "#row-7")
		Eventually("#drag-result").Should(b.HaveInnerText("row-4 -> row-7"))
	})

	It("drags to a row the pane has not scrolled to yet", func() {
		// row-9 (320..360) is below the band, so a scroll is unavoidable - and it has to be ONE scroll
		// that frames the pair, not one per endpoint
		b.Realistic().DragTo("#row-4", "#row-9")
		Eventually("#drag-result").Should(b.HaveInnerText("row-4 -> row-9"))
	})

	It("drags between two separate scroll containers", func() {
		// endpoints in different panes share no scroller, so framing them around their midpoint cannot
		// work: each pane has to be scrolled, and only then are both points measured
		b.Realistic().DragTo("#row-4", "#other-10")
		Eventually("#drag-result").Should(b.HaveInnerText("row-4 -> other-10"))
	})

	It("supports the matcher form", func() {
		Eventually("#row-4").Should(b.Realistic().DragTo("#row-7"))
		Eventually("#drag-result").Should(b.HaveInnerText("row-4 -> row-7"))
	})

	It("fails, naming both endpoints, when no scroll position can show them together", func() {
		// row-1 and row-20 are 760px apart in a 240px pane: no scroll position shows both, so there is
		// no correct drag to dispatch.  The point of the failure is that it is a failure - dispatching
		// the press and release at one point would silently enter row-20 instead.
		b.Realistic().WithTimeout(300*time.Millisecond).WithPolling(150*time.Millisecond).DragTo("#row-1", "#row-20")
		ExpectFailures(SatisfyAll(
			ContainSubstring("could not put both drag endpoints on screen at the same time"),
			ContainSubstring("#row-1"),
			ContainSubstring("#row-20"),
			ContainSubstring("scrolled out of view inside div#list"),
		))
		Expect(b.GetProperty("#drag-result", "innerText")).To(Equal("none"))
		Expect(b.GetProperty("#press-count", "innerText")).To(Equal("0"))
	})
})

package biloba_test

import (
	"time"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
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

		It("reports the border-excluded client box alongside the border-box", func() {
			// #clientbox is border-box 100x60 with a 10px border, so its bounding Width/Height are
			// 100x60 while clientWidth/clientHeight (border excluded) are 80x40.
			box := b.GetBoundingBox("#clientbox")
			Ω(box.Width).Should(BeNumerically("~", 100, 1))
			Ω(box.Height).Should(BeNumerically("~", 60, 1))
			Ω(box.ClientWidth).Should(BeNumerically("~", 80, 1))
			Ω(box.ClientHeight).Should(BeNumerically("~", 40, 1))
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

		It("with no expected value, passes once both selector and container are laid out (presence only)", func() {
			Eventually("#s2").Should(b.HaveOffsetTopWithin(".scroller"))
		})

		It("fails fast under Immediate() when the container is missing, naming the container selector", func() {
			b.Immediate().GetOffsetTopWithin("#s2", ".nope")
			ExpectFailures(ContainSubstring("could not find DOM element matching container selector: .nope"))
		})

		It("fails fast under Immediate() when the element itself is missing", func() {
			b.Immediate().GetOffsetTopWithin("#nope", ".scroller")
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #nope"))
		})
	})

	Describe("GetOffsetLeftWithin", func() {
		It("returns the horizontal offset within the container", func() {
			Ω(b.GetOffsetLeftWithin("#s0", ".scroller")).Should(BeNumerically("~", 0, 1))
		})

		It("with no expected value, passes once both selector and container are laid out (presence only)", func() {
			Eventually("#s0").Should(b.HaveOffsetLeftWithin(".scroller"))
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

		It("with no expected value, passes once both elements are laid out (presence only)", func() {
			Eventually("#span").Should(b.HaveGapBetween("#card"))
		})

		It("reports the delta in its failure message", func() {
			m := b.HaveGapBetween("#card", HaveField("CenterX", BeNumerically("==", 999)))
			match, err := m.Match("#span")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(match).Should(BeFalse())
			Ω(m.FailureMessage("#span")).Should(ContainSubstring("HaveGapBetween"))
		})

		It("fails fast under Immediate() when the other element is missing, naming the other selector", func() {
			b.Immediate().GetGapBetween("#span", ".nope")
			ExpectFailures(ContainSubstring("could not find DOM element matching other selector: .nope"))
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

		Context("with b.Fully()", func() {
			It("passes for an element wholly on screen", func() {
				Eventually("#vp-on").Should(b.BeInViewport(b.Fully()))
			})

			It("fails for an element that only partly overlaps the viewport", func() {
				// #vp-partial straddles the top edge: BeInViewport() passes, BeInViewport(b.Fully()) does not.
				Eventually("#vp-partial").Should(b.BeInViewport())
				Ω("#vp-partial").ShouldNot(b.BeInViewport(b.Fully()))
			})

			It("reports the fully-within phrasing in its failure message", func() {
				Ω(b.BeInViewport(b.Fully()).FailureMessage("#vp-partial")).Should(ContainSubstring("be fully within the viewport"))
			})
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

		It("reports the order it actually observed, so a backwards assertion is self-correcting", func() {
			b.Run("appendOrdered()")
			Eventually("#o-second").Should(b.BePrecededBy("#o-first"))

			// #o-first comes before #o-second, so this (backwards) expectation fails - and the message
			// must say which way round they actually are, not merely restate the expectation.
			matcher := b.BePrecededBy("#o-second")
			match, err := matcher.Match("#o-first")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(match).Should(BeFalse())
			Ω(matcher.FailureMessage("#o-first")).Should(ContainSubstring("Actually: #o-first comes BEFORE #o-second"))
		})

		It("describes containment rather than claiming a plain before/after", func() {
			b.Run("appendOrdered()")
			Eventually("#o-first").Should(b.Exist())

			matcher := b.BePrecededBy("#o-first")
			_, err := matcher.Match("#order")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(matcher.FailureMessage("#order")).Should(ContainSubstring("CONTAINS"))
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

	Describe("ScrollIntoView with options", func() {
		It("scrolls a specific container so the target lands at its top", func() {
			// #s1 sits 150px down inside .scroller; scrolling the container brings it to the top
			b.ScrollIntoView("#s1", b.WithinScroller(".scroller"))
			Eventually("#s1").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("~", 0, 1)))
		})

		It("honors a top offset (landing the target below the container top)", func() {
			b.ScrollIntoView("#s1", b.WithinScroller(".scroller"), b.AtTopOffset(20))
			Eventually("#s1").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("~", 20, 1)))
		})

		It("finds the nearest scrollable ancestor when no container is given", func() {
			b.ScrollIntoView("#s1", b.AtTopOffset(10))
			Eventually("#s1").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("~", 10, 1)))
		})

		It("can be used as a matcher with options", func() {
			Eventually("#s1").Should(b.ScrollIntoView(b.AtTopOffset(20)))
			Eventually("#s1").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("~", 20, 1)))
		})

		It("times out (poll-by-default) if a requested container never appears", func() {
			b.WithTimeout(time.Millisecond*60).ScrollIntoView("#s1", b.WithinScroller("#no-such-scroller"))
			ExpectFailures(ContainSubstring("Timed out after"))
		})
	})

	Describe("NormalizeColor and the MatchColor matcher", func() {
		It("normalizes a named color to rgb()", func() {
			Ω(b.NormalizeColor("teal")).Should(Equal("rgb(0, 128, 128)"))
		})

		It("resolves a design-token var() chain", func() {
			Ω(b.NormalizeColor("var(--tok-teal)")).Should(Equal("rgb(20, 184, 166)"))
		})

		It("fails the spec on an invalid color", func() {
			b.NormalizeColor("not-a-color")
			ExpectFailures(ContainSubstring("not a valid CSS color"))
		})

		It("is a hard error to configure it (it is a one-shot snapshot)", func() {
			b.WithTimeout(time.Second).NormalizeColor("teal")
			ExpectFailures(ContainSubstring("NormalizeColor does not support WithTimeout"))
		})

		It("matches a computed color against a token, normalizing both sides", func() {
			Eventually("#swatch").Should(b.HaveComputedStyle("color", b.MatchColor("var(--tok-teal)")))
			Expect("#swatch").To(b.HaveComputedStyle("background-color", b.MatchColor("#14b8a6")))
		})

		It("does not match a different color", func() {
			Eventually("#swatch").ShouldNot(b.HaveComputedStyle("color", b.MatchColor("red")))
		})
	})

	Describe("GetComputedStyleNumeric and HaveComputedStyleNumeric", func() {
		It("parses the leading numeric part of a px value", func() {
			Ω(b.GetComputedStyleNumeric("#hero", "width")).Should(BeNumerically("~", 100, 1))
		})

		It("reads a unitless numeric value", func() {
			Ω(b.GetComputedStyleNumeric("#rail", "z-index")).Should(BeNumerically("==", 7))
		})

		It("fails the spec when the value is not numeric", func() {
			b.Immediate().GetComputedStyleNumeric("#rail", "color")
			ExpectFailures(ContainSubstring("not numeric"))
		})

		It("backs the matcher form", func() {
			Eventually("#hero").Should(b.HaveComputedStyleNumeric("width", BeNumerically(">", 50)))
			Expect("#rail").To(b.HaveComputedStyleNumeric("z-index", 7))
		})
	})

	Describe("a missing element is an error, not a vacuous ShouldNot", func() {
		// The geometry probes split "element absent" from "element present but not yet laid out": absent
		// is an ERROR, present-but-degenerate stays a silent retry.  Gomega counts an assertion satisfied
		// only when the match result is the desired one AND there is no error - so without that error
		// ShouldNot(<geometry matcher>) passes INSTANTLY against a selector that never matches, and a
		// regression guard like Ω("#toast").ShouldNot(b.BeInViewport()) goes green on a page that never
		// rendered #toast at all.  Every spec below pins BOTH directions: Match errors, and the negation
		// therefore fails rather than passing vacuously.
		const missing = "could not find DOM element matching selector: #nope"
		const missingOther = "could not find DOM element matching other selector: #nope"
		const missingContainer = "could not find DOM element matching container selector: #nope"

		var failure string
		var g Gomega

		BeforeEach(func() {
			failure = ""
			g = NewGomega(func(message string, callerSkip ...int) { failure = message })
		})

		// expectHonest asserts that m errors on selector, and that the NEGATION fails because of it.
		// The ShouldNot leg runs through a capturing Gomega - that is the leg that used to pass
		// vacuously, so it is the leg worth pinning.
		expectHonest := func(selector any, m types.GomegaMatcher, expected string) {
			GinkgoHelper()
			match, err := m.Match(selector)
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError(expected))

			failure = ""
			g.Expect(selector).ShouldNot(m)
			Ω(failure).Should(ContainSubstring(expected), "ShouldNot(...) passed vacuously against %v", selector)
		}

		It("BeInViewport", func() {
			expectHonest("#nope", b.BeInViewport(), missing)
			expectHonest("#nope", b.BeInViewport(b.Fully()), missing)
		})

		It("HaveBoundingBox", func() {
			expectHonest("#nope", b.HaveBoundingBox(HaveField("Width", BeNumerically(">", 0))), missing)
		})

		It("HaveScrollOffset", func() {
			expectHonest("#nope", b.HaveScrollOffset(HaveField("Top", BeNumerically(">=", 0))), missing)
		})

		It("HaveComputedStyleNumeric", func() {
			expectHonest("#nope", b.HaveComputedStyleNumeric("width", BeNumerically(">", 0)), missing)
		})

		It("HaveOffsetTopWithin / HaveOffsetLeftWithin", func() {
			expectHonest("#nope", b.HaveOffsetTopWithin(".scroller", BeNumerically(">", 0)), missing)
			expectHonest("#nope", b.HaveOffsetLeftWithin(".scroller", BeNumerically(">=", 0)), missing)
			// ...and a missing CONTAINER says so, rather than blaming the element that is right there
			expectHonest("#s2", b.HaveOffsetTopWithin("#nope", BeNumerically(">", 0)), missingContainer)
			expectHonest("#s2", b.HaveOffsetLeftWithin("#nope", BeNumerically(">=", 0)), missingContainer)

			// the presence-only (zero-arg) form is honest the same way - it never degrades to BeNil()
			expectHonest("#nope", b.HaveOffsetTopWithin(".scroller"), missing)
			expectHonest("#nope", b.HaveOffsetLeftWithin(".scroller"), missing)
			expectHonest("#s2", b.HaveOffsetTopWithin("#nope"), missingContainer)
			expectHonest("#s2", b.HaveOffsetLeftWithin("#nope"), missingContainer)
		})

		It("the pairwise relational matchers", func() {
			expectHonest("#nope", b.BeAbove("#below"), missing)
			expectHonest("#nope", b.BeBelow("#above"), missing)
			expectHonest("#nope", b.BeLeftOf("#rightbox"), missing)
			expectHonest("#nope", b.BeRightOf("#leftbox"), missing)
			expectHonest("#nope", b.Encloses("#enclosed"), missing)
			expectHonest("#nope", b.Overlaps("#ovB"), missing)
		})

		It("the pairwise relational matchers name the OTHER selector when it is the missing one", func() {
			expectHonest("#above", b.BeAbove("#nope"), missingOther)
			expectHonest("#below", b.BeBelow("#nope"), missingOther)
			expectHonest("#leftbox", b.BeLeftOf("#nope"), missingOther)
			expectHonest("#rightbox", b.BeRightOf("#nope"), missingOther)
			expectHonest("#frame", b.Encloses("#nope"), missingOther)
			expectHonest("#ovA", b.Overlaps("#nope"), missingOther)
		})

		It("HaveGapBetween", func() {
			expectHonest("#nope", b.HaveGapBetween("#card", HaveField("Top", BeNumerically(">", 0))), missing)
			expectHonest("#span", b.HaveGapBetween("#nope", HaveField("Top", BeNumerically(">", 0))), missingOther)

			// the presence-only (zero-arg) form is honest the same way - it never degrades to BeNil()
			expectHonest("#nope", b.HaveGapBetween("#card"), missing)
			expectHonest("#span", b.HaveGapBetween("#nope"), missingOther)
		})

		It("BePrecededBy / BeFollowedBy", func() {
			b.Run("appendOrdered()")
			Eventually("#o-first").Should(b.Exist())
			expectHonest("#nope", b.BePrecededBy("#o-first"), missing)
			expectHonest("#nope", b.BeFollowedBy("#o-first"), missing)
			expectHonest("#o-first", b.BePrecededBy("#nope"), missingOther)
			expectHonest("#o-first", b.BeFollowedBy("#nope"), missingOther)
		})

		It("keeps 'present but not yet laid out' a silent retry, so the positive direction still polls", func() {
			// #late IS in the DOM, with height:0 until layoutLate() runs.  That must stay
			// {success:false}/no error - turning it into an error would break polling through late
			// layout, which is the whole reason these matchers exist.
			m := b.HaveBoundingBox(HaveField("Height", BeNumerically(">", 0)))
			match, err := m.Match("#late")
			Ω(match).Should(BeFalse())
			Ω(err).ShouldNot(HaveOccurred())

			relational := b.BeAbove("#late")
			match, err = relational.Match("#hero")
			Ω(match).Should(BeFalse())
			Ω(err).ShouldNot(HaveOccurred())

			b.Run("setTimeout(layoutLate, 50)")
			Eventually("#late").Should(b.HaveBoundingBox(HaveField("Height", BeNumerically(">", 0))))
			Eventually("#hero").Should(b.BeAbove("#late"))
		})

		It("keeps a laid-out element that is merely off screen a plain non-match for BeInViewport", func() {
			// the distinction that matters: #vp-below exists and is laid out, it is just scrolled away.
			// THAT is an honest ShouldNot - no error, and it passes without waiting.
			match, err := b.BeInViewport().Match("#vp-below")
			Ω(match).Should(BeFalse())
			Ω(err).ShouldNot(HaveOccurred())
			Ω("#vp-below").ShouldNot(b.BeInViewport())
		})

		It("no longer lets a REMOVED element satisfy ShouldNot - assert disappearance with Exist instead", func() {
			// This is the migration hazard of the change, and it is deliberate.  A spec that waited for
			// teardown with Eventually(sel).ShouldNot(b.BeInViewport()) used to pass the moment the node
			// left the DOM; it now fails, because a missing element is an error and an errored matcher is
			// never counted as satisfied in EITHER direction.  "Not in the viewport" is a claim about an
			// element that is THERE - so disappearance belongs to the matchers that are about existence.
			Ω("#hero").Should(b.BeInViewport())
			b.Run(`document.querySelector("#hero").remove()`)

			match, err := b.BeInViewport().Match("#hero")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #hero"))

			failure = ""
			g.Expect("#hero").ShouldNot(b.BeInViewport())
			Ω(failure).ShouldNot(BeEmpty(), "ShouldNot(BeInViewport) passed vacuously against a removed element")

			// ...and the replacement idiom, which answers the question that was actually being asked
			Eventually("#hero").ShouldNot(b.Exist())
			Eventually("#hero").Should(b.HaveCount(0))
		})
	})

	Describe("capturing values off the geometry matchers", func() {
		It("captures the Box that satisfied HaveBoundingBox", func() {
			var box biloba.Box
			Eventually("#hero").Should(b.HaveBoundingBox(HaveField("Width", BeNumerically(">", 50))).Capture(&box))
			Ω(box.Width).Should(BeNumerically("~", 100, 1))
			Ω(box.Height).Should(BeNumerically("~", 80, 1))
			Ω(box.Top).Should(BeNumerically("~", 400, 1))
		})

		It("captures the box from the attempt that finally passed", func() {
			// #late has no box until layoutLate() runs - the captured Box must be the laid-out one,
			// not the degenerate box from an earlier polling attempt.
			b.Run("setTimeout(layoutLate, 100)")
			var box biloba.Box
			Eventually("#late").Should(b.HaveBoundingBox(HaveField("Height", BeNumerically(">", 0))).Capture(&box))
			Ω(box.Height).Should(BeNumerically("~", 40, 1))
		})

		It("captures the ScrollOffset that satisfied HaveScrollOffset", func() {
			b.Run("scrollContainerTo(120)")
			var offset biloba.ScrollOffset
			Eventually(".scroller").Should(b.HaveScrollOffset(HaveField("Top", BeNumerically(">", 0))).Capture(&offset))
			Ω(offset.Top).Should(BeNumerically("==", 120))
			Ω(offset.MaxTop).Should(BeNumerically("~", 250, 1))
		})

		It("captures the float64 offset that satisfied HaveOffsetTopWithin", func() {
			var top float64
			Eventually("#s2").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically(">", 0)).Capture(&top))
			Ω(top).Should(BeNumerically("~", 300, 1))
		})

		It("captures the float64 offset that satisfied HaveOffsetLeftWithin", func() {
			// seeded with a sentinel: the offset here is legitimately ~0, and 0 is also float64's zero
			// value - so without the sentinel this spec would pass just as happily if Capture never wrote.
			left := -999.0
			Eventually("#s0").Should(b.HaveOffsetLeftWithin(".scroller", BeNumerically("~", 0, 1)).Capture(&left))
			Ω(left).Should(BeNumerically("~", 0, 1))
		})

		It("captures the float64 offset from the presence-only (zero-arg) form of HaveOffsetTopWithin", func() {
			var top float64
			Eventually("#s2").Should(b.HaveOffsetTopWithin(".scroller").Capture(&top))
			Ω(top).Should(BeNumerically("~", 300, 1))
		})

		It("captures the float64 offset from the presence-only (zero-arg) form of HaveOffsetLeftWithin", func() {
			left := -999.0
			Eventually("#s0").Should(b.HaveOffsetLeftWithin(".scroller").Capture(&left))
			Ω(left).Should(BeNumerically("~", 0, 1))
		})

		It("refuses to bridge one Biloba struct into a different one rather than silently half-filling", func() {
			// BoxDelta's fields are a strict SUBSET of Box's, so DisallowUnknownFields cannot catch this
			// and a JSON bridge would "succeed", leaving ClientWidth/ClientHeight at zero.  Capture names
			// both types instead of half-filling.
			var box biloba.Box
			failure := ""
			g := NewGomega(func(message string, callerSkip ...int) { failure = message })

			g.Expect("#span").Should(b.HaveGapBetween("#card", HaveField("Top", BeNumerically(">", -10000))).Capture(&box))

			Ω(failure).Should(ContainSubstring("biloba.BoxDelta"))
			Ω(failure).Should(ContainSubstring("biloba.Box"))
			Ω(box).Should(BeZero())
		})

		It("captures the BoxDelta that satisfied HaveGapBetween", func() {
			var delta biloba.BoxDelta
			Eventually("#span").Should(b.HaveGapBetween("#card", HaveField("CenterX", BeNumerically("~", 0, 1))).Capture(&delta))
			Ω(delta.Top).Should(BeNumerically("~", 20, 1))
			Ω(delta.Width).Should(BeNumerically("~", -100, 1))
		})

		It("captures the BoxDelta from the presence-only (zero-arg) form of HaveGapBetween", func() {
			var delta biloba.BoxDelta
			Eventually("#span").Should(b.HaveGapBetween("#card").Capture(&delta))
			Ω(delta.Top).Should(BeNumerically("~", 20, 1))
			Ω(delta.Width).Should(BeNumerically("~", -100, 1))
		})

		It("captures the number that satisfied HaveComputedStyleNumeric", func() {
			var width float64
			Eventually("#hero").Should(b.HaveComputedStyleNumeric("width", BeNumerically(">", 50)).Capture(&width))
			Ω(width).Should(BeNumerically("~", 100, 1))
		})

		It("decodes the captured number into the Go type you asked for", func() {
			// z-index is observed as float64(7); capturing into an *int must land 7, not float64(7)
			var z int
			Expect("#rail").To(b.HaveComputedStyleNumeric("z-index", 7).Capture(&z))
			Ω(z).Should(Equal(7))
		})

		It("does not capture when the matcher does not match", func() {
			var box biloba.Box
			m := b.HaveBoundingBox(HaveField("Width", BeNumerically("==", 999))).Capture(&box)
			match, err := m.Match("#hero")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(match).Should(BeFalse())
			Ω(box).Should(Equal(biloba.Box{}))

			// and the failure message is unchanged by the presence of a capture target
			Ω(m.FailureMessage("#hero")).Should(ContainSubstring("HaveBoundingBox for #hero:"))
		})

		It("does not capture under ShouldNot", func() {
			var box biloba.Box
			var width float64
			Ω("#hero").ShouldNot(b.HaveBoundingBox(HaveField("Width", BeNumerically("==", 999))).Capture(&box))
			Ω("#hero").ShouldNot(b.HaveComputedStyleNumeric("width", BeNumerically("==", 999)).Capture(&width))
			Ω(box).Should(Equal(biloba.Box{}))
			Ω(width).Should(BeNumerically("==", 0))
		})

		It("fails fast - it does not wait out the timeout - when the capture target cannot hold the value", func() {
			// the observed value is a number; capturing it into a *string can never come true, so
			// Capture stops the poll immediately rather than burning the (long) timeout.
			var wrongType string
			failure := ""
			g := NewGomega(func(message string, callerSkip ...int) { failure = message })

			start := time.Now()
			g.Eventually("#rail").WithTimeout(time.Second * 10).WithPolling(time.Millisecond * 10).Should(b.HaveComputedStyleNumeric("z-index", 7).Capture(&wrongType))
			Ω(time.Since(start)).Should(BeNumerically("<", time.Second*2))

			Ω(failure).Should(ContainSubstring("Told to stop trying"))
			Ω(failure).Should(ContainSubstring("Capture cannot decode the observed value into *string"))
			Ω(wrongType).Should(BeEmpty())
		})
	})
})

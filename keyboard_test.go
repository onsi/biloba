package biloba_test

import (
	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Keyboard input", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/keyboard.html")
		Eventually("#hello").Should(b.Exist())
	})

	Describe("Type", func() {
		It("sends real keystrokes that fire keydown events (unlike SetValue)", func() {
			b.Type("#typewriter", "abc")
			Ω("#typewriter").Should(b.HaveValue("abc"))
			// the keydown listener recorded each key - proving real key events fired
			Ω(".key-event").Should(b.EachHaveInnerText(HaveExactElements("a", "b", "c")))
		})

		It("does NOT fire key events when using SetValue (contrast)", func() {
			b.SetValue("#typewriter", "abc")
			Ω("#typewriter").Should(b.HaveValue("abc"))
			Ω(".key-event").Should(b.HaveCount(0))
		})

		It("appends to existing input rather than replacing it", func() {
			b.SetValue("#typewriter", "foo")
			b.Type("#typewriter", "bar")
			Ω("#typewriter").Should(b.HaveValue("foobar"))
		})

		It("works as a matcher polled with Eventually", func() {
			Eventually("#typewriter").Should(b.Type("hi"))
			Ω("#typewriter").Should(b.HaveValue("hi"))
		})

		It("fails the spec when the element does not exist", func() {
			b.Type("#non-existing", "abc")
			ExpectFailures(ContainSubstring("Failed to type"))
		})

		It("fails the spec when the element is disabled", func() {
			b.Type("#disabled-input", "abc")
			ExpectFailures(ContainSubstring("Failed to type"))
		})

		It("does not match when the element is hidden", func() {
			match, err := b.Type("abc").Match("#hidden-input")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError(ContainSubstring("not visible")))
		})
	})

	Describe("SendKeys", func() {
		It("sends named keys to a selected element", func() {
			b.SendKeys("#typewriter", "x", biloba.Keys.Backspace, "y")
			Ω("#typewriter").Should(b.HaveValue("y"))
		})

		It("submits a form when Enter is sent into an input", func() {
			b.Type("#search-input", "gophers")
			b.SendKeys("#search-input", biloba.Keys.Enter)
			Eventually("#submitted").Should(b.HaveInnerText("submitted: gophers"))
		})

		It("sends keys to the currently focused element when no selector is provided", func() {
			// Type focuses #typewriter; the subsequent selector-less SendKeys must land there too
			b.Type("#typewriter", "hi")
			b.SendKeys(biloba.Keys.Backspace)
			Ω("#typewriter").Should(b.HaveValue("h"))
		})

		It("fails the spec when the selected element does not exist", func() {
			b.SendKeys("#non-existing", biloba.Keys.Enter)
			ExpectFailures(ContainSubstring("Failed to send keys"))
		})

		It("fails the spec when no keys are provided", func() {
			b.SendKeys()
			ExpectFailures(ContainSubstring("SendKeys requires at least one key"))
		})

		It("fails the spec when only modifiers (no key) are provided", func() {
			b.SendKeys(b.Shift())
			ExpectFailures(ContainSubstring("SendKeys requires at least one key"))
		})

		It("supports the expanded set of named keys", func() {
			b.SendKeys("#editor", biloba.Keys.F2)
			Ω("#last-combo").Should(b.HaveInnerText("F2"))
			b.SendKeys("#editor", biloba.Keys.Insert)
			Ω("#last-combo").Should(b.HaveInnerText("Insert"))
			// Space is printable, so assert it types into the textarea (innerText collapses whitespace)
			b.SendKeys("#editor", biloba.Keys.Space)
			Ω("#editor").Should(b.HaveValue(" "))
		})
	})

	Describe("Modifiers", func() {
		It("holds a single modifier down while sending a key", func() {
			b.SendKeys("#editor", biloba.Keys.Enter, b.Shift())
			Ω("#last-combo").Should(b.HaveInnerText("Shift+Enter"))
		})

		It("holds multiple modifiers down at once", func() {
			b.SendKeys("#editor", biloba.Keys.Enter, b.Meta(), b.Shift())
			// the fixture renders flags in Shift,Ctrl,Alt,Meta order regardless of how they're passed
			Ω("#last-combo").Should(b.HaveInnerText("Shift+Meta+Enter"))
		})

		It("accepts a modifier in any position", func() {
			b.SendKeys("#editor", b.Ctrl(), biloba.Keys.Enter)
			Ω("#last-combo").Should(b.HaveInnerText("Ctrl+Enter"))
		})

		It("holds modifiers down for the focused element when no selector is given", func() {
			b.Type("#editor", "x") // focuses the editor
			b.SendKeys(biloba.Keys.Enter, b.Meta())
			Ω("#last-combo").Should(b.HaveInnerText("Meta+Enter"))
		})

		It("holds modifiers down while Typing (e.g. select-all)", func() {
			b.Type("#editor", "a", b.Meta())
			Ω("#last-combo").Should(b.HaveInnerText("Meta+a"))
		})

		It("works through the Type matcher form", func() {
			Eventually("#editor").Should(b.Type("a", b.Meta()))
			Ω("#last-combo").Should(b.HaveInnerText("Meta+a"))
		})
	})
})

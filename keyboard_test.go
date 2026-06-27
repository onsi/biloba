package biloba_test

import (
	"time"

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

		It("mixes named keys in with text in the immediate selector form", func() {
			b.Type("#typewriter", "x", biloba.Keys.Backspace, "y")
			Ω("#typewriter").Should(b.HaveValue("y"))
		})

		It("submits a form when text and Enter are typed into an input", func() {
			b.Type("#search-input", "gophers", biloba.Keys.Enter)
			Eventually("#submitted").Should(b.HaveInnerText("submitted: gophers"))
		})

		It("sends a bare named key into a selected element (immediate selector form)", func() {
			b.Type("#search-input", "gophers")
			b.Type("#search-input", biloba.Keys.Enter) // selector + single Key => immediate
			Eventually("#submitted").Should(b.HaveInnerText("submitted: gophers"))
		})

		It("works as a matcher polled with Eventually", func() {
			Eventually("#typewriter").Should(b.Type("hi"))
			Ω("#typewriter").Should(b.HaveValue("hi"))
		})

		It("returns a matcher when the only payload is a named key", func() {
			b.Type("#search-input", "gophers")
			Eventually("#search-input").Should(b.Type(biloba.Keys.Enter)) // first arg is a Key => matcher
			Eventually("#submitted").Should(b.HaveInnerText("submitted: gophers"))
		})

		It("polls by default, timing out when the element never appears", func() {
			b.WithTimeout(100 * time.Millisecond).Type("#non-existing", "abc")
			ExpectFailures(ContainSubstring("Timed out after"))
		})

		It("polls by default, timing out when the element stays disabled", func() {
			b.WithTimeout(100 * time.Millisecond).Type("#disabled-input", "abc")
			ExpectFailures(ContainSubstring("Timed out after"))
		})

		It("fails fast when Immediate() is set", func() {
			b.Immediate().Type("#non-existing", "abc")
			ExpectFailures(ContainSubstring("could not find DOM element"))
		})

		It("does not match when the element is hidden", func() {
			match, err := b.Type("abc").Match("#hidden-input")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError(ContainSubstring("not visible")))
		})

		It("fails the spec when no text or keys are provided", func() {
			b.Type()
			ExpectFailures(ContainSubstring("Type requires text or keys"))
		})

		It("fails the spec when a poll-config knob is set on the bare matcher form", func() {
			b.WithTimeout(time.Second).Type("hello")
			ExpectFailures(SatisfyAll(ContainSubstring("Type(...) returns a matcher"), ContainSubstring("WithTimeout")))
		})
	})

	Describe("SendKeysToWindowImmediately", func() {
		It("sends keys to the currently focused element", func() {
			// Type focuses #typewriter; the subsequent focus-free send must land there too
			b.Type("#typewriter", "hi")
			b.SendKeysToWindowImmediately(biloba.Keys.Backspace)
			Ω("#typewriter").Should(b.HaveValue("h"))
		})

		It("mixes text and named keys into the focused element", func() {
			b.Type("#typewriter", "hi")
			// "hi" + "x" => "hix", Backspace removes the "x" => "hi", + "y" => "hiy"
			b.SendKeysToWindowImmediately("x", biloba.Keys.Backspace, "y")
			Ω("#typewriter").Should(b.HaveValue("hiy"))
		})

		It("fires document-level hotkey handlers when nothing is focused", func() {
			b.Blur("#typewriter") // ensure nothing holds focus
			Ω("#global-hotkey").Should(b.HaveInnerText("none"))
			b.SendKeysToWindowImmediately(biloba.Keys.Escape)
			Ω("#global-hotkey").Should(b.HaveInnerText("Escape"))
		})

		It("fails the spec when no keys are provided", func() {
			b.SendKeysToWindowImmediately()
			ExpectFailures(ContainSubstring("SendKeysToWindowImmediately requires at least one key"))
		})

		It("fails the spec when only modifiers (no key) are provided", func() {
			b.SendKeysToWindowImmediately(b.Shift())
			ExpectFailures(ContainSubstring("SendKeysToWindowImmediately requires at least one key"))
		})

		It("supports the expanded set of named keys", func() {
			b.Focus("#editor")
			b.SendKeysToWindowImmediately(biloba.Keys.F2)
			Ω("#last-combo").Should(b.HaveInnerText("F2"))
			b.SendKeysToWindowImmediately(biloba.Keys.Insert)
			Ω("#last-combo").Should(b.HaveInnerText("Insert"))
			// Space is printable, so assert it types into the textarea (innerText collapses whitespace)
			b.SendKeysToWindowImmediately(biloba.Keys.Space)
			Ω("#editor").Should(b.HaveValue(" "))
		})
	})

	Describe("Modifiers", func() {
		It("holds a single modifier down while typing a key", func() {
			b.Type("#editor", biloba.Keys.Enter, b.Shift())
			Ω("#last-combo").Should(b.HaveInnerText("Shift+Enter"))
		})

		It("holds multiple modifiers down at once", func() {
			b.Type("#editor", biloba.Keys.Enter, b.Meta(), b.Shift())
			// the fixture renders flags in Shift,Ctrl,Alt,Meta order regardless of how they're passed
			Ω("#last-combo").Should(b.HaveInnerText("Shift+Meta+Enter"))
		})

		It("accepts a modifier in any position", func() {
			b.Type("#editor", b.Ctrl(), biloba.Keys.Enter)
			Ω("#last-combo").Should(b.HaveInnerText("Ctrl+Enter"))
		})

		It("holds modifiers down for the focused element via SendKeysToWindowImmediately", func() {
			b.Type("#editor", "x") // focuses the editor
			b.SendKeysToWindowImmediately(biloba.Keys.Enter, b.Meta())
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

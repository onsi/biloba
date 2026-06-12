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
	})
})

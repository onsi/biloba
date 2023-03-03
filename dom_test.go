package biloba_test

import (
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("DOM manipulators and matchers", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())
	})

	Describe("exist", func() {
		It("does not match when the dom node does not exist", func() {
			Ω("#non-existing").ShouldNot(b.Exist())
		})

		It("does not match when the dom node exists", func() {
			Ω("#hello").Should(b.Exist())
			Ω("#hidden-parent").Should(b.Exist())
		})

		It("matches eventually if a dom node pops into existence", func() {
			Consistently("#say-when").ShouldNot(b.Exist())
			b.Run("bilobaSaysWhen()")
			Eventually("#say-when").Should(b.Exist())
		})

		It("errors if the selector is malformed", func() {
			match, err := b.Exist().Match(b.XPath("//[blarg]"))
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError(ContainSubstring("'//[blarg]' is not a valid XPath expression")))

		})
	})

	Describe("HasElement()", func() {
		It("return true when the element exists", func() {
			Ω(b.HasElement("#hello")).Should(BeTrue())
			Ω(b.HasElement(b.XPath().WithID("hello"))).Should(BeTrue())
		})

		It("returns true when the element is hidden", func() {
			Ω(b.HasElement("#hidden-parent")).Should(BeTrue())
		})

		It("returns false when the element does not exist", func() {
			Ω(b.HasElement("#non-existing")).Should(BeFalse())
		})

		It("errors when the selector is malformed", func() {
			b.HasElement(b.XPath("//[blarg]"))
			ExpectFailures(ContainSubstring("'//[blarg]' is not a valid XPath expression"))
		})
	})

	Describe("BeVisible", func() {
		It("matches when the element is visible", func() {
			Ω("#hello").Should(b.BeVisible())
		})

		It("does not match when the element is hidden", func() {
			Ω("#hidden-parent").ShouldNot(b.BeVisible())
		})

		It("does not match when a child element is hidden", func() {
			Ω("#hidden-child").ShouldNot(b.BeVisible())
			Ω("#fixed-hidden-child").ShouldNot(b.BeVisible())
			Ω("#hidden-button").ShouldNot(b.BeVisible())
		})

		It("errors when the element does not exist", func() {
			match, err := b.BeVisible().Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM node matching selector: #non-existing"))
		})
	})

	Describe("BeEnabled", func() {
		It("matches when the element is enabled", func() {
			Ω("#increment").Should(b.BeEnabled())
		})

		It("matches when the element is enabled even if it is hidden", func() {
			Ω("#hidden-button").ShouldNot(b.BeVisible())
			Ω("#hidden-button").Should(b.BeEnabled())
		})

		It("does not match when the element is disabled", func() {
			Ω("#decrement").ShouldNot(b.BeEnabled())
		})

		It("errors when the element does not exist", func() {
			match, err := b.BeEnabled().Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM node matching selector: #non-existing"))
		})
	})

	Describe("InnerText", func() {
		It("returns the InnerText of the element", func() {
			Ω(b.InnerText("#hello")).Should(Equal("Hello Biloba!"))
			Ω(b.InnerText("#hidden-child")).Should(Equal("Can't see me!"))
			Ω(b.InnerText("#list")).Should(Equal("First Things\nSecond Things\nThird Things"))
		})

		It("auto-fails if the element does not exist", func() {
			Ω(b.InnerText("#non-existing")).Should(Equal(""))
			ExpectFailures("Failed to get inner text:\ncould not find DOM node matching selector: #non-existing")
		})
	})

	Describe("HaveInnerText", func() {
		It("matches if the element in question has the specified inner text", func() {
			Ω("#hello").Should(b.HaveInnerText("Hello Biloba!"))
			Ω("#hidden-child").Should(b.HaveInnerText("Can't see me!"))
			Ω("#hello").ShouldNot(b.HaveInnerText("nope"))
		})

		It("works with matchers", func() {
			Ω("#list").Should(b.HaveInnerText(ContainSubstring("Second Things")))
		})

		It("has a reasonable failure message", func() {
			matcher := b.HaveInnerText("Hello")
			match, err := matcher.Match("#hello")
			Ω(match).Should(BeFalse())
			Ω(err).ShouldNot(HaveOccurred())
			Ω(matcher.FailureMessage("#hello")).Should(Equal("HaveInnerText for #hello:\nExpected\n    <string>: Hello Biloba!\nto equal\n    <string>: Hello"))
			Ω(matcher.NegatedFailureMessage("#hello")).Should(Equal("HaveInnerText for #hello:\nExpected\n    <string>: Hello Biloba!\nnot to equal\n    <string>: Hello"))

			nestedMatcher := b.HaveInnerText(ContainSubstring("Fourth Things"))
			match, err = nestedMatcher.Match("#list")
			Ω(match).Should(BeFalse())
			Ω(err).ShouldNot(HaveOccurred())
			Ω(nestedMatcher.FailureMessage("#list")).Should(Equal("HaveInnerText for #list:\nExpected\n    <string>: First Things\n    Second Things\n    Third Things\nto contain substring\n    <string>: Fourth Things"))
		})

		It("errors if the element does not exist", func() {
			match, err := b.HaveInnerText("").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM node matching selector: #non-existing"))
		})
	})

	Describe("IsChecked", func() {
		It("matches if the checkbox is checked", func() {
			Ω(b.IsChecked("#red")).Should(BeTrue())
			Ω(b.IsChecked("#blue")).Should(BeFalse())
			Ω(b.IsChecked("#yellow")).Should(BeFalse())
		})

		It("auto-fails if the element does not exist", func() {
			Ω(b.IsChecked("#non-existing")).Should(BeFalse())
			ExpectFailures("Failed to determine if checked:\ncould not find DOM node matching selector: #non-existing")
		})
	})

	Describe("BeChecked", func() {
		It("matches if the checkbox is checked", func() {
			Ω("#red").Should(b.BeChecked())
			Ω("#blue").ShouldNot(b.BeChecked())
			Ω("#yellow").ShouldNot(b.BeChecked())
		})

		It("errors if the checkbox does not exist", func() {
			match, err := b.BeChecked().Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM node matching selector: #non-existing"))
		})
	})

	Describe("SetChecked", func() {
		BeforeEach(func() {
			Eventually("#checked-color").Should(b.HaveInnerText("red"))
		})

		Context("when called directly", func() {
			It("sets the checkboxes correctly", func() {
				b.SetChecked("#red", true)
				Ω("#red").Should(b.BeChecked())
				Ω("#checked-color").Should(b.HaveInnerText("red"))

				b.SetChecked("#red", false)
				Ω("#red").ShouldNot(b.BeChecked())
				Ω("#checked-color").Should(b.HaveInnerText("black"))

				b.SetChecked("#blue", true)
				Ω("#blue").Should(b.BeChecked())
				Ω("#checked-color").Should(b.HaveInnerText("blue"))

				b.SetChecked("#red", true)
				Ω("#red").Should(b.BeChecked())
				Ω("#checked-color").Should(b.HaveInnerText("purple"))
			})

			It("auto-fails if the element does not exist", func() {
				b.SetChecked("#non-existing", true)
				ExpectFailures("Failed to set checked:\ncould not find DOM node matching selector: #non-existing")
			})

			It("auto-fails if the element is not visible", func() {
				b.SetChecked("#green", true)
				ExpectFailures("Failed to set checked:\nDOM node is not visible: #green")
				Ω("#checked-color").Should(b.HaveInnerText("red"))
			})

			It("auto-fails if the element is not enabled", func() {
				b.SetChecked("#yellow", true)
				ExpectFailures("Failed to set checked:\nDOM node is not enabled: #yellow")
				Ω("#checked-color").Should(b.HaveInnerText("red"))
			})
		})

		Context("when used as a matcher", func() {
			It("sets the checkboxes correctly", func() {
				Ω("#red").Should(b.SetChecked(true))
				Ω("#red").Should(b.BeChecked())
				Ω("#checked-color").Should(b.HaveInnerText("red"))

				Ω("#red").Should(b.SetChecked(false))
				Ω("#red").ShouldNot(b.BeChecked())
				Ω("#checked-color").Should(b.HaveInnerText("black"))

				Ω("#blue").Should(b.SetChecked(true))
				Ω("#blue").Should(b.BeChecked())
				Ω("#checked-color").Should(b.HaveInnerText("blue"))

				Ω("#red").Should(b.SetChecked(true))
				Ω("#red").Should(b.BeChecked())
				Ω("#checked-color").Should(b.HaveInnerText("purple"))
			})

			It("retries when called in an eventually", func() {
				Ω("#yellow").ShouldNot(Or(b.BeChecked(), b.BeEnabled()))
				Ω("#checked-color").Should(b.HaveInnerText("red"))
				b.Run("enableYellow()")
				Eventually("#yellow").Should(b.SetChecked(true))
				Ω("#checked-color").Should(b.HaveInnerText("yellow"))
			})

			It("returns an error when the element does not exist", func() {
				match, err := b.SetChecked(true).Match("#non-existing")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("could not find DOM node matching selector: #non-existing"))
			})

			It("returns an error when the element is not visible", func() {
				match, err := b.SetChecked(true).Match("#green")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("DOM node is not visible: #green"))
			})

			It("returns an error when the element is not enabled", func() {
				match, err := b.SetChecked(true).Match("#yellow")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("DOM node is not enabled: #yellow"))
			})
		})
	})

	Describe("working with radio buttons", func() {
		It("can set and read the checked property correctly", func() {
			Ω("input[name='appliances'][value='toaster']").Should(b.BeChecked())
			b.SetChecked("input[name='appliances'][value='microwave']", true)
			Ω("input[name='appliances'][value='microwave']").Should(b.BeChecked())
			Ω("input[name='appliances'][value='toaster']").ShouldNot(b.BeChecked())
			b.Click("input[name='appliances'][value='stove']")
			Ω("input[name='appliances'][value='microwave']").ShouldNot(b.BeChecked())
			Ω("input[name='appliances'][value='stove']").Should(b.BeChecked())
		})
	})

	Describe("GetValue", func() {
		It("returns the value associated with the input", func() {
			Ω(b.GetValue("#hidden-text-input")).Should(Equal("my-hidden-value"))
			Ω(b.GetValue("#counter-input")).Should(Equal("0"))
			Ω(b.GetValue("#disabled-text-input")).Should(Equal("i'm off"))
		})

		It("auto-fails if the element does not exist", func() {
			Ω(b.GetValue("#non-existing")).Should(BeEmpty())
			ExpectFailures("Failed to get value:\ncould not find DOM node matching selector: #non-existing")
		})
	})

	Describe("HaveValue", func() {
		It("matches if returned value matches", func() {
			Ω("#hidden-text-input").Should(b.HaveValue("my-hidden-value"))
			Ω("#counter-input").Should(b.HaveValue("0"))
			Ω("#disabled-text-input").Should(b.HaveValue("i'm off"))
		})

		It("works with nested matchers", func() {
			Ω("#counter-input").Should(b.HaveValue(WithTransform(strconv.Atoi, BeNumerically("==", 0))))
			Ω("#counter-input").ShouldNot(b.HaveValue(WithTransform(strconv.Atoi, BeNumerically("==", 10))))

			matcher := b.HaveValue(WithTransform(strconv.Atoi, BeNumerically("==", 1)))
			match, err := matcher.Match("#counter-input")
			Ω(err).ShouldNot(HaveOccurred())
			Ω(match).Should(BeFalse())
			Ω(matcher.FailureMessage("#counter-input")).Should(Equal("HaveValue for #counter-input:\nExpected\n    <int>: 0\nto be ==\n    <int>: 1"))
		})

		It("errors if the DOM node does not exist", func() {
			match, err := b.HaveValue("foo").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM node matching selector: #non-existing"))
		})
	})
	Describe("SetValue", func() {
		Context("when called directly", func() {
			It("sets the value correctly", func() {
				Eventually("#text-input-mirror").Should(b.HaveInnerText("initial value"))
				Ω("#text-input").Should(b.HaveValue("initial value"))
				b.SetValue("#text-input", "new value")
				Ω("#text-input").Should(b.HaveValue("new value"))
				Ω("#text-input-mirror").Should(b.HaveInnerText("new value"))

				Ω("#counter-input").Should(b.HaveValue("0"))
				b.SetValue("#counter-input", 3)
				Ω("#counter-input").Should(b.HaveValue("3"))
			})

			It("auto-fails if the element does not exist", func() {
				b.SetValue("#non-existing", "foo")
				ExpectFailures("Failed to set value:\ncould not find DOM node matching selector: #non-existing")
			})

			It("auto-fails if the element is not visible", func() {
				b.SetValue("#hidden-text-input", "foo")
				ExpectFailures("Failed to set value:\nDOM node is not visible: #hidden-text-input")
				Ω("#hidden-text-input").Should(b.HaveValue("my-hidden-value"))
			})

			It("auto-fails if the element is not enabled", func() {
				b.SetValue("#disabled-text-input", "foo")
				ExpectFailures("Failed to set value:\nDOM node is not enabled: #disabled-text-input")
				Ω("#disabled-text-input").Should(b.HaveValue("i'm off"))
			})
		})

		Context("when used as a matcher", func() {
			It("sets the values correctly", func() {
				Eventually("#text-input-mirror").Should(b.HaveInnerText("initial value"))
				Ω("#text-input").Should(b.HaveValue("initial value"))
				Ω("#text-input").Should(b.SetValue("new value"))
				Ω("#text-input").Should(b.HaveValue("new value"))
				Ω("#text-input-mirror").Should(b.HaveInnerText("new value"))

				Ω("#counter-input").Should(b.HaveValue("0"))
				Ω("#counter-input").Should(b.SetValue(3))
				Ω("#counter-input").Should(b.HaveValue("3"))
			})

			It("retries when called in an eventually", func() {
				Eventually("#disabled-text-input-mirror").Should(b.HaveInnerText("i'm off"))
				Ω("#disabled-text-input").ShouldNot(b.BeEnabled())
				b.Run("enableTextInput()")
				Eventually("#disabled-text-input").Should(b.SetValue("i'm on"))
				Ω("#disabled-text-input").Should(b.HaveValue("i'm on"))
				Ω("#disabled-text-input-mirror").Should(b.HaveInnerText("i'm on"))
			})

			It("returns an error when the element does not exist", func() {
				match, err := b.SetValue("foo").Match("#non-existing")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("could not find DOM node matching selector: #non-existing"))
			})

			It("returns an error when the element is not visible", func() {
				match, err := b.SetValue("foo").Match("#hidden-text-input")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("DOM node is not visible: #hidden-text-input"))
			})

			It("returns an error when the element is not enabled", func() {
				match, err := b.SetValue("foo").Match("#disabled-text-input")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("DOM node is not enabled: #disabled-text-input"))
			})
		})
	})

	Describe("HaveClass", func() {
		It("matches if the elements has the class", func() {
			Ω("#hidden-parent").Should(b.HaveClass("hidden"))
			Ω("#classy").Should(b.HaveClass("dog"))
			Ω("#classy").Should(b.HaveClass("cat"))
			Ω("#classy").ShouldNot(b.HaveClass("fish"))
		})

		It("returns an error when the element does not exist", func() {
			match, err := b.HaveClass("foo").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM node matching selector: #non-existing"))
		})
	})

	Describe("GetProperty", func() {
		It("returns properties defined on the node", func() {
			Ω(b.GetProperty(".notice", "count")).Should(Equal(3.0))
			Ω(b.GetProperty(".notice", "tagName")).Should(Equal("DIV"))
			Ω(b.GetProperty(".notice", "flavor")).Should(Equal("strawberry"))
			Ω(b.GetProperty(".notice", "offsetWidth")).Should(Equal(200.0))
			Ω(b.GetProperty(".notice", "innerText")).Should(Equal("Some Text"))
			Ω(b.GetProperty(".notice", "innerText")).Should(Equal("Some Text"))
			Ω(b.GetProperty(".notice", "hidden")).Should(Equal(false))
			Ω(b.GetProperty(".notice", "classList")).Should(HaveKeyWithValue("0", "notice"))
			Ω(b.GetProperty("#hidden-text-input", "value")).Should(Equal("my-hidden-value"))
		})

		It("returns an error when the element does not exist", func() {
			b.GetProperty("#non-existing", "tagName")
			ExpectFailures("Failed to get property tagName:\ncould not find DOM node matching selector: #non-existing")
		})

		It("returns an error when the element does not have the property in question", func() {
			b.GetProperty(".notice", "floop")
			ExpectFailures("Failed to get property floop:\nDOM node does not have property floop: .notice")
		})
	})

	Describe("HaveProperty", func() {
		It("returns properties defined on the node", func() {
			Ω(".notice").Should(b.HaveProperty("count", 3.0))
			Ω(".notice").Should(b.HaveProperty("tagName", "DIV"))
			Ω(".notice").Should(b.HaveProperty("flavor", "strawberry"))
			Ω(".notice").Should(b.HaveProperty("offsetWidth", 200.0))
			Ω(".notice").Should(b.HaveProperty("innerText", "Some Text"))
			Ω(".notice").Should(b.HaveProperty("innerText", "Some Text"))
			Ω(".notice").Should(b.HaveProperty("hidden", false))
			Ω(".notice").Should(b.HaveProperty("classList", HaveKeyWithValue("0", "notice")))
			Ω("#hidden-text-input").Should(b.HaveProperty("value", "my-hidden-value"))
		})

		It("returns an error when the element does not exist", func() {
			match, err := b.HaveProperty("tagName", "any").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM node matching selector: #non-existing"))
		})

		It("returns an error when the element does not have the property in question", func() {
			match, err := b.HaveProperty("floop", "any").Match(".notice")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("DOM node does not have property floop: .notice"))
		})
	})

	Describe("Click", func() {
		Context("when called directly", func() {
			It("...clicks things", func() {
				b.Click("#increment")
				Ω("#counter-input").Should(b.HaveValue("1"))
				b.Click("#increment")
				Ω("#counter-input").Should(b.HaveValue("2"))
				b.Click("#decrement")
				Ω("#counter-input").Should(b.HaveValue("1"))

				b.Click("#blue")
				Ω("#checked-color").Should(b.HaveInnerText("purple"))
			})

			It("auto-fails if the element does not exist", func() {
				b.Click("#non-existing")
				ExpectFailures("Failed to click:\ncould not find DOM node matching selector: #non-existing")
			})

			It("auto-fails if the element is not visible", func() {
				b.Click("#hidden-button")
				ExpectFailures("Failed to click:\nDOM node is not visible: #hidden-button")
			})

			It("auto-fails if the element is not enabled", func() {
				b.Click("#decrement")
				ExpectFailures("Failed to click:\nDOM node is not enabled: #decrement")
			})
		})

		Context("when used as a matcher", func() {
			It("clicks just once", func() {
				Eventually("#increment").Should(b.Click())
				Ω("#counter-input").Should(b.HaveValue("1"))
			})

			It("retries when called in an eventually", func() {
				go func() {
					<-time.After(time.Millisecond * 200)
					b.Click("#increment")
					b.Click("#increment")
				}()
				Eventually("#decrement").Should(b.Click())
				Eventually("#counter-input").Should(b.HaveValue("1"))
			})

			It("returns an error when the element does not exist", func() {
				match, err := b.Click().Match("#non-existing")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("could not find DOM node matching selector: #non-existing"))
			})

			It("returns an error when the element is not visible", func() {
				match, err := b.Click().Match("#hidden-button")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("DOM node is not visible: #hidden-button"))
			})

			It("returns an error when the element is not enabled", func() {
				match, err := b.Click().Match("#decrement")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("DOM node is not enabled: #decrement"))
			})
		})
	})

	Describe("using xpath selectors", func() {
		It("uses the first node returned by the xpath selector", func() {
			b.Click(b.XPath("button").WithText("Increment"))
			Ω("#counter-input").Should(b.HaveValue("1"))
			Ω(b.XPath("button").WithID("decrement")).Should(b.Click())
			Ω(b.XPath().WithID("counter").FollowingSibling("input")).Should(b.HaveValue("0"))
			b.Click(b.XPath("button").WithText("Increment"))
			Ω(b.XPath().WithID("counter-input"))
		})

		It("errors when the node does not exist", func() {
			b.Click(b.XPath("button").WithText("nope"))
			ExpectFailures("Failed to click:\ncould not find DOM node matching selector: //button[text()='nope']")
		})
	})
})

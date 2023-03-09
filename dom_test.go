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
		It("does not match when the dom element does not exist", func() {
			Ω("#non-existing").ShouldNot(b.Exist())
		})

		It("does not match when the dom element exists", func() {
			Ω("#hello").Should(b.Exist())
			Ω("#hidden-parent").Should(b.Exist())
		})

		It("matches eventually if a dom element pops into existence", func() {
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

	Describe("counting elements", func() {
		It("count returns the number of elements", func() {
			Ω(b.Count("#non-existing")).Should(Equal(0))
			Ω(b.Count("#hello")).Should(Equal(1))
			Ω(b.Count(b.XPath("div").WithID("hidden-parent").Descendant())).Should(Equal(4))
			Ω(b.Count("input[type='radio']")).Should(Equal(10))
		})

		It("HaveCount does the same, as a matcher", func() {
			Ω("#non-existing").Should(b.HaveCount(0))
			Ω("#hello").Should(b.HaveCount(1))
			Ω(b.XPath("div").WithID("hidden-parent").Descendant()).Should(b.HaveCount(4))
			Ω("input[type='radio']").Should(b.HaveCount(10))

			matcher := b.HaveCount(BeNumerically("<", 8))
			match, err := matcher.Match("input[type='radio']")
			Ω(match).Should(BeFalse())
			Ω(err).Should(BeNil())
			Ω(matcher.FailureMessage("input[type='radio']")).Should(Equal("HaveCount for input[type='radio']:\nExpected\n    <int>: 10\nto be <\n    <int>: 8"))
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
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
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
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
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
			ExpectFailures("Failed to get property \"innerText\":\ncould not find DOM element matching selector: #non-existing")
		})
	})

	Describe("InnerTextForEach", func() {
		It("returns the InnerText of the element", func() {
			Ω(b.InnerTextForEach(b.XPath().WithID("party").Descendant("optgroup").WithAttr("label", "Heros").Descendant("option"))).Should(HaveExactElements("Luke", "Leia", "Han", "Obi-Wan"))

			Ω(b.InnerTextForEach("#list li")).Should(HaveExactElements("First Things", "Second Things", "Third Things"))
		})

		It("returns an empty slice if no elements exist", func() {
			Ω(b.InnerTextForEach(".non-existing")).Should(BeEmpty())
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
			Ω(matcher.FailureMessage("#hello")).Should(Equal("HaveProperty \"innerText\" for #hello:\nExpected\n    <string>: Hello Biloba!\nto equal\n    <string>: Hello"))
			Ω(matcher.NegatedFailureMessage("#hello")).Should(Equal("HaveProperty \"innerText\" for #hello:\nExpected\n    <string>: Hello Biloba!\nnot to equal\n    <string>: Hello"))

			nestedMatcher := b.HaveInnerText(ContainSubstring("Fourth Things"))
			match, err = nestedMatcher.Match("#list")
			Ω(match).Should(BeFalse())
			Ω(err).ShouldNot(HaveOccurred())
			Ω(nestedMatcher.FailureMessage("#list")).Should(Equal("HaveProperty \"innerText\" for #list:\nExpected\n    <string>: First Things\n    Second Things\n    Third Things\nto contain substring\n    <string>: Fourth Things"))
		})

		It("errors if the element does not exist", func() {
			match, err := b.HaveInnerText("").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
		})
	})

	Describe("EachHaveInnerText", func() {
		It("matches if the elements in question have the specified inner text", func() {
			Ω(b.XPath().WithID("party").Descendant("optgroup").WithAttr("label", "Heros").Descendant("option")).Should(b.EachHaveInnerText("Luke", "Leia", "Han", "Obi-Wan"))

			Ω("#list li").Should(b.EachHaveInnerText(ConsistOf("Second Things", "First Things", "Third Things")))
			Ω("#list li").Should(b.EachHaveInnerText(ContainElement("Second Things")))
			Ω("#non-existing").Should(b.EachHaveInnerText())
		})
	})

	Describe("Working with inputs that honor value", func() {
		Describe("GetValue", func() {
			It("returns the value associated with the input", func() {
				Ω(b.GetValue("#hidden-text-input")).Should(Equal("my-hidden-value"))
				Ω(b.GetValue("#counter-input")).Should(Equal("0"))
				Ω(b.GetValue("#disabled-text-input")).Should(Equal("i'm off"))
				Ω(b.GetValue("#text-area")).Should(Equal("Something long"))
				Ω(b.GetValue("#droid")).Should(Equal("r2d2"))
			})

			It("auto-fails if the element does not exist", func() {
				Ω(b.GetValue("#non-existing")).Should(BeNil())
				ExpectFailures("Failed to get value:\ncould not find DOM element matching selector: #non-existing")
			})
		})

		Describe("HaveValue", func() {
			It("matches if returned value matches", func() {
				Ω("#hidden-text-input").Should(b.HaveValue("my-hidden-value"))
				Ω("#counter-input").Should(b.HaveValue("0"))
				Ω("#disabled-text-input").Should(b.HaveValue("i'm off"))
				Ω("#text-area").Should(b.HaveValue("Something long"))
				Ω("#droid").Should(b.HaveValue("r2d2"))
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

			It("errors if the DOM element does not exist", func() {
				match, err := b.HaveValue("foo").Match("#non-existing")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
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

					b.SetValue("#text-area", "Something even longer")
					Ω("#text-area").Should(b.HaveValue("Something even longer"))

					b.SetValue("#droid", "bb8")
					Ω("#droid").Should(b.HaveValue("bb8"))
					Ω(b.XPath("option").WithAttr("value", "bb8")).Should(b.HaveProperty("selected", BeTrue()))
				})

				It("auto-fails if the element does not exist", func() {
					b.SetValue("#non-existing", "foo")
					ExpectFailures("Failed to set value:\ncould not find DOM element matching selector: #non-existing")
				})

				It("auto-fails if the element is not visible", func() {
					b.SetValue("#hidden-text-input", "foo")
					ExpectFailures("Failed to set value:\nDOM element is not visible: #hidden-text-input")
					Ω("#hidden-text-input").Should(b.HaveValue("my-hidden-value"))
				})

				It("auto-fails if the element is not enabled", func() {
					b.SetValue("#disabled-text-input", "foo")
					ExpectFailures("Failed to set value:\nDOM element is not enabled: #disabled-text-input")
					Ω("#disabled-text-input").Should(b.HaveValue("i'm off"))
				})

				It("fails if attempting to set the value of a select input to an option that does not exist", func() {
					b.SetValue("#droid", "grogu")
					ExpectFailures("Failed to set value:\nSelect input does not have option with value \"grogu\": #droid")
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

					Ω("#text-area").Should(b.SetValue("Something even longer"))
					Ω("#text-area").Should(b.HaveValue("Something even longer"))

					Ω("#droid").Should(b.SetValue("bb8"))
					Ω("#droid").Should(b.HaveValue("bb8"))
					Ω(b.XPath("option").WithAttr("value", "bb8")).Should(b.HaveProperty("selected", BeTrue()))
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
					Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
				})

				It("returns an error when the element is not visible", func() {
					match, err := b.SetValue("foo").Match("#hidden-text-input")
					Ω(match).Should(BeFalse())
					Ω(err).Should(MatchError("DOM element is not visible: #hidden-text-input"))
				})

				It("returns an error when the element is not enabled", func() {
					match, err := b.SetValue("foo").Match("#disabled-text-input")
					Ω(match).Should(BeFalse())
					Ω(err).Should(MatchError("DOM element is not enabled: #disabled-text-input"))
				})

				It("fails if attempting to set the value of a select input to an option that does not exist", func() {
					match, err := b.SetValue("grogu").Match("#droid")
					Ω(match).Should(BeFalse())
					Ω(err).Should(MatchError("Select input does not have option with value \"grogu\": #droid"))
				})
			})
		})
	})

	Describe("Working with Checkboxes", func() {
		BeforeEach(func() {
			Eventually("#checked-color").Should(b.HaveInnerText("red"))
		})

		It("returns booleans", func() {
			Ω(b.GetValue("#red")).Should(BeTrue())
			Ω(b.GetValue("#blue")).Should(BeFalse())
		})

		Context("when setting values directly", func() {
			It("sets the checkboxes correctly", func() {
				b.SetValue("#red", true)
				Ω("#red").Should(b.HaveValue(true))
				Ω("#checked-color").Should(b.HaveInnerText("red"))

				b.SetValue("#red", false)
				Ω("#red").Should(b.HaveValue(false))
				Ω("#checked-color").Should(b.HaveInnerText("black"))

				b.SetValue("#blue", true)
				Ω("#blue").Should(b.HaveValue(true))
				Ω("#checked-color").Should(b.HaveInnerText("blue"))

				b.SetValue("#red", true)
				Ω("#red").Should(b.HaveValue(true))
				Ω("#checked-color").Should(b.HaveInnerText("purple"))
			})

			It("auto-fails if the element is not visible", func() {
				b.SetValue("#green", true)
				ExpectFailures("Failed to set value:\nDOM element is not visible: #green")
				Ω("#checked-color").Should(b.HaveInnerText("red"))
			})

			It("auto-fails if the element is not enabled", func() {
				b.SetValue("#yellow", true)
				ExpectFailures("Failed to set value:\nDOM element is not enabled: #yellow")
				Ω("#checked-color").Should(b.HaveInnerText("red"))
			})

			It("fails if not provided a boolean value", func() {
				b.SetValue("#red", "true")
				ExpectFailures("Failed to set value:\nCheckboxes only accept boolean values: #red")
			})
		})

		Context("when setting values as a matcher", func() {
			It("sets the checkboxes correctly", func() {
				Ω("#red").Should(b.SetValue(true))
				Ω("#red").Should(b.HaveValue(true))
				Ω("#checked-color").Should(b.HaveInnerText("red"))

				Ω("#red").Should(b.SetValue(false))
				Ω("#red").Should(b.HaveValue(false))
				Ω("#checked-color").Should(b.HaveInnerText("black"))

				Ω("#blue").Should(b.SetValue(true))
				Ω("#blue").Should(b.HaveValue(true))
				Ω("#checked-color").Should(b.HaveInnerText("blue"))

				Ω("#red").Should(b.SetValue(true))
				Ω("#red").Should(b.HaveValue(true))
				Ω("#checked-color").Should(b.HaveInnerText("purple"))
			})

			It("retries when called in an eventually", func() {
				Ω("#yellow").ShouldNot(Or(b.HaveValue(true), b.BeEnabled()))
				Ω("#checked-color").Should(b.HaveInnerText("red"))
				b.Run("enableYellow()")
				Eventually("#yellow").Should(b.SetValue(true))
				Ω("#checked-color").Should(b.HaveInnerText("yellow"))
			})

			It("returns an error when the element is not visible", func() {
				match, err := b.SetValue(true).Match("#green")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("DOM element is not visible: #green"))
			})

			It("returns an error when the element is not enabled", func() {
				match, err := b.SetValue(true).Match("#yellow")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("DOM element is not enabled: #yellow"))
			})

			It("returns an error when not provided a boolean value", func() {
				match, err := b.SetValue("true").Match("#red")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("Checkboxes only accept boolean values: #red"))
			})
		})
	})

	Describe("working with radio buttons", func() {
		It("returns the value of the group, regardless of which radio button is selected", func() {
			Ω(b.GetValue("input[name='appliances']")).Should(Equal("toaster"))
			Ω(b.GetValue("input[name='appliances'][value='stove']")).Should(Equal("toaster"))
			Ω(b.GetValue("input[name='transportation']")).Should(Equal("hovercraft"))

			Ω("input[name='appliances']").Should(b.HaveValue("toaster"))
			Ω("input[name='transportation'][value='bike']").Should(b.HaveValue("hovercraft"))
		})

		It("returns nil if no options is selected", func() {
			Ω(b.GetValue("input[name='turtle']")).Should(BeNil())
			Ω("input[name='turtle']").Should(b.HaveValue(BeNil()))
		})

		Context("when setting values directly", func() {
			It("sets the appropriate radio button in the group correctly", func() {
				b.SetValue("input[name='appliances']", "stove")
				Ω("input[name='appliances']").Should(b.HaveValue("stove"))
				Ω("input[name='appliances'][value='toaster']").Should(b.HaveProperty("checked", false))
				Ω("input[name='appliances'][value='stove']").Should(b.HaveProperty("checked", true))

				Ω("input[name='transportation']").Should(b.HaveValue("hovercraft"))
				Ω("input[name='transportation'][value='hovercraft']").Should(b.HaveProperty("checked", true))

				b.SetValue("input[name='transportation'][value='hovercraft']", "car")
				Ω("input[name='transportation']").Should(b.HaveValue("car"))
				Ω("input[name='transportation'][value='hovercraft']").Should(b.HaveProperty("checked", false))
				Ω("input[name='transportation'][value='car']").Should(b.HaveProperty("checked", true))
			})

			It("auto-fails if the element is not visible", func() {
				b.SetValue("input[name='appliances']", "microwave")
				ExpectFailures("Failed to set value:\nThe \"microwave\" option is not visible: input[name='appliances']")
				Ω("input[name='appliances']").Should(b.HaveValue("toaster"))
			})

			It("auto-fails if the element is not enabled", func() {
				b.SetValue("input[name='transportation']", "bike")
				ExpectFailures("Failed to set value:\nThe \"bike\" option is not enabled: input[name='transportation']")
				Ω("input[name='transportation']").Should(b.HaveValue("hovercraft"))
			})

			It("fails if provided an invalid value", func() {
				b.SetValue("input[name='turtle']", "splinter")
				ExpectFailures("Failed to set value:\nRadio input does not have option with value \"splinter\": input[name='turtle']")
			})

			It("fails if provided a boolean value", func() {
				b.SetValue("input[name='appliances'][value='stove']", true)
				ExpectFailures("Failed to set value:\nRadio inputs only accept string values: input[name='appliances'][value='stove']")
			})
		})

		Context("when setting values as a matcher", func() {
			It("sets the appropriate radio button in the group correctly", func() {
				Ω("input[name='appliances']").Should(b.SetValue("stove"))
				Ω("input[name='appliances']").Should(b.HaveValue("stove"))
				Ω("input[name='appliances'][value='toaster']").Should(b.HaveProperty("checked", false))
				Ω("input[name='appliances'][value='stove']").Should(b.HaveProperty("checked", true))

				Ω("input[name='transportation']").Should(b.HaveValue("hovercraft"))
				Ω("input[name='transportation'][value='hovercraft']").Should(b.HaveProperty("checked", true))

				Ω("input[name='transportation'][value='hovercraft']").Should(b.SetValue("car"))
				Ω("input[name='transportation']").Should(b.HaveValue("car"))
				Ω("input[name='transportation'][value='hovercraft']").Should(b.HaveProperty("checked", false))
				Ω("input[name='transportation'][value='car']").Should(b.HaveProperty("checked", true))
			})

			It("auto-fails if the element is not visible", func() {
				match, err := b.SetValue("microwave").Match("input[name='appliances']")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("The \"microwave\" option is not visible: input[name='appliances']"))
				Ω("input[name='appliances']").Should(b.HaveValue("toaster"))
			})

			It("auto-fails if the element is not enabled", func() {
				match, err := b.SetValue("bike").Match("input[name='transportation']")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("The \"bike\" option is not enabled: input[name='transportation']"))
				Ω("input[name='transportation']").Should(b.HaveValue("hovercraft"))
			})

			It("fails if provided an invalid value", func() {
				match, err := b.SetValue("splinter").Match("input[name='turtle']")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("Radio input does not have option with value \"splinter\": input[name='turtle']"))
			})

			It("fails if provided a boolean value", func() {
				match, err := b.SetValue(true).Match("input[name='appliances'][value='stove']")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("Radio inputs only accept string values: input[name='appliances'][value='stove']"))
			})
		})
	})

	Describe("working with multi-select inputs", func() {
		It("returns the selected options as a slice of strings", func() {
			Ω(b.GetValue("#party")).Should(ConsistOf("luke", "han", "vader"))
		})

		It("returns an empty slice if no options are selected", func() {
			Ω(b.GetValue("#empty-party")).Should(BeEmpty())
		})

		Context("when setting values directly", func() {
			It("sets the appropriate options on the group correctly", func() {
				b.SetValue("#party", []string{"obi-wan", "han", "emperor"})
				Ω(b.GetValue("#party")).Should(ConsistOf("obi-wan", "han", "emperor"))

				b.SetValue("#party", []string{})
				Ω(b.GetValue("#party")).Should(BeEmpty())
			})

			It("auto-fails if one of the options is not enabled", func() {
				b.SetValue("#party", []string{"obi-wan", "han", "leia", "tarkin"})
				ExpectFailures("Failed to set value:\nThe \"leia\" option is not enabled: #party")
			})

			It("fails if provided an invalid value", func() {
				b.SetValue("#party", []string{"obi-wan", "han", "chewie", "tarkin"})
				ExpectFailures("Failed to set value:\nThe \"chewie\" option does not exist: #party")
			})

			It("fails if provided a non-slice value", func() {
				b.SetValue("#party", "han")
				ExpectFailures("Failed to set value:\nMulti-select inputs only accept []string values: #party")
			})
		})

		Context("when setting values as a matcher", func() {
			It("sets the appropriate options on the group correctly", func() {
				Ω("#party").Should(b.SetValue([]string{"obi-wan", "han", "emperor"}))
				Ω(b.GetValue("#party")).Should(ConsistOf("obi-wan", "han", "emperor"))

				Ω("#party").Should(b.SetValue([]string{}))
				Ω(b.GetValue("#party")).Should(BeEmpty())
			})

			It("auto-fails if one of the options is not enabled", func() {
				match, err := b.SetValue([]string{"obi-wan", "han", "leia", "tarkin"}).Match("#party")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("The \"leia\" option is not enabled: #party"))
			})

			It("fails if provided an invalid value", func() {
				match, err := b.SetValue([]string{"obi-wan", "han", "chewie", "tarkin"}).Match("#party")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("The \"chewie\" option does not exist: #party"))
			})

			It("fails if provided a non-slice value", func() {
				match, err := b.SetValue("han").Match("#party")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("Multi-select inputs only accept []string values: #party"))
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
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
		})
	})

	Describe("GetProperty", func() {
		It("returns properties defined on the element", func() {
			Ω(b.GetProperty(".notice", "count")).Should(Equal(3.0))
			Ω(b.GetProperty(".notice", "tagName")).Should(Equal("DIV"))
			Ω(b.GetProperty(".notice", "flavor")).Should(Equal("strawberry"))
			Ω(b.GetProperty(".notice", "offsetWidth")).Should(Equal(200.0))
			Ω(b.GetProperty(".notice", "innerText")).Should(Equal("Some Text"))
			Ω(b.GetProperty(".notice", "innerText")).Should(Equal("Some Text"))
			Ω(b.GetProperty(".notice", "hidden")).Should(Equal(false))
			Ω(b.GetProperty(".notice", "dataset.name")).Should(Equal("henry"))
			Ω(b.GetProperty("#hidden-text-input", "value")).Should(Equal("my-hidden-value"))
			Ω(b.GetProperty(".notice", "floop")).Should(BeNil())
		})

		It("converts iterables into arrays", func() {
			Ω(b.GetProperty(".notice", "classList")).Should(ConsistOf("notice"))
		})

		It("converts DOMStringMaps into objects", func() {
			Ω(b.GetProperty(".notice", "dataset")).Should(HaveKeyWithValue("name", "henry"))
		})

		It("returns an error when the element does not exist", func() {
			b.GetProperty("#non-existing", "tagName")
			ExpectFailures("Failed to get property \"tagName\":\ncould not find DOM element matching selector: #non-existing")
		})
	})

	Describe("HaveProperty", func() {
		It("checks property existence when not passed a second argument", func() {
			Ω(".notice").Should(b.HaveProperty("count"))
			Ω(".notice").Should(b.HaveProperty("classList"))
			Ω(".notice").ShouldNot(b.HaveProperty("non-existing"))
		})

		It("returns properties defined on the element", func() {
			Ω(".notice").Should(b.HaveProperty("count", 3.0))
			Ω(".notice").Should(b.HaveProperty("tagName", "DIV"))
			Ω(".notice").Should(b.HaveProperty("flavor", "strawberry"))
			Ω(".notice").Should(b.HaveProperty("offsetWidth", 200.0))
			Ω(".notice").Should(b.HaveProperty("innerText", "Some Text"))
			Ω(".notice").Should(b.HaveProperty("innerText", "Some Text"))
			Ω(".notice").Should(b.HaveProperty("hidden", false))
			Ω(".notice").ShouldNot(b.HaveProperty("floop", "any"))
			Ω(".notice").Should(b.HaveProperty("classList", ConsistOf("notice")))
			Ω(".notice").Should(b.HaveProperty("dataset", HaveKeyWithValue("name", "henry")))
			Ω(".notice").Should(b.HaveProperty("dataset.name", "henry"))
			Ω("#hidden-text-input").Should(b.HaveProperty("value", "my-hidden-value"))
		})

		It("returns an error when the element does not exist", func() {
			match, err := b.HaveProperty("tagName", "any").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
		})

	})

	Describe("SetProperty", func() {
		It("modifies properties set on dom elements", func() {
			Ω(".notice").Should(b.HaveProperty("count", 3.0))
			b.SetProperty(".notice", "count", 7.0)
			Ω(".notice").Should(b.HaveProperty("count", 7.0))

			Ω(".notice").Should(b.BeVisible())
			Ω(".notice").Should(b.SetProperty("hidden", true))
			Eventually(".notice").ShouldNot(b.BeVisible())

			Ω(".notice").Should(b.HaveProperty("dataset.name", "henry"))
			b.SetProperty(".notice", "dataset.name", "bob")
			Ω(".notice").Should(b.HaveProperty("dataset.name", "bob"))

			Ω(".notice").ShouldNot(b.HaveProperty("dataset.age"))
			b.SetProperty(".notice", "dataset.age", 17.0)
			Ω(".notice").Should(b.HaveProperty("dataset.age", "17"))

			Ω("#hidden-text-input").Should(b.HaveProperty("value", "my-hidden-value"))
			b.SetProperty("#hidden-text-input", "value", "new-hidden-value")
			Ω("#hidden-text-input").Should(b.HaveProperty("value", "new-hidden-value"))
		})

		It("returns an error when the property chain can't be traversed", func() {
			b.SetProperty(".notice", "foo.bar", "baz")
			ExpectFailures("Failed to set property \"foo.bar\":\ncould not resolve property component \".foo\": .notice")
		})

		It("returns an error when the element does not exist", func() {
			match, err := b.SetProperty("tagName", "any").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))

			b.SetProperty("#non-existing", "foo", "bar")
			ExpectFailures("Failed to set property \"foo\":\ncould not find DOM element matching selector: #non-existing")
		})
	})

	Describe("GetPropertyForEach", func() {
		It("fetches the requested property from all elements matching the selector", func() {
			values := b.GetPropertyForEach("input[type='radio'][name='appliances']", "value")
			Expect(values).To(HaveExactElements("toaster", "stove", "microwave"))

			values = b.GetPropertyForEach(b.XPath("div").WithID("check-boxes").Descendant("input").WithAttr("type", "checkbox"), "id")
			Expect(values).To(HaveExactElements("red", "blue", "yellow", "green"))

			values = b.GetPropertyForEach(".notice", "dataset.name")
			Expect(values).To(HaveExactElements("henry", "bob", BeNil()))
		})

		It("returns an empty array when no elements are found", func() {
			values := b.GetPropertyForEach("#non-existing", "href")
			Expect(values).To(BeEmpty())
		})

		It("returns nil values for elements that are found but don't have the property", func() {
			values := b.GetPropertyForEach("input[type='radio'][name='appliances']", "href")
			Expect(values).To(HaveExactElements(BeNil(), BeNil(), BeNil()))
		})
	})

	Describe("SetPropertyForEach", func() {
		It("sets the specified property to the same value on any matching elements", func() {
			Expect("#check-boxes input[type='checkbox']").To(b.EachHaveProperty("checked", true, false, false, false))
			b.SetPropertyForEach("#check-boxes input[type='checkbox']", "checked", true)
			Expect("#check-boxes input[type='checkbox']").To(b.EachHaveProperty("checked", true, true, true, true))

			Expect(".notice").To(b.EachHaveProperty("dataset.name", "henry", "bob", nil))
			b.SetPropertyForEach(".notice", "dataset.name", "John")
			Expect(".notice").To(b.EachHaveProperty("dataset.name", HaveEach("John")))
		})

		It("does nothing if no elements match", func() {
			b.SetPropertyForEach(".non-existing", "href", "http://example.com")
		})

		It("fails if a property can't be set because of delimiter issues", func() {
			b.SetPropertyForEach("li", "foo.bar", 3)
			ExpectFailures("Failed to set property \"foo.bar\" for each:\ncould not resolve property component \".foo\": li")
		})
	})

	Describe("EachHaveProperty", func() {
		It("simply asserts that the property is defined if it is only passed in a property", func() {
			Expect("#party optgroup[label='Heros'] option").To(b.EachHaveProperty("value"))
			Expect(".notice").NotTo(b.EachHaveProperty("data-name"))

			Expect(".non-existing").NotTo(b.EachHaveProperty("href"))
		})

		It("verifies that the returned values all match the expected properties if provided", func() {
			Expect(".notice").To(b.EachHaveProperty("dataset.name", "henry", "bob", nil))
		})

		It("uses the passed-in matcher if there is only one argument", func() {
			Expect(".notice").To(b.EachHaveProperty("dataset.name", ContainElement("bob")))
			Expect(".notice").NotTo(b.EachHaveProperty("dataset.name", ContainElement("john")))
			Expect("input").To(b.EachHaveProperty("tagName", HaveEach("INPUT")))
		})

		It("returns empty if no elements are found", func() {
			Expect(".non-existing").NotTo(b.EachHaveProperty("href"))
			Expect(".non-existing").To(b.EachHaveProperty("href", BeEmpty()))
		})
	})

	Describe("GetProperties", func() {
		It("returns the requested properties defined on the element", func() {
			p := b.GetProperties(".notice", "count", "disabled", "tagName", "flavor", "dataset.name", "classList", "innerText", "dataset", "nonExisting", "foo.bar.baz")
			Expect(p["count"]).To(Equal(3.0))
			Expect(p.GetInt("count")).To(Equal(3))
			Expect(p.GetBool("disabled")).To(Equal(false))
			Expect(p["tagName"]).To(Equal("DIV"))
			Expect(p["flavor"]).To(Equal("strawberry"))
			Expect(p["dataset.name"]).To(Equal("henry"))
			Expect(p["dataset"]).To(HaveKeyWithValue("name", "henry"))
			Expect(p.GetStringSlice("classList")).To(Equal([]string{"notice"}))
			Expect(p["innerText"]).To(Equal("Some Text"))
			Expect(p["nonExisting"]).To(BeNil())
			Expect(p["foo.bar.baz"]).To(BeNil())
			Expect(p.Get("blah")).To(BeNil())
		})

		It("fails if no properties are requested when the element does not exist", func() {
			b.GetProperties(".notice")
			ExpectFailures("GetProperties requires at least one property to fetch")
		})

		It("fails when the element does not exist", func() {
			b.GetProperties("#non-existing", "tagName", "classList")
			ExpectFailures("Failed to get properties tagName, classList:\ncould not find DOM element matching selector: #non-existing")
		})
	})

	Describe("GetPropertiesForEach", func() {
		It("returns all the requested properties defined on all matched elements", func() {
			p := b.GetPropertiesForEach(".notice", "count", "disabled", "tagName", "flavor", "dataset.name", "classList", "innerText", "dataset", "nonExisting", "foo.bar.baz")
			Expect(p).To(HaveLen(3))
			Expect(p[0]["count"]).To(Equal(3.0))
			Expect(p[0].GetInt("count")).To(Equal(3))
			Expect(p[0].GetBool("disabled")).To(Equal(false))
			Ω(p.GetInt("count")).Should(Equal([]int{3, 0, 0}))
			Ω(p.GetBool("disabled")).Should(Equal([]bool{false, false, false}))
			Ω(p.GetString("tagName")).Should(Equal([]string{"DIV", "DIV", "DIV"}))
			Ω(p.GetString("flavor")).Should(Equal([]string{"strawberry", "", ""}))
			Ω(p.GetString("dataset.name")).Should(Equal([]string{"henry", "bob", ""}))
			Ω(p.GetStringSlice("classList")).Should(Equal([][]string{{"notice"}, {"notice"}, {"notice", "anon"}}))
			Ω(p.GetString("innerText")).Should(Equal([]string{"Some Text", "Some Other Text", "Nameless"}))
			Ω(p.Get("dataset")).Should(HaveExactElements(
				HaveKeyWithValue("name", "henry"),
				HaveKeyWithValue("name", "bob"),
				BeEmpty(),
			))
			Ω(p.GetString("nonExisting")).Should(HaveEach(""))
			Ω(p.GetString("foo.bar.baz")).Should(HaveEach(""))
			Ω(p.GetInt("floop")).Should(HaveEach(0))
			Ω(p.GetString("floop")).Should(HaveEach(""))
			Ω(p.GetStringSlice("floop")).Should(HaveEach([]string{}))
			Ω(p.Get("floop")).Should(HaveLen(3))
		})

		It("fails if no properties are requested when the element does not exist", func() {
			b.GetPropertiesForEach(".notice")
			ExpectFailures("GetPropertiesForEach requires at least one property to fetch")
		})

		It("returns an empty slice if no element is found", func() {
			Ω(b.GetPropertiesForEach("#non-existing", "tagName", "classList")).Should(HaveLen(0))
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
				ExpectFailures("Failed to click:\ncould not find DOM element matching selector: #non-existing")
			})

			It("auto-fails if the element is not visible", func() {
				b.Click("#hidden-button")
				ExpectFailures("Failed to click:\nDOM element is not visible: #hidden-button")
			})

			It("auto-fails if the element is not enabled", func() {
				b.Click("#decrement")
				ExpectFailures("Failed to click:\nDOM element is not enabled: #decrement")
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
				Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
			})

			It("returns an error when the element is not visible", func() {
				match, err := b.Click().Match("#hidden-button")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("DOM element is not visible: #hidden-button"))
			})

			It("returns an error when the element is not enabled", func() {
				match, err := b.Click().Match("#decrement")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("DOM element is not enabled: #decrement"))
			})
		})
	})

	Describe("using xpath selectors", func() {
		It("uses the first element returned by the xpath selector", func() {
			b.Click(b.XPath("button").WithText("Increment"))
			Ω("#counter-input").Should(b.HaveValue("1"))
			Ω(b.XPath("button").WithID("decrement")).Should(b.Click())
			Ω(b.XPath().WithID("counter").FollowingSibling("input")).Should(b.HaveValue("0"))
			b.Click(b.XPath("button").WithText("Increment"))
			Ω(b.XPath().WithID("counter-input"))
		})

		It("errors when the element does not exist", func() {
			b.Click(b.XPath("button").WithText("nope"))
			ExpectFailures("Failed to click:\ncould not find DOM element matching selector: //button[text()='nope']")
		})
	})
})

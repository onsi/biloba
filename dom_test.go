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

	Describe("EachBeVisible", func() {
		It("matches when every element is visible", func() {
			Ω(".each-vis:not(.hidden)").Should(b.EachBeVisible())
		})

		It("does not match when any element is hidden", func() {
			Ω(".each-vis").ShouldNot(b.EachBeVisible())
		})

		It("fails (does not pass vacuously) when no elements match the selector", func() {
			m := b.EachBeVisible()
			Ω(".non-existing").ShouldNot(m)
			Ω(m.FailureMessage(".non-existing")).Should(ContainSubstring("Expected at least one element to match .non-existing, but none did"))
		})

		It("fails the spec via the immediate-failure path of a captured failure", func() {
			match, err := b.EachBeVisible().Match(".each-vis")
			Ω(match).Should(BeFalse())
			Ω(err).Should(BeNil())
			Ω(b.EachBeVisible().FailureMessage(".each-vis")).Should(ContainSubstring("each be visible"))
		})
	})

	Describe("EachBeEnabled", func() {
		It("matches when every element is enabled", func() {
			Ω(".each-en:not([disabled])").Should(b.EachBeEnabled())
		})

		It("does not match when any element is disabled", func() {
			Ω(".each-en").ShouldNot(b.EachBeEnabled())
		})

		It("matches enabled elements even when some are hidden", func() {
			Ω(".each-vis").Should(b.EachBeEnabled())
		})

		It("fails (does not pass vacuously) when no elements match the selector", func() {
			m := b.EachBeEnabled()
			Ω(".non-existing").ShouldNot(m)
			Ω(m.FailureMessage(".non-existing")).Should(ContainSubstring("Expected at least one element to match .non-existing, but none did"))
		})
	})

	Describe("EachHaveClass", func() {
		It("matches when every element has the class", func() {
			Ω(".each-vis").Should(b.EachHaveClass("tagged"))
			Ω(".each-vis").Should(b.EachHaveClass("each-vis"))
		})

		It("does not match when any element lacks the class", func() {
			Ω("#each-group > *").ShouldNot(b.EachHaveClass("tagged"))
		})

		It("fails (does not pass vacuously) when no elements match the selector", func() {
			m := b.EachHaveClass("tagged")
			Ω(".non-existing").ShouldNot(m)
			Ω(m.FailureMessage(".non-existing")).Should(ContainSubstring("Expected at least one element to match .non-existing, but none did"))
		})
	})

	Describe("GetInnerText", func() {
		It("returns the InnerText of the element", func() {
			Ω(b.GetInnerText("#hello")).Should(Equal("Hello Biloba!"))
			Ω(b.GetInnerText("#hidden-child")).Should(Equal("Can't see me!"))
			Ω(b.GetInnerText("#list")).Should(Equal("First Things\nSecond Things\nThird Things"))
		})

		It("times out (poll-by-default) if the element never exists", func() {
			b.WithTimeout(time.Millisecond * 60).GetInnerText("#non-existing")
			ExpectFailures(ContainSubstring("Timed out after"))
		})

		It("fails fast under Immediate() if the element does not exist", func() {
			Ω(b.Immediate().GetInnerText("#non-existing")).Should(Equal(""))
			ExpectFailures(ContainSubstring("have property \"innerText\""))
		})
	})

	Describe("CurrentInnerTextForEach", func() {
		It("returns the InnerText of the element", func() {
			Ω(b.CurrentInnerTextForEach(b.XPath().WithID("party").Descendant("optgroup").WithAttr("label", "Heros").Descendant("option"))).Should(HaveExactElements("Luke", "Leia", "Han", "Obi-Wan"))

			Ω(b.CurrentInnerTextForEach("#list li")).Should(HaveExactElements("First Things", "Second Things", "Third Things"))
		})

		It("returns an empty slice if no elements exist", func() {
			Ω(b.CurrentInnerTextForEach(".non-existing")).Should(BeEmpty())
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
		})

		It("fails (does not pass vacuously) when no elements match the selector", func() {
			m := b.EachHaveInnerText("anything")
			Ω("#non-existing").ShouldNot(m)
			Ω(m.FailureMessage("#non-existing")).Should(ContainSubstring("Expected at least one element to match #non-existing, but none did"))
			// to assert that nothing matches, use HaveCount(0) instead
			Ω("#non-existing").Should(b.HaveCount(0))
		})
	})

	Describe("GetTextContent", func() {
		It("returns the textContent of the element - including hidden content", func() {
			Ω(b.GetTextContent("#hello")).Should(Equal("Hello Biloba!"))
			// textContent reads straight from the DOM tree, so it sees hidden elements just the same
			Ω(b.GetTextContent("#hidden-child")).Should(Equal("Can't see me!"))
			// unlike innerText, textContent does not collapse block layout into newlines - it returns
			// the raw template whitespace, so #list comes back with each <li> on its own indented line
			Ω(b.GetTextContent("#list")).Should(ContainSubstring("First Things"))
			Ω(b.GetTextContent("#list")).Should(ContainSubstring("Second Things"))
			Ω(b.GetTextContent("#list")).Should(ContainSubstring("Third Things"))
		})

		It("times out (poll-by-default) if the element never exists", func() {
			b.WithTimeout(time.Millisecond * 60).GetTextContent("#non-existing")
			ExpectFailures(ContainSubstring("Timed out after"))
		})

		It("fails fast under Immediate() if the element does not exist", func() {
			Ω(b.Immediate().GetTextContent("#non-existing")).Should(Equal(""))
			ExpectFailures(ContainSubstring("have property \"textContent\""))
		})
	})

	Describe("CurrentTextContentForEach", func() {
		It("returns the textContent of each element", func() {
			Ω(b.CurrentTextContentForEach(b.XPath().WithID("party").Descendant("optgroup").WithAttr("label", "Heros").Descendant("option"))).Should(HaveExactElements("Luke", "Leia", "Han", "Obi-Wan"))

			Ω(b.CurrentTextContentForEach("#list li")).Should(HaveExactElements("First Things", "Second Things", "Third Things"))
		})

		It("returns an empty slice if no elements exist", func() {
			Ω(b.CurrentTextContentForEach(".non-existing")).Should(BeEmpty())
		})
	})

	Describe("HaveTextContent", func() {
		It("matches if the element in question has the specified text content", func() {
			Ω("#hello").Should(b.HaveTextContent("Hello Biloba!"))
			Ω("#hidden-child").Should(b.HaveTextContent("Can't see me!"))
			Ω("#hello").ShouldNot(b.HaveTextContent("nope"))
		})

		It("works with matchers", func() {
			Ω("#list").Should(b.HaveTextContent(ContainSubstring("Second Things")))
		})

		It("errors if the element does not exist", func() {
			match, err := b.HaveTextContent("").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
		})
	})

	Describe("EachHaveTextContent", func() {
		It("matches if the elements in question have the specified text content", func() {
			Ω(b.XPath().WithID("party").Descendant("optgroup").WithAttr("label", "Heros").Descendant("option")).Should(b.EachHaveTextContent("Luke", "Leia", "Han", "Obi-Wan"))

			Ω("#list li").Should(b.EachHaveTextContent(ConsistOf("Second Things", "First Things", "Third Things")))
			Ω("#list li").Should(b.EachHaveTextContent(ContainElement("Second Things")))
		})

		It("fails (does not pass vacuously) when no elements match the selector", func() {
			m := b.EachHaveTextContent("anything")
			Ω("#non-existing").ShouldNot(m)
			Ω(m.FailureMessage("#non-existing")).Should(ContainSubstring("Expected at least one element to match #non-existing, but none did"))
			// to assert that nothing matches, use HaveCount(0) instead
			Ω("#non-existing").Should(b.HaveCount(0))
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

			It("polls until the element appears, then returns its value", func() {
				go func() {
					defer GinkgoRecover()
					<-time.After(time.Millisecond * 100)
					b.Run(`document.querySelector("#text-area").id = "delayed-text-area"`)
				}()
				Ω(b.GetValue("#delayed-text-area")).Should(Equal("Something long"))
			})

			It("treats an empty value as valid (does not wait for it to be non-empty)", func() {
				b.SetValue("#text-input", "")
				Ω(b.GetValue("#text-input")).Should(Equal(""))
			})

			It("times out (poll-by-default) if the element never exists", func() {
				b.WithTimeout(time.Millisecond * 60).GetValue("#non-existing")
				ExpectFailures(SatisfyAll(
					ContainSubstring("Timed out after"),
					ContainSubstring("have a value"),
				))
			})

			It("fails fast under Immediate() if the element does not exist", func() {
				Ω(b.Immediate().GetValue("#non-existing")).Should(BeNil())
				ExpectFailures(ContainSubstring("have a value"))
			})
		})

		Describe("CurrentValueForEach", func() {
			It("returns a snapshot of the rationalized value for all matching elements", func() {
				Ω(b.CurrentValueForEach("#check-boxes input[type='checkbox']")).Should(HaveExactElements(true, false, false, false))

				b.SetPropertyForEachImmediately("#check-boxes input[type='checkbox']", "checked", true)
				Ω(b.CurrentValueForEach("#check-boxes input[type='checkbox']")).Should(HaveExactElements(true, true, true, true))
			})

			It("returns an empty slice when no elements are found (it does not poll)", func() {
				Ω(b.CurrentValueForEach("#non-existing")).Should(BeEmpty())
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

				It("times out (poll-by-default) if the element never exists", func() {
					b.WithTimeout(time.Millisecond*60).SetValue("#non-existing", "foo")
					ExpectFailures(SatisfyAll(
						ContainSubstring("Timed out after"),
						ContainSubstring("could not find DOM element matching selector: #non-existing"),
					))
				})

				It("fails fast under Immediate() if the element does not exist", func() {
					b.Immediate().SetValue("#non-existing", "foo")
					ExpectFailures(ContainSubstring("could not find DOM element matching selector: #non-existing"))
				})

				It("fails fast under Immediate() if the element is not visible", func() {
					b.Immediate().SetValue("#hidden-text-input", "foo")
					ExpectFailures(ContainSubstring("DOM element is not visible: #hidden-text-input"))
					Ω("#hidden-text-input").Should(b.HaveValue("my-hidden-value"))
				})

				It("fails fast under Immediate() if the element is not enabled", func() {
					b.Immediate().SetValue("#disabled-text-input", "foo")
					ExpectFailures(ContainSubstring("DOM element is not enabled: #disabled-text-input"))
					Ω("#disabled-text-input").Should(b.HaveValue("i'm off"))
				})

				It("fails if attempting to set the value of a select input to an option that does not exist", func() {
					b.Immediate().SetValue("#droid", "grogu")
					ExpectFailures(ContainSubstring("Select input does not have option with value \"grogu\": #droid"))
				})

				It("is a hard error to configure the bare-matcher form", func() {
					b.WithTimeout(time.Second).SetValue("foo")
					ExpectFailures(ContainSubstring("SetValue(...) returns a matcher"))
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

			Context("when passed a ValueLabel", func() {
				It("selects a single-select option by its visible label", func() {
					b.SetValue("#droid", b.ValueLabel("BB-8"))
					Ω("#droid").Should(b.HaveValue("bb8"))

					Ω("#droid").Should(b.SetValue(b.ValueLabel("C-3PO")))
					Ω("#droid").Should(b.HaveValue("c3po"))
				})

				It("selects multi-select options by label, mixing labels and raw values", func() {
					b.SetValue("#party", []any{b.ValueLabel("Obi-Wan"), "han", b.ValueLabel("The Emperor")})
					Ω(b.GetValue("#party")).Should(ConsistOf("obi-wan", "han", "emperor"))
				})

				It("fails when no option has the given label", func() {
					b.Immediate().SetValue("#droid", b.ValueLabel("Grogu"))
					ExpectFailures(ContainSubstring("Select input does not have option with label \"Grogu\": #droid"))
				})

				It("fails when used on a non-select element", func() {
					b.Immediate().SetValue("#text-input", b.ValueLabel("nope"))
					ExpectFailures(ContainSubstring("ValueLabel is only supported for <select> elements: #text-input"))
				})
			})

			Context("when the input is a controlled (React/Vue-style) input", func() {
				It("drives the value past the framework's value tracker", func() {
					// #react-controlled installs a value tracker that shadows the element's own value setter
					// and only commits when the change is seen via the native prototype setter. A raw
					// `n.value = v` would leave window._reactCommitted at "".
					Ω(b.Run("window._reactCommitted")).Should(Equal(""))
					b.SetValue("#react-controlled", "chloroplast")
					Ω("#react-controlled").Should(b.HaveValue("chloroplast"))
					Eventually("window._reactCommitted").Should(b.EvaluateTo("chloroplast"))
				})
			})

			Context("when setting a text input", func() {
				It("does not blur the input (so onBlur handlers do not fire)", func() {
					Ω(b.Run("window._blurFired")).Should(BeFalse())
					b.SetValue("#blur-tracker", "hi")
					Ω("#blur-tracker").Should(b.HaveValue("hi"))
					Consistently("window._blurFired").Should(b.EvaluateTo(false))
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

			It("fails fast under Immediate() if the element is not visible", func() {
				b.Immediate().SetValue("#green", true)
				ExpectFailures(ContainSubstring("DOM element is not visible: #green"))
				Ω("#checked-color").Should(b.HaveInnerText("red"))
			})

			It("fails fast under Immediate() if the element is not enabled", func() {
				b.Immediate().SetValue("#yellow", true)
				ExpectFailures(ContainSubstring("DOM element is not enabled: #yellow"))
				Ω("#checked-color").Should(b.HaveInnerText("red"))
			})

			It("fails if not provided a boolean value", func() {
				b.Immediate().SetValue("#red", "true")
				ExpectFailures(ContainSubstring("Checkboxes only accept boolean values: #red"))
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

			It("fails fast under Immediate() if the element is not visible", func() {
				b.Immediate().SetValue("input[name='appliances']", "microwave")
				ExpectFailures(ContainSubstring("The \"microwave\" option is not visible: input[name='appliances']"))
				Ω("input[name='appliances']").Should(b.HaveValue("toaster"))
			})

			It("fails fast under Immediate() if the element is not enabled", func() {
				b.Immediate().SetValue("input[name='transportation']", "bike")
				ExpectFailures(ContainSubstring("The \"bike\" option is not enabled: input[name='transportation']"))
				Ω("input[name='transportation']").Should(b.HaveValue("hovercraft"))
			})

			It("fails if provided an invalid value", func() {
				b.Immediate().SetValue("input[name='turtle']", "splinter")
				ExpectFailures(ContainSubstring("Radio input does not have option with value \"splinter\": input[name='turtle']"))
			})

			It("fails if provided a boolean value", func() {
				b.Immediate().SetValue("input[name='appliances'][value='stove']", true)
				ExpectFailures(ContainSubstring("Radio inputs only accept string values: input[name='appliances'][value='stove']"))
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

			It("fails fast under Immediate() if one of the options is not enabled", func() {
				b.Immediate().SetValue("#party", []string{"obi-wan", "han", "leia", "tarkin"})
				ExpectFailures(ContainSubstring("The \"leia\" option is not enabled: #party"))
			})

			It("fails if provided an invalid value", func() {
				b.Immediate().SetValue("#party", []string{"obi-wan", "han", "chewie", "tarkin"})
				ExpectFailures(ContainSubstring("The \"chewie\" option does not exist: #party"))
			})

			It("fails if provided a non-slice value", func() {
				b.Immediate().SetValue("#party", "han")
				ExpectFailures(ContainSubstring("Multi-select inputs only accept []string values: #party"))
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

	Describe("HaveText", func() {
		It("matches against the whitespace-normalized innerText", func() {
			Ω("#hello").Should(b.HaveText("Hello Biloba!"))
			Ω("#spacey-text").Should(b.HaveText("Hello there Biloba!"))
			Ω("#spacey-text").ShouldNot(b.HaveText("Hello   there\n\n        Biloba!"))
		})

		It("supports matchers", func() {
			Ω("#spacey-text").Should(b.HaveText(ContainSubstring("there Biloba")))
			Eventually("#spacey-text").Should(b.HaveText(HavePrefix("Hello there")))
		})

		It("returns an error when the element does not exist", func() {
			match, err := b.HaveText("foo").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
		})
	})

	Describe("HaveAttribute", func() {
		It("checks attribute existence when given only a name", func() {
			Ω("#link").Should(b.HaveAttribute("href"))
			Ω("#link").Should(b.HaveAttribute("data-role"))
			Ω("#link").ShouldNot(b.HaveAttribute("data-missing"))
		})

		It("checks the attribute value when given a second argument", func() {
			Ω("#link").Should(b.HaveAttribute("href", "/about"))
			Ω("#link").Should(b.HaveAttribute("data-role", "nav"))
			Ω("#link").Should(b.HaveAttribute("href", HaveSuffix("about")))
			Ω("#link").ShouldNot(b.HaveAttribute("href", "/contact"))
		})

		It("is distinct from HaveProperty (attribute vs property)", func() {
			//the href property is resolved to an absolute URL; the attribute is the raw value
			Ω("#link").Should(b.HaveAttribute("href", "/about"))
			Ω("#link").Should(b.HaveProperty("href", HaveSuffix("/about")))
			Ω("#link").ShouldNot(b.HaveProperty("href", "/about"))
		})

		It("returns an error when the element does not exist", func() {
			match, err := b.HaveAttribute("href").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))

			match, err = b.HaveAttribute("href", "/about").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
		})
	})

	Describe("BeChecked", func() {
		It("matches checked checkboxes and radio buttons", func() {
			Ω("#red").Should(b.BeChecked())
			Ω("#blue").ShouldNot(b.BeChecked())
			Ω("input[name='appliances'][value='toaster']").Should(b.BeChecked())
			Ω("input[name='appliances'][value='stove']").ShouldNot(b.BeChecked())
		})

		It("updates as the checkbox state changes", func() {
			b.SetValue("#blue", true)
			Eventually("#blue").Should(b.BeChecked())
		})

		It("returns an error when the element does not exist", func() {
			match, err := b.BeChecked().Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
		})
	})

	Describe("BeFocused", func() {
		It("matches the document's activeElement", func() {
			Ω("#focus-input").ShouldNot(b.BeFocused())
			b.InvokeOn("#focus-input", "focus")
			Eventually("#focus-input").Should(b.BeFocused())
			Ω("#hello").ShouldNot(b.BeFocused())
		})

		It("returns an error when the element does not exist", func() {
			match, err := b.BeFocused().Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
		})
	})

	Describe("Blur", func() {
		It("fires the element's onBlur handler when called directly", func() {
			// blur only emits a blur event if the element is actually focused, so focus it first
			b.Focus("#blur-tracker")
			Eventually("#blur-tracker").Should(b.BeFocused())
			Ω(b.Run("window._blurFired")).Should(BeFalse())
			b.Blur("#blur-tracker")
			Eventually("window._blurFired").Should(b.EvaluateTo(true))
			Ω("#blur-tracker").ShouldNot(b.BeFocused())
		})

		It("works in the matcher form", func() {
			b.Run("window._blurFired = false")
			b.Focus("#blur-tracker")
			Eventually("#blur-tracker").Should(b.BeFocused())
			Ω(b.Run("window._blurFired")).Should(BeFalse())
			Eventually("#blur-tracker").Should(b.Blur())
			Eventually("window._blurFired").Should(b.EvaluateTo(true))
		})

		It("times out (poll-by-default) if the element never exists", func() {
			b.WithTimeout(time.Millisecond * 60).Blur("#non-existing")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("could not find DOM element matching selector: #non-existing"),
			))
		})

		It("fails fast under Immediate() if the element does not exist", func() {
			b.Immediate().Blur("#non-existing")
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #non-existing"))
		})

		It("is a hard error to configure the bare-matcher form", func() {
			b.WithTimeout(time.Second).Blur()
			ExpectFailures(ContainSubstring("Blur(...) returns a matcher"))
		})

		It("returns an error when the element does not exist in the matcher form", func() {
			match, err := b.Blur().Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
		})
	})

	Describe("HaveComputedStyle", func() {
		It("matches the computed style of the element", func() {
			Ω("#styled").Should(b.HaveComputedStyle("display", "none"))
			Ω("#styled").Should(b.HaveComputedStyle("color", "rgb(255, 0, 0)"))
			Ω("#styled").Should(b.HaveComputedStyle("color", ContainSubstring("255")))
			Ω("#hello").Should(b.HaveComputedStyle("display", "block"))
			Ω("#hello").ShouldNot(b.HaveComputedStyle("display", "none"))
		})

		It("returns an error when the element does not exist", func() {
			match, err := b.HaveComputedStyle("display", "none").Match("#non-existing")
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
		})

		It("converts iterables into arrays", func() {
			Ω(b.GetProperty(".notice", "classList")).Should(ConsistOf("notice"))
		})

		It("converts DOMStringMaps into objects", func() {
			Ω(b.GetProperty(".notice", "dataset")).Should(HaveKeyWithValue("name", "henry"))
		})

		It("polls until the property is defined, then returns it", func() {
			go func() {
				defer GinkgoRecover()
				<-time.After(time.Millisecond * 100)
				b.Run(`document.querySelector(".notice").late = "here"`)
			}()
			Ω(b.GetProperty(".notice", "late")).Should(Equal("here"))
		})

		It("returns nil for an undefined property wrapped in AllowMissing (no waiting)", func() {
			Ω(b.GetProperty(".notice", b.AllowMissing("floop"))).Should(BeNil())
			Ω(b.GetProperty(".notice", b.AllowMissing("dataset.name"))).Should(Equal("henry"))
		})

		It("times out for a required-but-undefined property", func() {
			b.WithTimeout(time.Millisecond*60).GetProperty(".notice", "floop")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("have property \"floop\""),
			))
		})

		It("times out (poll-by-default) when the element never exists", func() {
			b.WithTimeout(time.Millisecond*60).GetProperty("#non-existing", "tagName")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("have property \"tagName\""),
			))
		})

		It("fails fast under Immediate() when the element does not exist", func() {
			Ω(b.Immediate().GetProperty("#non-existing", "tagName")).Should(BeNil())
			ExpectFailures(ContainSubstring("have property \"tagName\""))
		})
	})

	Describe("GetAttribute", func() {
		It("returns the raw attribute defined on the element", func() {
			Ω(b.GetAttribute("#link", "href")).Should(Equal("/about"))
			Ω(b.GetAttribute("#link", "data-role")).Should(Equal("nav"))
		})

		It("returns nil for an absent attribute wrapped in AllowMissing (no waiting)", func() {
			Ω(b.GetAttribute("#link", b.AllowMissing("data-missing"))).Should(BeNil())
			Ω(b.GetAttribute("#link", b.AllowMissing("href"))).Should(Equal("/about"))
		})

		It("reads the raw attribute, not the resolved property", func() {
			//the href property is resolved to an absolute URL; the attribute is the raw value
			Ω(b.GetAttribute("#link", "href")).Should(Equal("/about"))
			Ω(b.GetProperty("#link", "href")).Should(HaveSuffix("/about"))
		})

		It("times out for a required-but-absent attribute", func() {
			b.WithTimeout(time.Millisecond*60).GetAttribute("#link", "data-missing")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("have attribute \"data-missing\""),
			))
		})

		It("times out (poll-by-default) when the element never exists", func() {
			b.WithTimeout(time.Millisecond*60).GetAttribute("#non-existing", "href")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("have attribute \"href\""),
			))
		})

		It("fails fast under Immediate() when the element does not exist", func() {
			Ω(b.Immediate().GetAttribute("#non-existing", "href")).Should(BeNil())
			ExpectFailures(ContainSubstring("have attribute \"href\""))
		})
	})

	Describe("GetJSONAttribute and HaveJSONAttribute", func() {
		It("decodes a JSON attribute into a struct", func() {
			var state struct {
				Open   bool     `json:"open"`
				Count  int      `json:"count"`
				Labels []string `json:"labels"`
			}
			b.GetJSONAttribute("#widget", "data-widget-state", &state)
			Ω(state.Open).Should(BeTrue())
			Ω(state.Count).Should(Equal(3))
			Ω(state.Labels).Should(ConsistOf("a", "b"))
		})

		It("decodes into a map too", func() {
			var m map[string]any
			b.GetJSONAttribute("#widget", "data-widget-state", &m)
			Ω(m).Should(HaveKeyWithValue("count", 3.0))
		})

		It("polls until the attribute is present and valid, tolerating a re-render", func() {
			b.Run("setTimeout(() => document.getElementById('widget-mutate').click(), 40)")
			var m map[string]any
			// poll until the mutation has landed (count flips 3 -> 7)
			Eventually("#widget").Should(b.HaveJSONAttribute("data-widget-state", HaveKeyWithValue("count", 7.0)))
			b.GetJSONAttribute("#widget", "data-widget-state", &m)
			Ω(m).Should(HaveKeyWithValue("open", false))
		})

		It("matches with a composed matcher", func() {
			Eventually("#widget").Should(b.HaveJSONAttribute("data-widget-state", HaveKeyWithValue("open", true)))
			Expect("#widget").NotTo(b.HaveJSONAttribute("data-widget-state", HaveKeyWithValue("open", false)))
		})

		It("times out (poll-by-default) when the element never exists", func() {
			var m map[string]any
			b.WithTimeout(time.Millisecond*60).GetJSONAttribute("#non-existing", "data-widget-state", &m)
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("have JSON-parseable attribute"),
			))
		})
	})

	Describe("HaveDistinctCount", func() {
		It("counts distinct attribute values across matches", func() {
			Expect(".mark").To(b.HaveCount(4))
			Expect(".mark").To(b.HaveDistinctCount("data-key", 3))
			Eventually(".mark").Should(b.HaveDistinctCount("data-key", BeNumerically("<", 4)))
		})

		It("does not match the wrong count", func() {
			Expect(".mark").NotTo(b.HaveDistinctCount("data-key", 4))
		})
	})

	Describe("GetAttributes", func() {
		It("returns the requested raw attributes defined on the element", func() {
			a := b.GetAttributes("#link", "href", "data-role")
			Ω(a.GetString("href")).Should(Equal("/about"))
			Ω(a.GetString("data-role")).Should(Equal("nav"))
		})

		It("returns nil for absent attributes wrapped in AllowMissing", func() {
			a := b.GetAttributes("#link", "href", b.AllowMissing("data-missing"))
			Ω(a.GetString("href")).Should(Equal("/about"))
			Ω(a.Get("data-missing")).Should(BeNil())
		})

		It("fails if no attributes are requested", func() {
			b.GetAttributes("#link")
			ExpectFailures("GetAttributes requires at least one attribute to fetch")
		})

		It("times out for a required-but-absent attribute", func() {
			b.WithTimeout(time.Millisecond*60).GetAttributes("#link", "href", "data-missing")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("have attributes href, data-missing"),
			))
		})

		It("times out (poll-by-default) when the element never exists", func() {
			b.WithTimeout(time.Millisecond*60).GetAttributes("#non-existing", "href")
			ExpectFailures(ContainSubstring("Timed out after"))
		})

		It("fails fast under Immediate() when the element does not exist", func() {
			Ω(b.Immediate().GetAttributes("#non-existing", "href")).Should(BeNil())
			ExpectFailures(ContainSubstring("have attributes href"))
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
			b.Immediate().SetProperty(".notice", "foo.bar", "baz")
			ExpectFailures(ContainSubstring("could not resolve property component \".foo\": .notice"))
		})

		It("times out (poll-by-default) when the element never exists", func() {
			b.WithTimeout(time.Millisecond*60).SetProperty("#non-existing", "foo", "bar")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("could not find DOM element matching selector: #non-existing"),
			))
		})

		It("fails fast under Immediate() when the element does not exist", func() {
			b.Immediate().SetProperty("#non-existing", "foo", "bar")
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #non-existing"))
		})

		It("is a hard error to configure the bare-matcher form", func() {
			b.WithTimeout(time.Second).SetProperty("foo", "bar")
			ExpectFailures(ContainSubstring("SetProperty(...) returns a matcher"))
		})

		It("returns an error when the element does not exist in the matcher form", func() {
			match, err := b.SetProperty("tagName", "any").Match("#non-existing")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
		})
	})

	Describe("CurrentPropertyForEach", func() {
		It("fetches the requested property from all elements matching the selector", func() {
			values := b.CurrentPropertyForEach("input[type='radio'][name='appliances']", "value")
			Expect(values).To(HaveExactElements("toaster", "stove", "microwave"))

			values = b.CurrentPropertyForEach(b.XPath("div").WithID("check-boxes").Descendant("input").WithAttr("type", "checkbox"), "id")
			Expect(values).To(HaveExactElements("red", "blue", "yellow", "green"))

			values = b.CurrentPropertyForEach(".notice", "dataset.name")
			Expect(values).To(HaveExactElements("henry", "bob", BeNil()))
		})

		It("returns an empty array when no elements are found", func() {
			values := b.CurrentPropertyForEach("#non-existing", "href")
			Expect(values).To(BeEmpty())
		})

		It("returns nil values for elements that are found but don't have the property", func() {
			values := b.CurrentPropertyForEach("input[type='radio'][name='appliances']", "href")
			Expect(values).To(HaveExactElements(BeNil(), BeNil(), BeNil()))
		})
	})

	Describe("CurrentAttributeForEach", func() {
		It("fetches the requested attribute from all elements matching the selector", func() {
			values := b.CurrentAttributeForEach(".notice", "magic")
			Expect(values).To(HaveExactElements("on", "on", "off"))

			values = b.CurrentAttributeForEach(".notice", "data-name")
			Expect(values).To(HaveExactElements("henry", "bob", BeNil()))
		})

		It("returns an empty array when no elements are found", func() {
			values := b.CurrentAttributeForEach("#non-existing", "href")
			Expect(values).To(BeEmpty())
		})

		It("returns nil values for elements that are found but don't have the attribute", func() {
			values := b.CurrentAttributeForEach("input[type='radio'][name='appliances']", "data-missing")
			Expect(values).To(HaveExactElements(BeNil(), BeNil(), BeNil()))
		})
	})

	Describe("SetPropertyForEachImmediately", func() {
		It("sets the specified property to the same value on any matching elements", func() {
			Expect("#check-boxes input[type='checkbox']").To(b.EachHaveProperty("checked", true, false, false, false))
			b.SetPropertyForEachImmediately("#check-boxes input[type='checkbox']", "checked", true)
			Expect("#check-boxes input[type='checkbox']").To(b.EachHaveProperty("checked", true, true, true, true))

			Expect(".notice").To(b.EachHaveProperty("dataset.name", "henry", "bob", nil))
			b.SetPropertyForEachImmediately(".notice", "dataset.name", "John")
			Expect(".notice").To(b.EachHaveProperty("dataset.name", HaveEach("John")))
		})

		It("does nothing if no elements match", func() {
			b.SetPropertyForEachImmediately(".non-existing", "href", "http://example.com")
		})

		It("fails if a property can't be set because of delimiter issues", func() {
			b.SetPropertyForEachImmediately("li", "foo.bar", 3)
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

		It("fails (does not pass vacuously) when no elements match the selector", func() {
			// the defined-form fails on empty...
			mDefined := b.EachHaveProperty("href")
			Expect(".non-existing").NotTo(mDefined)
			Expect(mDefined.FailureMessage(".non-existing")).To(ContainSubstring("Expected at least one element to match .non-existing, but none did"))
			// ...and so does the value-matcher form, even when the value matcher would itself accept an empty slice
			mValue := b.EachHaveProperty("href", BeEmpty())
			Expect(".non-existing").NotTo(mValue)
			Expect(mValue.FailureMessage(".non-existing")).To(ContainSubstring("Expected at least one element to match .non-existing, but none did"))
		})
	})

	Describe("GetProperties", func() {
		It("returns the requested properties defined on the element", func() {
			// disabled is not a property of a <div>, so it must be AllowMissing (it comes back nil) - a
			// plain required "disabled" would block the two-axis poll forever
			p := b.GetProperties(".notice", "count", b.AllowMissing("disabled"), "tagName", "flavor", "dataset.name", "classList", "innerText", "dataset", b.AllowMissing("nonExisting"), b.AllowMissing("foo.bar.baz"))
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

		It("polls until every required property is defined, then returns them all", func() {
			go func() {
				defer GinkgoRecover()
				<-time.After(time.Millisecond * 100)
				b.Run(`document.querySelector(".notice").late = "here"`)
			}()
			p := b.GetProperties(".notice", "count", "late")
			Expect(p["count"]).To(Equal(3.0))
			Expect(p["late"]).To(Equal("here"))
		})

		It("times out for a required-but-undefined property", func() {
			b.WithTimeout(time.Millisecond*60).GetProperties(".notice", "count", "floop")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("have properties count, floop"),
			))
		})

		It("fails if no properties are requested", func() {
			b.GetProperties(".notice")
			ExpectFailures("GetProperties requires at least one property to fetch")
		})

		It("times out (poll-by-default) when the element does not exist", func() {
			b.WithTimeout(time.Millisecond*60).GetProperties("#non-existing", "tagName", "classList")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("have properties tagName, classList"),
			))
		})

		It("fails fast under Immediate() when the element does not exist", func() {
			Ω(b.Immediate().GetProperties("#non-existing", "tagName", "classList")).Should(BeNil())
			ExpectFailures(ContainSubstring("have properties tagName, classList"))
		})
	})

	Describe("CurrentPropertiesForEach", func() {
		It("returns all the requested properties defined on all matched elements", func() {
			p := b.CurrentPropertiesForEach(".notice", "count", "disabled", "tagName", "flavor", "dataset.name", "classList", "innerText", "dataset", "nonExisting", "foo.bar.baz")
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
			b.CurrentPropertiesForEach(".notice")
			ExpectFailures("CurrentPropertiesForEach requires at least one property to fetch")
		})

		It("returns an empty slice if no element is found", func() {
			Ω(b.CurrentPropertiesForEach("#non-existing", "tagName", "classList")).Should(HaveLen(0))
		})
	})

	Describe("CurrentAttributesForEach", func() {
		It("returns a snapshot of all the requested raw attributes for all matched elements", func() {
			p := b.CurrentAttributesForEach(".notice", "magic", "data-name", "data-missing")
			Expect(p).To(HaveLen(3))
			Ω(p.GetString("magic")).Should(Equal([]string{"on", "on", "off"}))
			// absent attributes come back as nil for that element (no AllowMissing axis, no poll)
			Ω(p.Get("data-name")).Should(HaveExactElements("henry", "bob", BeNil()))
			Ω(p.GetString("data-name")).Should(Equal([]string{"henry", "bob", ""}))
			Ω(p.Get("data-missing")).Should(HaveExactElements(BeNil(), BeNil(), BeNil()))
		})

		It("fails if no attributes are requested", func() {
			b.CurrentAttributesForEach(".notice")
			ExpectFailures("CurrentAttributesForEach requires at least one attribute to fetch")
		})

		It("returns an empty slice if no element is found (it does not poll)", func() {
			Ω(b.CurrentAttributesForEach("#non-existing", "href", "data-role")).Should(HaveLen(0))
		})
	})

	Describe("Click", func() {
		Context("when called directly (poll-by-default)", func() {
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

			It("polls until the element appears, then clicks it", func() {
				go func() {
					defer GinkgoRecover()
					<-time.After(time.Millisecond * 200)
					b.Run(`document.querySelector("#increment").id = "delayed-increment"`)
				}()
				b.Click("#delayed-increment")
				Ω("#counter-input").Should(b.HaveValue("1"))
			})

			It("times out (rather than failing immediately) if the element never exists", func() {
				b.WithTimeout(time.Millisecond * 60).Click("#non-existing")
				ExpectFailures(SatisfyAll(
					ContainSubstring("Timed out after"),
					ContainSubstring("could not find DOM element matching selector: #non-existing"),
				))
			})

			It("times out if the element stays invisible", func() {
				b.WithTimeout(time.Millisecond * 60).Click("#hidden-button")
				ExpectFailures(SatisfyAll(
					ContainSubstring("Timed out after"),
					ContainSubstring("DOM element is not visible: #hidden-button"),
				))
			})

			It("times out if the element stays disabled", func() {
				b.WithTimeout(time.Millisecond * 60).Click("#decrement")
				ExpectFailures(SatisfyAll(
					ContainSubstring("Timed out after"),
					ContainSubstring("DOM element is not enabled: #decrement"),
				))
			})
		})

		Context("when made Immediate() (the fail-fast escape hatch)", func() {
			It("clicks an actionable element once", func() {
				b.Immediate().Click("#increment")
				Ω("#counter-input").Should(b.HaveValue("1"))
			})

			It("fails fast if the element does not exist", func() {
				b.Immediate().Click("#non-existing")
				ExpectFailures(ContainSubstring("could not find DOM element matching selector: #non-existing"))
			})

			It("fails fast if the element is not visible", func() {
				b.Immediate().Click("#hidden-button")
				ExpectFailures(ContainSubstring("DOM element is not visible: #hidden-button"))
			})

			It("fails fast if the element is not enabled", func() {
				b.Immediate().Click("#decrement")
				ExpectFailures(ContainSubstring("DOM element is not enabled: #decrement"))
			})
		})

		Context("when configured but resolving to the bare-matcher form", func() {
			It("is a hard error to configure the matcher", func() {
				b.WithTimeout(time.Second).Click()
				ExpectFailures(ContainSubstring("click(...) returns a matcher"))
			})

			It("rejects Immediate() on the matcher form too", func() {
				b.Immediate().Click()
				ExpectFailures(ContainSubstring("click(...) returns a matcher"))
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

	Describe("ClickEachImmediately", func() {
		It("clicks on all matching elements - but only if they turn out to be clickable", func() {
			Ω("#red").Should(b.HaveValue(true))
			Ω("#blue").Should(b.HaveValue(false))
			Ω("#yellow").Should(b.HaveValue(false))
			Ω("#green").Should(b.HaveValue(false))
			b.ClickEachImmediately("[type='checkbox']")
			Ω("#red").Should(b.HaveValue(false))
			Ω("#blue").Should(b.HaveValue(true))
			Ω("#yellow").Should(b.HaveValue(false)) //disabled
			Ω("#green").Should(b.HaveValue(false))  //hidden
		})
	})

	Describe("DblClick", func() {
		Context("when called directly", func() {
			It("fires two clicks plus a dblclick", func() {
				b.DblClick("#dbl-btn")
				Ω("#dbl-clicks").Should(b.HaveInnerText("2"))
				Ω("#dbl-dblclicks").Should(b.HaveInnerText("1"))
			})

			It("times out if the element never exists", func() {
				b.WithTimeout(time.Millisecond * 60).DblClick("#non-existing")
				ExpectFailures(SatisfyAll(
					ContainSubstring("Timed out after"),
					ContainSubstring("could not find DOM element matching selector: #non-existing"),
				))
			})

			It("fails fast when Immediate() if the element is not enabled", func() {
				b.Immediate().DblClick("#disabled-dbl-btn")
				ExpectFailures(ContainSubstring("DOM element is not enabled: #disabled-dbl-btn"))
			})
		})

		Context("when used as a matcher", func() {
			It("double-clicks when polled", func() {
				Eventually("#dbl-btn").Should(b.DblClick())
				Ω("#dbl-dblclicks").Should(b.HaveInnerText("1"))
			})

			It("returns an error when the element does not exist", func() {
				match, err := b.DblClick().Match("#non-existing")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
			})
		})
	})

	Describe("RightClick", func() {
		Context("when called directly", func() {
			It("fires a contextmenu event", func() {
				b.RightClick("#ctx-btn")
				Ω("#ctx-result").Should(b.HaveInnerText("menu"))
			})

			It("times out if the element never exists", func() {
				b.WithTimeout(time.Millisecond * 60).RightClick("#non-existing")
				ExpectFailures(SatisfyAll(
					ContainSubstring("Timed out after"),
					ContainSubstring("could not find DOM element matching selector: #non-existing"),
				))
			})
		})

		Context("when used as a matcher", func() {
			It("right-clicks when polled", func() {
				Eventually("#ctx-btn").Should(b.RightClick())
				Ω("#ctx-result").Should(b.HaveInnerText("menu"))
			})
		})
	})

	Describe("DragTo", func() {
		It("drags the source onto the target", func() {
			b.DragTo("#drag-src", "#drop-zone")
			Ω("#drop-result").Should(b.HaveInnerText("dropped"))
		})

		It("drags when polled as a matcher (the subject is the source)", func() {
			Eventually("#drag-src").Should(b.DragTo("#drop-zone"))
			Ω("#drop-result").Should(b.HaveInnerText("dropped"))
		})

		It("polls both sides: waits for a late-arriving target", func() {
			go func() {
				defer GinkgoRecover()
				<-time.After(time.Millisecond * 200)
				b.Run(`document.querySelector("#drop-zone").id = "late-drop-zone"`)
			}()
			b.DragTo("#drag-src", "#late-drop-zone")
			Ω("#drop-result").Should(b.HaveInnerText("dropped"))
		})

		It("times out (poll-by-default) if the source never exists", func() {
			b.WithTimeout(time.Millisecond*60).DragTo("#non-existing", "#drop-zone")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("could not find DOM element matching selector: #non-existing"),
			))
		})

		It("times out (poll-by-default) if the target never exists", func() {
			b.WithTimeout(time.Millisecond*60).DragTo("#drag-src", "#non-existing")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("could not find DOM element matching target selector: #drag-src"),
			))
		})

		It("fails fast under Immediate() if the source element does not exist", func() {
			b.Immediate().DragTo("#non-existing", "#drop-zone")
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #non-existing"))
		})

		It("fails fast under Immediate() if the target element does not exist", func() {
			b.Immediate().DragTo("#drag-src", "#non-existing")
			ExpectFailures(ContainSubstring("could not find DOM element matching target selector: #drag-src"))
		})

		It("is a hard error to configure the bare-matcher form", func() {
			b.WithTimeout(time.Second).DragTo("#drop-zone")
			ExpectFailures(ContainSubstring("DragTo(...) returns a matcher"))
		})
	})

	Describe("Click with b.At(offset)", func() {
		It("clicks the element at the requested offset from its top-left corner", func() {
			b.Click("#click-pad", b.At(30, 40))
			Ω("#click-pad-result").Should(b.HaveInnerText("30,40"))
		})

		It("honors the offset in the matcher form too", func() {
			Eventually("#click-pad").Should(b.Click(b.At(30, 40)))
			Ω("#click-pad-result").Should(b.HaveInnerText("30,40"))
		})

		It("times out if the element never exists", func() {
			b.WithTimeout(time.Millisecond*60).Click("#non-existing", b.At(30, 40))
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("could not find DOM element matching selector: #non-existing"),
			))
		})
	})

	Describe("ScrollWheel", func() {
		It("fires the wheel handler and scrolls the element", func() {
			Ω(b.GetProperty("#scroll-box", "scrollTop")).Should(BeEquivalentTo(0))
			b.ScrollWheel("#scroll-box", 0, 200)
			Ω("#wheel-result").Should(b.HaveInnerText("wheeled"))
			Ω(b.GetProperty("#scroll-box", "scrollTop")).Should(BeEquivalentTo(200))
		})

		It("times out (poll-by-default) if the element never exists", func() {
			b.WithTimeout(time.Millisecond*60).ScrollWheel("#non-existing", 0, 200)
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("could not find DOM element matching selector: #non-existing"),
			))
		})

		It("fails fast under Immediate() if the element does not exist", func() {
			b.Immediate().ScrollWheel("#non-existing", 0, 200)
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #non-existing"))
		})

		It("is a hard error to configure the bare-matcher form", func() {
			b.WithTimeout(time.Second).ScrollWheel(0, 200)
			ExpectFailures(ContainSubstring("ScrollWheel(...) returns a matcher"))
		})

		It("returns a matcher when under-applied, polling until the element is present", func() {
			Ω(b.GetProperty("#scroll-box", "scrollTop")).Should(BeEquivalentTo(0))
			Eventually("#scroll-box").Should(b.ScrollWheel(0, 200))
			Ω("#wheel-result").Should(b.HaveInnerText("wheeled"))
			Ω(b.GetProperty("#scroll-box", "scrollTop")).Should(BeEquivalentTo(200))
		})

		It("fails the matcher when the deltas are not numeric", func() {
			b.ScrollWheel("not-a-delta", "nope")
			ExpectFailures("ScrollWheel requires numeric deltaX and deltaY")
		})
	})

	Describe("MiddleClick", func() {
		Context("when called directly", func() {
			It("fires an auxclick event", func() {
				b.MiddleClick("#aux-btn")
				Ω("#aux-result").Should(b.HaveInnerText("middle"))
			})

			It("times out if the element never exists", func() {
				b.WithTimeout(time.Millisecond * 60).MiddleClick("#non-existing")
				ExpectFailures(SatisfyAll(
					ContainSubstring("Timed out after"),
					ContainSubstring("could not find DOM element matching selector: #non-existing"),
				))
			})
		})

		Context("when used as a matcher", func() {
			It("middle-clicks when polled", func() {
				Eventually("#aux-btn").Should(b.MiddleClick())
				Ω("#aux-result").Should(b.HaveInnerText("middle"))
			})

			It("returns an error when the element does not exist", func() {
				match, err := b.MiddleClick().Match("#non-existing")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
			})
		})
	})

	Describe("Click with modifier options", func() {
		It("clicks with a single modifier", func() {
			b.Click("#mod-btn", b.Shift())
			Ω("#mod-result").Should(b.HaveInnerText("shift"))
		})

		It("clicks with multiple modifiers", func() {
			b.Click("#mod-btn", b.Shift(), b.Meta())
			Ω("#mod-result").Should(b.HaveInnerText("shift+meta"))
		})

		It("clicks with no modifiers (the native fast path)", func() {
			b.Click("#mod-btn")
			Ω("#mod-result").Should(b.HaveInnerText("none"))
		})

		It("carries modifiers through the matcher form", func() {
			Eventually("#mod-btn").Should(b.Click(b.Ctrl(), b.Alt()))
			Ω("#mod-result").Should(b.HaveInnerText("control+alt"))
		})

		It("combines an offset and modifiers on the same click", func() {
			b.Click("#mod-btn", b.At(2, 2), b.Shift())
			Ω("#mod-result").Should(b.HaveInnerText("shift"))
		})

		It("times out if the element never exists", func() {
			b.WithTimeout(time.Millisecond*60).Click("#non-existing", b.Shift())
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("could not find DOM element matching selector: #non-existing"),
			))
		})

		It("auto-fails when handed a non-option in the option position", func() {
			b.Click("#mod-btn", "oops")
			ExpectFailures(ContainSubstring("expected a selector or a biloba pointer option"))
		})
	})

	Describe("Tap", func() {
		Context("when called directly", func() {
			It("fires a touchend event", func() {
				b.Tap("#tap-btn")
				Ω("#tap-result").Should(b.HaveInnerText("tapped"))
			})

			It("times out if the element never exists", func() {
				b.WithTimeout(time.Millisecond * 60).Tap("#non-existing")
				ExpectFailures(SatisfyAll(
					ContainSubstring("Timed out after"),
					ContainSubstring("could not find DOM element matching selector: #non-existing"),
				))
			})
		})

		Context("when used as a matcher", func() {
			It("taps when polled", func() {
				Eventually("#tap-btn").Should(b.Tap())
				Ω("#tap-result").Should(b.HaveInnerText("tapped"))
			})

			It("returns an error when the element does not exist", func() {
				match, err := b.Tap().Match("#non-existing")
				Ω(match).Should(BeFalse())
				Ω(err).Should(MatchError("could not find DOM element matching selector: #non-existing"))
			})
		})
	})

	Describe("invokeOn and invokeOnEach", func() {
		It("invokes the requested function on the selected dom element", func() {
			b.InvokeOn("#increment", "click")
			Ω("#counter-input").Should(b.HaveValue("1"))

			checked := b.CurrentPropertiesForEach(".clickable[type='checkbox']", "checked").GetBool("checked")
			b.InvokeOnEachImmediately(".clickable[type='checkbox']", "click")
			newChecked := b.CurrentPropertiesForEach(".clickable[type='checkbox']", "checked").GetBool("checked")
			for i := range checked {
				Ω(newChecked[i]).Should(Equal(!checked[i]))
			}

			b.InvokeOn(".notice", "append", " I Can Add To")
			Ω(".notice").Should(b.HaveInnerText("Some Text I Can Add To"))

			texts := b.CurrentInnerTextForEach("li")
			b.InvokeOnEachImmediately("li", "append", "!")
			newTexts := b.CurrentInnerTextForEach("li")
			for i := range texts {
				Ω(newTexts[i]).Should(Equal(texts[i] + "!"))
			}

			result := b.InvokeOn(".notice", "getAttributeNames")
			Ω(result).Should(ConsistOf("class", "magic", "data-name"))

			initial := b.InvokeOnEachImmediately(".notice", "getAttribute", "magic")
			Ω(initial).Should(HaveExactElements("on", "on", "off"))
			b.InvokeOnEachImmediately(".notice", "setAttribute", "magic", "on")
			subsequent := b.InvokeOnEachImmediately(".notice", "getAttribute", "magic")
			Ω(subsequent).Should(HaveExactElements("on", "on", "on"))
		})

		It("polls until the element appears, then invokes on it", func() {
			go func() {
				defer GinkgoRecover()
				<-time.After(time.Millisecond * 100)
				b.Run(`document.querySelector("#increment").id = "delayed-increment"`)
			}()
			b.InvokeOn("#delayed-increment", "click")
			Ω("#counter-input").Should(b.HaveValue("1"))
		})

		It("times out (poll-by-default) if the dom element does not exist", func() {
			b.WithTimeout(time.Millisecond*60).InvokeOn("#non-existing", "click")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("respond to \"click\""),
			))

			b.InvokeOnEachImmediately("#non-existing", "click") // does nothing
		})

		It("fails fast (does not poll) if the function does not exist", func() {
			b.Immediate().InvokeOn(".notice", "encabulate")
			ExpectFailures(ContainSubstring("element does not implement \"encabulate\": .notice"))

			b.InvokeOnEachImmediately(".notice", "encabulate") // does nothing
		})
	})

	Describe("invokeWith and invokeWithEach", func() {
		It("invokes the passed in script passing it the node and any additional arguments", func() {
			count := b.Count("ol li")
			r := b.InvokeWith("ol", "(n) => { li = document.createElement('li'); li.innerText = 'new' ; n.appendChild(li); return 'done' }")
			Ω(r).Should(Equal("done"))
			Ω(b.Count("ol li")).Should(Equal(count + 1))

			b.InvokeWith("ol", "(n, text) => { li = document.createElement('li'); li.innerText = text ; n.appendChild(li)}", "yet another")
			Ω(b.Count("ol li")).Should(Equal(count + 2))
			Ω(b.XPath("ol").Descendant("li").Last()).Should(b.HaveInnerText("yet another"))

			r = b.InvokeWithEachImmediately(".notice", "(n) => n.dataset.name ? n.dataset.name + '!' : 'who?'")
			Ω(r).Should(ConsistOf("henry!", "bob!", "who?"))

			Ω(b.CurrentPropertiesForEach(".notice", "count").GetInt("count")).Should(HaveExactElements(3, 0, 0))
			b.InvokeWithEachImmediately(".notice", "(n, count) => n.count = n.count || count", 17)
			Ω(b.CurrentPropertiesForEach(".notice", "count").GetInt("count")).Should(HaveExactElements(3, 17, 17))
		})

		It("times out (poll-by-default) if the dom element does not exist", func() {
			b.WithTimeout(time.Millisecond*60).InvokeWith("#non-existing", "(n) => 1")
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring("be invokable"),
			))

			r := b.InvokeOnEachImmediately("#non-existing", "(n) => 1") // does nothing
			Ω(r).Should(BeEmpty())
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
			b.Immediate().Click(b.XPath("button").WithText("nope"))
			ExpectFailures("\ncould not find DOM element matching selector: //button[text()=\"nope\"]")
		})
	})

	Describe("escaping strings correctly", func() {
		It("honors the user's escaping of strings when using css selectors", func() {
			Ω(`[data-name="McDonald's"]`).Should(b.HaveInnerText("Big Mac"))
			Ω(`[data-name='McDonald"s']`).Should(b.HaveInnerText("Bigger Mac"))
			Ω(`[data-name='Burger King']`).Should(b.HaveInnerText("Filet'o'fish"))
			Ω(`#weird\:strings\#oh\"oh`).Should(b.Exist())
			Ω(`.weirder\:strings\#oh\'oh`).Should(b.Exist())
		})
		It("correctly escapes weird strings when using XPath", func() {
			Ω(b.XPath().WithID(`weird:strings#oh"oh`)).Should(b.Exist())
			Ω(b.XPath().WithClass(`weirder:strings#oh"oh`)).Should(b.Exist())
			Ω(b.XPath().WithID("weird:strings#oh\"oh")).Should(b.Exist())
			Ω(b.XPath().WithClass("weirder:strings#oh\"oh")).Should(b.Exist())
			Ω(b.XPath().WithText("Filet'o'fish")).Should(b.HaveProperty("dataset.name", "Burger King"))
			Ω(b.XPath().WithText("Filet'o'fish")).Should(b.HaveProperty("dataset.name", "Burger King"))
			Ω(b.XPath().WithAttr("data-name", "McDonald's")).Should(b.HaveInnerText("Big Mac"))
			Ω(b.XPath().WithClass("weirder:strings#oh'oh")).Should(b.HaveInnerText("Big Mac"))
			Ω(b.XPath().WithAttr("data-name", "McDonald\"s")).Should(b.HaveInnerText("Bigger Mac"))
			Ω(b.XPath().WithText(`"Something magic"al""!!'`)).Should(b.HaveProperty("dataset.name", "White-Castle"))
			Ω(b.XPath().WithTextContains(`"Something magic"al""!!'`)).Should(b.HaveProperty("dataset.name", "White-Castle"))
			Ω(b.XPath().WithTextStartsWith(`"Something magic"al""!!'`)).Should(b.HaveProperty("dataset.name", "White-Castle"))
		})
	})
})

package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba"
)

var _ = Describe("Role / text / label locators", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/locator.html")
		Eventually("#heading").Should(b.Exist())
	})

	Describe("ByRole", func() {
		It("matches an element by role and accessible name", func() {
			Expect(b.ByRole("button").WithName("Save")).To(b.Exist())
			Expect(b.GetInnerText(b.ByRole("button").WithName("Save"))).To(Equal("Save"))
		})

		It("matches a heading by name", func() {
			Expect(b.ByRole("heading").WithName("Getting Started")).To(b.BeVisible())
		})

		It("matches an implicit link role", func() {
			Expect(b.GetProperty(b.ByRole("link").WithName("Documentation"), "tagName")).To(Equal("A"))
		})

		It("uses aria-label as the accessible name", func() {
			Expect(b.ByRole("button").WithName("Close dialog")).To(b.Exist())
		})

		It("matches by accessible name substring", func() {
			Expect(b.ByRole("heading").WithNameContains("Getting")).To(b.Exist())
		})

		It("matches all elements of a role when no name is given", func() {
			Expect(b.ByRole("button")).To(b.HaveCount(15))
		})

		It("flows through actions (Click)", func() {
			b.Click(b.ByRole("button").WithName("Save"))
			Expect("#clicked").To(b.HaveInnerText("Save"))
		})

		It("flows through realistic mode", func() {
			b.Realistic().Click(b.ByRole("button").WithName("Save"))
			Eventually("#clicked").Should(b.HaveInnerText("Save"))
		})
	})

	Describe("ByText", func() {
		It("matches the smallest element with exact visible text", func() {
			Expect(b.GetProperty(b.ByText("Save"), "tagName")).To(Equal("BUTTON"))
		})

		It("matches by substring", func() {
			Expect(b.ByTextContains("Welcome back")).To(b.Exist())
		})
	})

	Describe("ByLabel", func() {
		It("matches a form control by its <label for=...>", func() {
			b.SetValue(b.ByLabel("Email"), "jane@example.com")
			Expect(b.ByLabel("Email")).To(b.HaveValue("jane@example.com"))
		})

		It("matches a form control by its wrapping <label>", func() {
			Expect(b.GetProperty(b.ByLabel("Password"), "id")).To(Equal("pw"))
		})

		It("matches a form control by aria-label", func() {
			Expect(b.GetProperty(b.ByLabel("Site search"), "id")).To(Equal("search"))
		})

		It("matches a form control by its placeholder", func() {
			Expect(b.GetProperty(b.ByLabel("Phone number"), "id")).To(Equal("phone"))
		})
	})

	Describe("Within", func() {
		It("restricts matches to descendants of the scope element", func() {
			Expect(b.ByRole("button").WithName("Delete")).To(b.HaveCount(2))
			Expect(b.ByRole("button").WithName("Delete").Within("#dialog-a")).To(b.HaveCount(1))
			b.Click(b.ByRole("button").WithName("Delete").Within("#dialog-b"))
		})

		It("accepts a CSS scope and finds the right element", func() {
			Expect(b.GetProperty(b.ByText("Delete").Within("#dialog-a"), "tagName")).To(Equal("BUTTON"))
		})

		It("matches nothing when the scope is not found", func() {
			Expect(b.ByRole("button").WithName("Delete").Within("#nope")).To(b.HaveCount(0))
			Expect(b.ByRole("button").WithName("Delete").Within("#nope")).NotTo(b.Exist())
		})
	})

	Describe("NotWithin", func() {
		It("excludes matches nested inside the scope", func() {
			// "Continue" text appears both inside #quiz (a span) and as a sibling button after it
			Expect(b.ByText("Continue")).To(b.HaveCount(2))
			Expect(b.ByText("Continue").NotWithin("#quiz")).To(b.HaveCount(1))
			Expect(b.GetProperty(b.ByText("Continue").NotWithin("#quiz"), "tagName")).To(Equal("BUTTON"))
		})

		It("composes with the document-order matchers to mean follows-in-flow-not-nested", func() {
			// the surviving match is the sibling button, which follows #quiz in document order
			Expect(b.ByText("Continue").NotWithin("#quiz")).To(b.BePrecededBy("#quiz"))
			// the nested span would (wrongly) also be "preceded by" #quiz on document order alone -
			// NotWithin is what removes it from consideration
			Expect(b.ByText("Continue").NotWithin("#quiz")).NotTo(b.BeFollowedBy("#quiz"))
		})
	})

	Describe("Nth / First / Last", func() {
		It("selects the first match", func() {
			Expect(b.GetInnerText(b.ByRole("listitem").Within("#fruits").First())).To(Equal("Apple"))
		})

		It("selects the nth match", func() {
			Expect(b.GetInnerText(b.ByRole("listitem").Within("#fruits").Nth(2))).To(Equal("Cherry"))
		})

		It("selects the last match", func() {
			Expect(b.GetInnerText(b.ByRole("listitem").Within("#fruits").Last())).To(Equal("Date"))
		})

		It("matches nothing for an out-of-range index", func() {
			Expect(b.ByRole("listitem").Nth(99)).NotTo(b.Exist())
			Expect(b.ByRole("listitem").Nth(99)).To(b.HaveCount(0))
		})

		It("composes with Within", func() {
			Expect(b.GetInnerText(b.ByRole("listitem").Within("#fruits").Last())).To(Equal("Date"))
		})
	})

	Describe("shadow-DOM piercing", func() {
		It("finds a role+name match inside an open shadow root", func() {
			Expect(b.ByRole("button").WithName("Shadow Action")).To(b.Exist())
		})

		It("flows through actions into the shadow root", func() {
			Eventually(b.ByRole("button").WithName("Shadow Action")).Should(b.BeVisible())
		})
	})

	Describe("ByPlaceholder / ByAltText / ByTitle / ByTestID", func() {
		It("matches an input by placeholder", func() {
			Expect(b.GetProperty(b.ByPlaceholder("Phone number"), "id")).To(Equal("phone"))
			Expect(b.GetProperty(b.ByPlaceholderContains("Phone"), "id")).To(Equal("phone"))
		})

		It("matches an element by alt text", func() {
			Expect(b.GetProperty(b.ByAltText("Company logo"), "id")).To(Equal("logo"))
			Expect(b.ByAltTextContains("logo")).To(b.Exist())
		})

		It("matches an element by title", func() {
			Expect(b.GetProperty(b.ByTitle("Tooltip help"), "id")).To(Equal("help"))
		})

		It("matches an element by test id", func() {
			Expect(b.GetInnerText(b.ByTestID("submit-btn"))).To(Equal("Go"))
		})

		It("honors a custom TestIDAttribute", func() {
			biloba.TestIDAttribute = "data-test"
			DeferCleanup(func() { biloba.TestIDAttribute = "data-testid" })
			b.Run(`document.querySelector('[data-testid=submit-btn]').setAttribute('data-test', 'custom')`)
			Expect(b.ByTestID("custom")).To(b.Exist())
		})
	})

	Describe("ContainingText / NotContainingText", func() {
		It("filters a role to elements whose visible text contains a string", func() {
			Expect(b.ByRole("listitem").ContainingText("Product 2")).To(b.HaveCount(1))
			Expect(b.GetInnerText(b.ByRole("listitem").ContainingText("Product 2"))).To(ContainSubstring("Product 2"))
		})

		It("filters out elements containing a string", func() {
			// of the three #products items, only "Product 2" has no Remove button
			Expect(b.ByRole("listitem").Within("#products").NotContainingText("Remove")).To(b.HaveCount(1))
		})
	})

	Describe("Containing / NotContaining", func() {
		It("filters a role to elements that have a matching descendant", func() {
			Expect(b.ByRole("listitem").Containing(".del")).To(b.HaveCount(2))
		})

		It("accepts a sub-locator as the descendant matcher", func() {
			Expect(b.ByRole("listitem").Containing(b.ByRole("button").WithName("Remove"))).To(b.HaveCount(2))
		})

		It("filters out elements that have a matching descendant", func() {
			Expect(b.GetInnerText(b.ByRole("listitem").Within("#products").NotContaining(".del"))).To(ContainSubstring("Product 2"))
		})
	})

	Describe("And / Or", func() {
		It("intersects with another selector", func() {
			Expect(b.ByRole("button").And(".primary")).To(b.HaveCount(1))
			Expect(b.GetInnerText(b.ByRole("button").And(".primary"))).To(Equal("Primary Action"))
		})

		It("unions with another selector", func() {
			both := b.ByRole("button").WithName("Primary Action").Or(b.ByRole("button").WithName("Secondary Action"))
			Expect(both).To(b.HaveCount(2))
		})

		It("unions in document order regardless of operand order", func() {
			both := b.ByRole("button").WithName("Secondary Action").Or(b.ByRole("button").WithName("Primary Action"))
			Expect(b.GetInnerText(both.First())).To(Equal("Primary Action")) // primary precedes secondary in the DOM
		})
	})

	Describe("ByCSS", func() {
		It("takes a raw CSS selector into the locator algebra", func() {
			Expect(b.ByCSS("#fruits li")).To(b.HaveCount(4))
			Expect(b.GetInnerText(b.ByCSS("#fruits li").First())).To(Equal("Apple"))
			Expect(b.GetInnerText(b.ByCSS("#fruits li").Nth(1))).To(Equal("Banana"))
			Expect(b.GetInnerText(b.ByCSS("#fruits li").Last())).To(Equal("Date"))
		})

		It("composes with Within and the filters", func() {
			Expect(b.ByCSS("li").Within("#products")).To(b.HaveCount(3))
			Expect(b.GetInnerText(b.ByCSS("li").Within("#products").NotContaining(".del"))).To(ContainSubstring("Product 2"))
		})

		It("combines with semantic locators via And", func() {
			Expect(b.GetInnerText(b.ByRole("button").And(b.ByCSS(".primary")))).To(Equal("Primary Action"))
		})

		It("matches nothing for an out-of-range ordinal", func() {
			Expect(b.ByCSS("#fruits li").Nth(99)).NotTo(b.Exist())
		})
	})

	Describe("Level (heading level)", func() {
		It("matches a heading at a given level", func() {
			Expect(b.GetInnerText(b.ByRole("heading").Level(1))).To(Equal("Locators"))
			Expect(b.GetInnerText(b.ByRole("heading").Level(2))).To(Equal("Getting Started"))
			Expect(b.GetInnerText(b.ByRole("heading").Level(3))).To(Equal("Subsection"))
		})

		It("composes with a name filter", func() {
			Expect(b.ByRole("heading").Level(2).WithName("Getting Started")).To(b.Exist())
			Expect(b.ByRole("heading").Level(3).WithName("Getting Started")).NotTo(b.Exist())
		})
	})

	Describe("ARIA state filters", func() {
		It("filters by checked", func() {
			Expect(b.ByRole("checkbox").Checked()).To(b.HaveCount(1))
			Expect(b.GetProperty(b.ByRole("checkbox").Checked(), "id")).To(Equal("checked-box"))
		})

		It("filters by disabled", func() {
			Expect(b.GetProperty(b.ByRole("button").Disabled(), "id")).To(Equal("disabled-btn"))
		})

		It("filters by expanded", func() {
			Expect(b.GetProperty(b.ByRole("button").Expanded(), "id")).To(Equal("menu-btn"))
		})

		It("filters by pressed", func() {
			Expect(b.GetProperty(b.ByRole("button").Pressed(), "id")).To(Equal("toggle-btn"))
		})

		It("filters by selected", func() {
			Expect(b.GetProperty(b.ByRole("option").Selected(), "id")).To(Equal("opt-2"))
		})
	})
})

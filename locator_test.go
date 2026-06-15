package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Role / text / label locators", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/locator.html")
		Eventually("#heading").Should(b.Exist())
	})

	Describe("ByRole", func() {
		It("matches an element by role and accessible name", func() {
			Expect(b.ByRole("button").WithName("Save")).To(b.Exist())
			Expect(b.InnerText(b.ByRole("button").WithName("Save"))).To(Equal("Save"))
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
			Expect(b.ByRole("button")).To(b.HaveCount(6))
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

	Describe("Nth / First / Last", func() {
		It("selects the first match", func() {
			Expect(b.InnerText(b.ByRole("listitem").First())).To(Equal("Apple"))
		})

		It("selects the nth match", func() {
			Expect(b.InnerText(b.ByRole("listitem").Nth(2))).To(Equal("Cherry"))
		})

		It("selects the last match", func() {
			Expect(b.InnerText(b.ByRole("listitem").Last())).To(Equal("Date"))
		})

		It("matches nothing for an out-of-range index", func() {
			Expect(b.ByRole("listitem").Nth(99)).NotTo(b.Exist())
			Expect(b.ByRole("listitem").Nth(99)).To(b.HaveCount(0))
		})

		It("composes with Within", func() {
			Expect(b.InnerText(b.ByRole("listitem").Within("#fruits").Last())).To(Equal("Date"))
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
})

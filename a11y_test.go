package biloba_test

import (
	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("A11yOutline", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/interactions.html")
		Eventually("#hello").Should(b.Exist())
	})

	It("renders the accessibility tree as role/name lines", func() {
		outline := b.A11yOutline()

		Expect(outline).To(ContainSubstring(`RootWebArea "Interactions Testpage"`))
		Expect(outline).To(ContainSubstring(`heading "Interactions Testpage"`))
		Expect(outline).To(ContainSubstring(`button "Disabled"`))
		Expect(outline).To(ContainSubstring("textbox"))
	})

	It("indents children beneath their parents", func() {
		outline := b.A11yOutline()
		// the heading sits one level deep; its StaticText child is nested one level deeper still
		Expect(outline).To(ContainSubstring("  heading \"Interactions Testpage\"\n    StaticText \"Interactions Testpage\""))
	})

	It("prunes presentational InlineTextBox noise", func() {
		Expect(b.A11yOutline()).NotTo(ContainSubstring("InlineTextBox"))
	})

	It("reflects accessible values", func() {
		// a file input exposes its 'No file chosen' value through the AX tree
		Expect(b.A11yOutline()).To(ContainSubstring(`(value: "No file chosen")`))
	})

	It("caps very large outlines", func() {
		Expect(len(biloba.CapOutlineForTest("x", 1))).To(Equal(1))
	})
})

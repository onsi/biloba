package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Selecting text", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/selection.html")
		Eventually("#passage").Should(b.Exist())
	})

	selectedText := func() string { return b.Run(`window.getSelection().toString()`).(string) }

	Describe("SelectText", func() {
		It("selects all of the element's text", func() {
			b.SelectText("#passage")
			Ω(selectedText()).Should(Equal("The quick brown fox"))
		})

		It("dispatches a mouseup so selection-driven UIs react", func() {
			b.SelectText("#passage")
			Ω(b.Run(`window._lastMouseUpTarget`)).Should(Equal("passage"))
			Ω("#menu").Should(b.HaveClass("open"))
		})

		It("works in the matcher form", func() {
			Eventually("#passage").Should(b.SelectText())
			Ω(selectedText()).Should(Equal("The quick brown fox"))
		})

		It("fails the spec when no element matches", func() {
			b.SelectText("#non-existing")
			ExpectFailures(ContainSubstring("Failed to select text"))
		})
	})

	Describe("SelectRange", func() {
		It("selects a sub-range by character offset", func() {
			b.SelectRange("#passage", 4, 9)
			Ω(selectedText()).Should(Equal("quick"))
		})

		It("selects a range that spans multiple text nodes", func() {
			// #rich textContent is "Hello brave world"; chars 6..11 land inside the nested <strong>
			b.SelectRange("#rich", 6, 11)
			Ω(selectedText()).Should(Equal("brave"))
		})

		It("dispatches a mouseup", func() {
			b.SelectRange("#passage", 4, 9)
			Ω(b.Run(`window._lastMouseUpTarget`)).Should(Equal("passage"))
		})

		It("works in the matcher form", func() {
			Eventually("#passage").Should(b.SelectRange(4, 9))
			Ω(selectedText()).Should(Equal("quick"))
		})

		It("fails the spec when the range is out of bounds", func() {
			b.SelectRange("#passage", 0, 999)
			ExpectFailures(ContainSubstring("out of bounds"))
		})

		It("fails the spec when no element matches", func() {
			b.SelectRange("#non-existing", 0, 1)
			ExpectFailures(ContainSubstring("Failed to select range"))
		})
	})

	Describe("ClearSelection", func() {
		It("clears an active selection", func() {
			b.SelectText("#passage")
			Ω(selectedText()).Should(Equal("The quick brown fox"))
			b.ClearSelection()
			Ω(selectedText()).Should(Equal(""))
		})

		It("is a no-op when nothing is selected", func() {
			b.ClearSelection()
			Ω(selectedText()).Should(Equal(""))
		})
	})
})

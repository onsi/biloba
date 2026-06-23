package biloba_test

import (
	"strings"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Outline", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/outline.html")
		Eventually("#greeting").Should(b.Exist())
	})

	It("returns a text representation of the DOM body", func() {
		outline := b.Outline()
		// Basic elements are present
		Ω(outline).Should(ContainSubstring(`<div id="greeting">`))
		Ω(outline).Should(ContainSubstring("Hello Outline!"))
		Ω(outline).Should(ContainSubstring(`<p id="para">`))
		Ω(outline).Should(ContainSubstring(`<strong>`))
		Ω(outline).Should(ContainSubstring("Some"))
		Ω(outline).Should(ContainSubstring("bold"))
		Ω(outline).Should(ContainSubstring("text"))
	})

	It("prunes script body content but keeps the script tag", func() {
		outline := b.Outline()
		Ω(outline).Should(ContainSubstring("<script>"))
		// Script body is replaced with a pruning marker, not the actual JS
		Ω(outline).ShouldNot(ContainSubstring("window.outlineLoaded"))
		Ω(outline).ShouldNot(ContainSubstring("var x = 42"))
	})

	It("prunes svg body content but keeps the svg tag", func() {
		outline := b.Outline()
		Ω(outline).Should(ContainSubstring(`<svg id="chart"`))
		// SVG children are pruned
		Ω(outline).ShouldNot(ContainSubstring("<circle"))
		Ω(outline).ShouldNot(ContainSubstring("<text"))
	})

	It("collapses whitespace in text nodes", func() {
		outline := b.Outline()
		// The fixture #spaced div has "Words   with    extra    spaces"; after collapsing it should be a single space between words
		Ω(outline).Should(ContainSubstring("Words with extra spaces"))
	})

	It("returns indented output", func() {
		outline := b.Outline()
		// Children are indented relative to parents
		lines := strings.Split(outline, "\n")
		var greetingLine, helloLine string
		for _, line := range lines {
			if strings.Contains(line, `id="greeting"`) {
				greetingLine = line
			}
			if strings.Contains(line, "Hello Outline!") {
				helloLine = line
			}
		}
		Ω(greetingLine).ShouldNot(BeEmpty())
		Ω(helloLine).ShouldNot(BeEmpty())
		// The text content of the div is indented one level deeper
		greetingIndent := len(greetingLine) - len(strings.TrimLeft(greetingLine, " "))
		helloIndent := len(helloLine) - len(strings.TrimLeft(helloLine, " "))
		Ω(helloIndent).Should(BeNumerically(">", greetingIndent))
	})

	It("truncates oversized output with a truncation marker", func() {
		truncated := biloba.CapOutlineForTest("a\nb\nc\nd\ne\nf", 5)
		Ω(truncated).Should(HaveSuffix("\n... [truncated]"))
		Ω(truncated).ShouldNot(ContainSubstring("f"))
	})

	It("does not truncate when the cap is negative (BILOBA_OUTLINE_MAX=0/off)", func() {
		full := biloba.CapOutlineForTest("a\nb\nc\nd\ne\nf", -1)
		Ω(full).Should(Equal("a\nb\nc\nd\ne\nf"))
		Ω(full).ShouldNot(ContainSubstring("[truncated]"))
	})
})

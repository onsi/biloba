package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Piercing shadow DOM and same-origin iframes with >>>", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/shadow.html")
		Eventually("#hello").Should(b.Exist())
	})

	Describe("open shadow roots", func() {
		It("finds, reads, and acts on elements inside a shadow root", func() {
			Eventually("my-widget >>> .shadow-btn").Should(b.Exist())
			Expect("my-widget >>> .label").To(b.HaveInnerText("Shadow Label"))

			b.Click("my-widget >>> .shadow-btn")
			Eventually("my-widget >>> .shadow-btn").Should(b.HaveInnerText("Clicked"))
		})

		It("supports the *Each / count forms across the boundary", func() {
			Expect("my-widget >>> .item").To(b.HaveCount(2))
			Expect(b.InnerTextForEach("my-widget >>> .item")).To(Equal([]string{"A", "B"}))
		})

		It("reports the full selector when a boundary can't be crossed", func() {
			// #hello is a plain element with no shadow root, so the boundary can't be crossed
			b.Click("#hello >>> .nope")
			ExpectFailures(ContainSubstring("could not find DOM element matching selector: #hello >>> .nope"))
		})
	})

	Describe("same-origin iframes", func() {
		It("finds and acts on elements inside the iframe document", func() {
			Eventually("#inner >>> #iframe-btn").Should(b.Exist())

			b.Click("#inner >>> #iframe-btn")
			Eventually("#inner >>> #iframe-btn").Should(b.HaveInnerText("Iframe Clicked"))
		})

		It("supports counting across the iframe boundary", func() {
			Eventually("#inner >>> .cell").Should(b.HaveCount(2))
		})
	})
})

package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Windows", func() {
	It("has a default window size", func() {
		width, height := b.WindowSize()
		Ω(width).Should(Equal(1024))
		Ω(height).Should(Equal(768))
		Ω(`window.innerWidth`).Should(b.EvaluateTo(1024.))
		Ω(`window.innerHeight`).Should(b.EvaluateTo(768.))
	})

	Describe("overriding window size and resetting it", Ordered, func() {
		It("can override the window size", func() {
			b.SetWindowSize(800, 1000)
			Ω(`window.innerWidth`).Should(b.EvaluateTo(800.))
			Ω(`window.innerHeight`).Should(b.EvaluateTo(1000.))

			b.SetWindowSize(200, 100)
			Ω(`window.innerWidth`).Should(b.EvaluateTo(200.))
			Ω(`window.innerHeight`).Should(b.EvaluateTo(100.))
		})

		It("resets it for the next spec", func() {
			width, height := b.WindowSize()
			Ω(width).Should(Equal(1024))
			Ω(height).Should(Equal(768))
			Ω(`window.innerWidth`).Should(b.EvaluateTo(1024.))
			Ω(`window.innerHeight`).Should(b.EvaluateTo(768.))
		})
	})
})

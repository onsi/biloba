package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba"
)

var _ = Describe("Emulation", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())
	})

	Describe("SetMedia", func() {
		// The full-chrome lane follows the OS colour scheme, so the specs read the ambient scheme and
		// emulate the other one: a hardcoded "dark" would be a no-op on a machine already in dark mode.
		var ambient, other string
		BeforeEach(func() {
			ambient, other = "light", "dark"
			if b.Run(`matchMedia("(prefers-color-scheme: dark)").matches`) == true {
				ambient, other = "dark", "light"
			}
		})

		prefers := func(feature string) string {
			return `matchMedia("(` + feature + `)").matches`
		}

		Describe("emulating and resetting", Ordered, func() {
			It("emulates the colour scheme, reduced motion, and media type, across navigations", func() {
				b.SetMedia(biloba.Media{ColorScheme: other, ReducedMotion: "reduce", Type: "print"})
				Ω(prefers("prefers-color-scheme: " + other)).Should(b.EvaluateTo(true))
				Ω(prefers("prefers-reduced-motion: reduce")).Should(b.EvaluateTo(true))
				Ω(`matchMedia("print").matches`).Should(b.EvaluateTo(true))

				b.Navigate(fixtureServer + "/dom.html")
				Eventually("#hello").Should(b.Exist())
				Ω(prefers("prefers-color-scheme: " + other)).Should(b.EvaluateTo(true))
				Ω(prefers("prefers-reduced-motion: reduce")).Should(b.EvaluateTo(true))
			})

			It("resets it for the next spec", func() {
				Ω(prefers("prefers-color-scheme: " + ambient)).Should(b.EvaluateTo(true))
				Ω(prefers("prefers-reduced-motion: reduce")).Should(b.EvaluateTo(false))
				Ω(`matchMedia("print").matches`).Should(b.EvaluateTo(false))
			})
		})

		It("replaces the previous media rather than merging into it", func() {
			b.SetMedia(biloba.Media{ColorScheme: other, ReducedMotion: "reduce"})
			b.SetMedia(biloba.Media{ReducedMotion: "reduce"})
			Ω(prefers("prefers-color-scheme: " + ambient)).Should(b.EvaluateTo(true))
			Ω(prefers("prefers-reduced-motion: reduce")).Should(b.EvaluateTo(true))
		})

		It("is per tab", func() {
			tab := b.NewTab()
			tab.Navigate(fixtureServer + "/dom.html")
			Eventually("#hello").Should(tab.Exist())
			tab.SetMedia(biloba.Media{ColorScheme: other})
			Ω(prefers("prefers-color-scheme: " + other)).Should(tab.EvaluateTo(true))
			Ω(prefers("prefers-color-scheme: " + ambient)).Should(b.EvaluateTo(true))
		})

		It("rejects values Chrome would silently ignore", func() {
			b.SetMedia(biloba.Media{ColorScheme: "midnight"})
			b.SetMedia(biloba.Media{ReducedMotion: "less"})
			b.SetMedia(biloba.Media{Type: "tv"})
			ExpectFailures(
				`SetMedia: Media.ColorScheme must be one of ["light" "dark"], got "midnight"`,
				`SetMedia: Media.ReducedMotion must be one of ["reduce" "no-preference"], got "less"`,
				`SetMedia: Media.Type must be one of ["screen" "print"], got "tv"`,
			)
		})

		It("is a one-shot mutation, so it rejects the poll knobs", func() {
			b.Immediate().SetMedia(biloba.Media{ColorScheme: other})
			ExpectFailures(ContainSubstring("SetMedia does not support Immediate"))
		})
	})
})

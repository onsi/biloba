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

	Describe("SetUserAgent", func() {
		const agent = "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) BilobaTest/1.0"

		Describe("overriding and restoring", Ordered, func() {
			It("overrides navigator.userAgent and the User-Agent header for the documents the tab loads", func() {
				b.SetUserAgent(agent)
				b.Navigate(fixtureServer + "/dom.html?ua")
				Eventually("#hello").Should(b.Exist())
				Ω(`navigator.userAgent`).Should(b.EvaluateTo(agent))
				request := b.AllRequests().Find(b.RequestMatching(fixtureServer + "/dom.html?ua"))
				Ω(request).ShouldNot(BeNil())
				Ω(request.Headers).Should(HaveKeyWithValue("User-Agent", agent))
			})

			It("restores the browser's own user agent for the next spec", func() {
				Ω(`navigator.userAgent`).ShouldNot(b.EvaluateTo(agent))
				Ω(`navigator.userAgent`).Should(b.EvaluateTo(ContainSubstring("Chrome")))
			})
		})

		It("unwinds nested overrides in order", func() {
			b.SetUserAgent("first")
			DeferCleanup(func() {
				b.Navigate(fixtureServer + "/dom.html")
				Ω(`navigator.userAgent`).Should(b.EvaluateTo("first"), "the second override's cleanup restores the first")
			})
			b.SetUserAgent("second")
			b.Navigate(fixtureServer + "/dom.html")
			Ω(`navigator.userAgent`).Should(b.EvaluateTo("second"))
		})
	})
})

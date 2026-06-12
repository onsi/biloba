package biloba_test

import (
	"time"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cookies", func() {
	BeforeEach(func() {
		//cookies require a navigated origin - about:blank cannot hold cookies
		b.Navigate(fixtureServer + "/storage.html")
		Eventually("#title").Should(b.Exist())
		DeferCleanup(b.ClearCookies)
	})

	Describe("Setting and getting cookies", func() {
		It("round-trips a cookie via GetCookies", func() {
			b.SetCookie(biloba.Cookie{Name: "user", Value: "Joe"})
			cookies := b.GetCookies()
			Ω(cookies).Should(HaveLen(1))
			Ω(cookies[0].Name).Should(Equal("user"))
			Ω(cookies[0].Value).Should(Equal("Joe"))
			Ω(cookies[0].Session).Should(BeTrue())
		})

		It("makes the cookie visible to the page", func() {
			b.SetCookie(biloba.Cookie{Name: "user", Value: "Joe"})
			b.Run("window.refresh()")
			Ω(b.GetProperty("#cookie", "innerText")).Should(ContainSubstring("user=Joe"))
		})

		It("can set multiple cookies at once", func() {
			b.SetCookie(
				biloba.Cookie{Name: "user", Value: "Joe"},
				biloba.Cookie{Name: "role", Value: "admin"},
			)
			cookies := b.GetCookies()
			Ω(cookies).Should(HaveLen(2))
			names := []string{cookies[0].Name, cookies[1].Name}
			Ω(names).Should(ConsistOf("user", "role"))
		})

		It("can set a persistent cookie with an expiration", func() {
			expiry := time.Now().Add(180 * 24 * time.Hour)
			b.SetCookie(biloba.Cookie{Name: "user", Value: "Joe", Expires: expiry})
			cookies := b.GetCookies()
			Ω(cookies).Should(HaveLen(1))
			Ω(cookies[0].Session).Should(BeFalse())
			Ω(cookies[0].Expires).Should(BeTemporally("~", expiry, time.Minute))
		})
	})

	Describe("ClearCookies", func() {
		It("clears all cookies in the browser context", func() {
			b.SetCookie(biloba.Cookie{Name: "user", Value: "Joe"})
			Ω(b.GetCookies()).Should(HaveLen(1))
			b.ClearCookies()
			Ω(b.GetCookies()).Should(BeEmpty())
		})
	})

	Describe("when setting a cookie fails", func() {
		It("fails the spec", func() {
			//a cookie cannot be associated with about:blank's opaque origin
			b.Navigate("about:blank")
			b.SetCookie(biloba.Cookie{Name: "user", Value: "Joe"})
			ExpectFailures(ContainSubstring("Failed to set cookies"))
		})
	})

	Describe("isolation across tabs", func() {
		It("does not leak cookies between isolated tabs", func() {
			b.SetCookie(biloba.Cookie{Name: "user", Value: "Joe"})

			tab := b.NewTab()
			tab.Navigate(fixtureServer + "/storage.html")
			Eventually("#title").Should(tab.Exist())
			Ω(tab.GetCookies()).Should(BeEmpty())
		})
	})
})

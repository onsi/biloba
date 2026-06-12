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

	Describe("the HaveCookie matcher", func() {
		It("passes when a cookie with the name exists (literal and matcher args)", func() {
			b.SetCookie(biloba.Cookie{Name: "session", Value: "abc123"})
			Eventually(b).Should(b.HaveCookie("session"))
			Ω(b).Should(b.HaveCookie(ContainSubstring("sess")))
			Ω(b).ShouldNot(b.HaveCookie("nope"))
		})

		It("refines on the cookie's fields with literal and matcher args", func() {
			b.SetCookie(biloba.Cookie{Name: "session", Value: "abc123", Domain: "localhost", Path: "/"})
			Ω(b).Should(b.HaveCookie("session").WithValue("abc123").WithPath("/"))
			Ω(b).Should(b.HaveCookie("session").WithValue(ContainSubstring("abc")))
			Ω(b).ShouldNot(b.HaveCookie("session").WithValue("nope"))
		})

		It("supports the WithSecure and WithHTTPOnly flag refinements", func() {
			b.SetCookie(biloba.Cookie{Name: "session", Value: "abc123", Secure: true, HTTPOnly: true})
			Ω(b).Should(b.HaveCookie("session").WithSecure().WithHTTPOnly())
			Ω(b).Should(b.HaveCookie("session").WithSecure(true).WithHTTPOnly(true))

			b.SetCookie(biloba.Cookie{Name: "plain", Value: "v"})
			Ω(b).ShouldNot(b.HaveCookie("plain").WithSecure())
			// the variadic form can assert the negative explicitly
			Ω(b).Should(b.HaveCookie("plain").WithSecure(false).WithHTTPOnly(false))
			Ω(b).ShouldNot(b.HaveCookie("session").WithSecure(false))
		})

		It("supports WithDomain and WithSameSite", func() {
			b.SetCookie(biloba.Cookie{Name: "session", Value: "abc123", Domain: "localhost", SameSite: "Lax"})
			Ω(b).Should(b.HaveCookie("session").WithDomain("localhost").WithSameSite("Lax"))
		})

		It("requires all refinements to hold for the SAME cookie", func() {
			//two cookies, each satisfies one refinement but neither satisfies both
			b.SetCookie(
				biloba.Cookie{Name: "session", Value: "abc123", Domain: "localhost", Path: "/foo"},
				biloba.Cookie{Name: "session", Value: "different", Domain: "localhost", Path: "/bar"},
			)
			//each refinement is individually satisfiable...
			Ω(b).Should(b.HaveCookie("session").WithValue("abc123"))
			Ω(b).Should(b.HaveCookie("session").WithPath("/bar"))
			//...but no single cookie satisfies both
			Ω(b).ShouldNot(b.HaveCookie("session").WithValue("abc123").WithPath("/bar"))
		})

		It("produces a debuggable failure message", func() {
			b.SetCookie(biloba.Cookie{Name: "session", Value: "abc123"})
			matcher := b.HaveCookie("session").WithValue("nope")
			match, err := matcher.Match(b)
			Ω(match).Should(BeFalse())
			Ω(err).ShouldNot(HaveOccurred())
			msg := matcher.FailureMessage(b)
			Ω(msg).Should(ContainSubstring("have a cookie with Name matching"))
			Ω(msg).Should(ContainSubstring("did not satisfy the refinements"))
			Ω(msg).Should(ContainSubstring("abc123"))
		})

		It("errors when not passed a tab", func() {
			match, err := b.HaveCookie("session").Match("not-a-tab")
			Ω(match).Should(BeFalse())
			Ω(err).Should(MatchError(ContainSubstring("HaveCookie must be passed a Biloba tab")))
		})
	})

	Describe("the HaveNumCookies matcher", func() {
		It("matches the cookie count with literal and matcher args", func() {
			Ω(b).Should(b.HaveNumCookies(0))
			b.SetCookie(
				biloba.Cookie{Name: "a", Value: "1"},
				biloba.Cookie{Name: "b", Value: "2"},
			)
			Eventually(b).Should(b.HaveNumCookies(2))
			Ω(b).Should(b.HaveNumCookies(BeNumerically(">", 0)))
			Ω(b).ShouldNot(b.HaveNumCookies(5))
		})
	})
})

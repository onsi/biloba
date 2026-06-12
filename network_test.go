package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Observing the network", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/network.html")
		Eventually("#hello").Should(b.Exist())
	})

	Describe("HaveMadeRequest", func() {
		It("records requests the page makes and lets you assert on them", func() {
			b.Click("#fetch-users")
			Eventually(b).Should(b.HaveMadeRequest(b.RequestWithURL(ContainSubstring("/api/users"))))
			Eventually("#result").Should(b.HaveInnerText(ContainSubstring("/api/users")))
		})

		It("can match on method as well as URL (all filters must match)", func() {
			b.Click("#post-user")
			Eventually(b).Should(b.HaveMadeRequest(
				b.RequestWithURL(ContainSubstring("/api/users")),
				b.RequestWithMethod("POST"),
			))
		})

		It("does not match requests that were never made", func() {
			b.Click("#fetch-users")
			Eventually(b).Should(b.HaveMadeRequest(b.RequestWithURL(ContainSubstring("/api/users"))))
			Expect(b).NotTo(b.HaveMadeRequest(b.RequestWithURL(ContainSubstring("/api/widgets"))))
			Expect(b).NotTo(b.HaveMadeRequest(
				b.RequestWithURL(ContainSubstring("/api/users")),
				b.RequestWithMethod("DELETE"),
			))
		})

		It("exposes the raw request records via AllRequests", func() {
			b.Click("#fetch-users")
			Eventually(b).Should(b.HaveMadeRequest(b.RequestWithURL(ContainSubstring("/api/users"))))
			req := b.AllRequests().Find(b.RequestWithURL(ContainSubstring("/api/users")))
			Expect(req).NotTo(BeNil())
			Expect(req.Method).To(Equal("GET"))
		})
	})

	Describe("BeNetworkIdle", func() {
		It("eventually becomes idle once in-flight requests complete", func() {
			b.Click("#fetch-slow")
			Eventually(b).Should(b.HaveMadeRequest(b.RequestWithURL(ContainSubstring("/api/slow"))))
			// the slow endpoint sleeps ~300ms, so the request is briefly in-flight then settles
			Eventually(b).Should(b.BeNetworkIdle())
			Eventually("#result").Should(b.HaveInnerText("slow done"))
		})
	})
})

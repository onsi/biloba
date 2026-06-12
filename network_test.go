package biloba_test

import (
	"github.com/onsi/biloba"
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
			Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/users")))
			Eventually("#result").Should(b.HaveInnerText(ContainSubstring("/api/users")))
		})

		It("can refine on method as well as URL", func() {
			b.Click("#post-user")
			Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/users")).WithMethod("POST"))
		})

		It("does not match requests that were never made", func() {
			b.Click("#fetch-users")
			Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/users")))
			Expect(b).NotTo(b.HaveMadeRequest(ContainSubstring("/api/widgets")))
			Expect(b).NotTo(b.HaveMadeRequest(ContainSubstring("/api/users")).WithMethod("DELETE"))
		})

		It("doubles as a predicate over AllRequests via Find/Filter", func() {
			b.Click("#fetch-users")
			Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/users")))

			// the same query shape used as a matcher above reads as a predicate here (RequestMatching)
			req := b.AllRequests().Find(b.RequestMatching(ContainSubstring("/api/users")).WithMethod("GET"))
			Expect(req).NotTo(BeNil())
			Expect(req.Method).To(Equal("GET"))

			Expect(b.AllRequests().Filter(b.RequestMatching(ContainSubstring("/api/users")))).NotTo(BeEmpty())
			Expect(b.AllRequests().Find(b.RequestMatching(ContainSubstring("/api/widgets")))).To(BeNil())
		})
	})

	Describe("StubRequest", func() {
		It("fulfills matching requests with the stubbed response instead of hitting the network", func() {
			b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{
				Body:    `{"stubbed": true}`,
				Headers: map[string]string{"Content-Type": "application/json"},
			})
			b.Click("#fetch-users")
			Eventually("#result").Should(b.HaveInnerText(ContainSubstring("stubbed")))
			// the real /api/users handler echoes the path, so a passthrough would have shown it
			Expect("#result").NotTo(b.HaveInnerText(ContainSubstring("/api/users")))
		})

		It("honors the configured status code", func() {
			b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{
				Status: 503,
				Body:   "service unavailable",
			})
			b.Run(`window.stubStatus = null`)
			b.Run(`fetch("/api/users").then(r => window.stubStatus = r.status)`)
			Eventually("window.stubStatus").Should(b.EvaluateTo(503.0))
		})

		It("passes through requests that match no stub", func() {
			b.StubRequest(ContainSubstring("/api/widgets"), biloba.StubResponse{Body: "nope"})
			b.Click("#fetch-users")
			// /api/users matches no stub, so it reaches the real echoing backend
			Eventually("#result").Should(b.HaveInnerText(ContainSubstring("/api/users")))
		})

		It("still records stubbed requests so they can be observed", func() {
			b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: "{}"})
			b.Click("#fetch-users")
			Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/users")))
		})
	})

	Describe("BeNetworkIdle", func() {
		It("eventually becomes idle once in-flight requests complete", func() {
			b.Click("#fetch-slow")
			Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/slow")))
			// the slow endpoint sleeps ~300ms, so the request is briefly in-flight then settles
			Eventually(b).Should(b.BeNetworkIdle())
			Eventually("#result").Should(b.HaveInnerText("slow done"))
		})
	})
})

package biloba_test

import (
	"strings"

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

	Describe("AbortRequest", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/network-interception.html")
			Eventually("#hello").Should(b.Exist())
		})

		It("makes matching requests fail, so the page sees a rejected fetch", func() {
			b.AbortRequest(ContainSubstring("/api/echo"))
			b.Click("#fetch")
			Eventually("#error").Should(b.HaveInnerText(ContainSubstring("fetch failed")))
			Expect("#status").To(b.HaveInnerText(""))
		})

		It("only aborts matching requests; others pass through", func() {
			b.AbortRequest(ContainSubstring("/api/widgets"))
			b.Click("#fetch")
			// /api/echo matches no abort, so it reaches the real echoing backend
			Eventually("#status").Should(b.HaveInnerText("200"))
			Expect("#error").To(b.HaveInnerText(""))
		})
	})

	Describe("ModifyRequest", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/network-interception.html")
			Eventually("#hello").Should(b.Exist())
		})

		It("rewrites the URL, method, and body before the request goes out", func() {
			b.ModifyRequest(ContainSubstring("/api/echo")).
				WithURL(fixtureServer + "/api/rewritten").
				WithMethod("POST").
				WithBody(`{"name":"Jane"}`)
			b.Click("#fetch")
			// the backend echoes path/method/body, so we can observe every override landed
			Eventually("#body").Should(b.HaveInnerText(ContainSubstring(`"path":"/api/rewritten"`)))
			Expect("#body").To(b.HaveInnerText(ContainSubstring(`"method":"POST"`)))
			Expect("#body").To(b.HaveInnerText(ContainSubstring(`{\"name\":\"Jane\"}`)))
		})

		It("can add a header without disturbing the rest of the request", func() {
			b.ModifyRequest(ContainSubstring("/api/echo")).WithHeader("X-Test", "true")
			b.Click("#fetch")
			Eventually("#status").Should(b.HaveInnerText("200"))
			Expect("#body").To(b.HaveInnerText(ContainSubstring(`"path":"/api/echo"`)))
		})
	})

	Describe("ModifyResponse", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/network-interception.html")
			Eventually("#hello").Should(b.Exist())
		})

		It("overrides the status and body of a real response", func() {
			b.ModifyResponse(ContainSubstring("/api/echo")).
				WithStatus(503).
				WithBody("service unavailable")
			b.Click("#fetch")
			Eventually("#status").Should(b.HaveInnerText("503"))
			Expect("#body").To(b.HaveInnerText("service unavailable"))
		})

		It("can read the real response and transform it via Using", func() {
			b.ModifyResponse(ContainSubstring("/api/echo")).Using(func(r biloba.InterceptedResponse) biloba.StubResponse {
				return biloba.StubResponse{
					Status:  r.Status,
					Body:    strings.ToUpper(r.Body),
					Headers: r.Headers,
				}
			})
			b.Click("#fetch")
			Eventually("#status").Should(b.HaveInnerText("200"))
			// the real echoed body contains the lowercase path; the transform upcases it
			Expect("#body").To(b.HaveInnerText(ContainSubstring("/API/ECHO")))
		})

		It("leaves non-matching responses untouched", func() {
			b.ModifyResponse(ContainSubstring("/api/widgets")).WithStatus(503)
			b.Click("#fetch")
			Eventually("#status").Should(b.HaveInnerText("200"))
			Expect("#body").To(b.HaveInnerText(ContainSubstring("/api/echo")))
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

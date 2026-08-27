package biloba_test

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	ginkgotypes "github.com/onsi/ginkgo/v2/types"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
)

// lineAbove returns the "file:line" of the line immediately above the call - i.e. the registration
// call it follows.  The shadowed-handler note is only worth anything if it points at the *user's*
// spec line rather than somewhere inside Biloba, so the specs pin the exact expected location
// instead of settling for "mentions network_test.go".
func lineAbove() string {
	cl := ginkgotypes.NewCodeLocation(1)
	cl.LineNumber--
	return cl.String()
}

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

		It("counts how many requests it has fulfilled, and stays at 0 when its URL never matches", func() {
			stub := b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{}`})
			miss := b.StubRequest(ContainSubstring("/api/widgets"), biloba.StubResponse{Body: `{}`})
			Expect(stub.Count()).To(Equal(0))

			b.Click("#fetch-users")
			Eventually(stub.Count).Should(Equal(1))

			b.Click("#fetch-users")
			Eventually(stub.Count).Should(Equal(2))

			Expect(miss.Count()).To(Equal(0))
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

		It("counts how many requests it has aborted, and stays at 0 when its URL never matches", func() {
			abort := b.AbortRequest(ContainSubstring("/api/echo"))
			miss := b.AbortRequest(ContainSubstring("/api/widgets"))
			Expect(abort.Count()).To(Equal(0))

			b.Click("#fetch")
			Eventually("#error").Should(b.HaveInnerText(ContainSubstring("fetch failed")))
			Eventually(abort.Count).Should(Equal(1))

			b.Click("#fetch")
			Eventually(abort.Count).Should(Equal(2))

			Expect(miss.Count()).To(Equal(0))
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
			// #body is written after an awaited response.text(), so it is the one to gate on
			Eventually("#body").Should(b.HaveInnerText(ContainSubstring(`"path":"/api/echo"`)))
			Expect("#status").To(b.HaveInnerText("200"))
		})

		It("counts how many requests it has modified, and stays at 0 when its URL never matches", func() {
			mod := b.ModifyRequest(ContainSubstring("/api/echo")).WithHeader("X-Test", "true")
			miss := b.ModifyRequest(ContainSubstring("/api/widgets")).WithHeader("X-Test", "true")
			Expect(mod.Count()).To(Equal(0))

			b.Click("#fetch")
			Eventually("#body").Should(b.HaveInnerText(ContainSubstring(`"path":"/api/echo"`)))
			Eventually(mod.Count).Should(Equal(1))

			Expect(miss.Count()).To(Equal(0))
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
			Eventually("#body").Should(b.HaveInnerText("service unavailable"))
			Expect("#status").To(b.HaveInnerText("503"))
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
			// the real echoed body contains the lowercase path; the transform upcases it
			Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/API/ECHO")))
			Expect("#status").To(b.HaveInnerText("200"))
		})

		It("leaves non-matching responses untouched", func() {
			b.ModifyResponse(ContainSubstring("/api/widgets")).WithStatus(503)
			b.Click("#fetch")
			Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo")))
			Expect("#status").To(b.HaveInnerText("200"))
		})

		It("counts how many responses it has modified, and stays at 0 when its URL never matches", func() {
			mod := b.ModifyResponse(ContainSubstring("/api/echo")).WithStatus(503)
			miss := b.ModifyResponse(ContainSubstring("/api/widgets")).WithStatus(503)
			Expect(mod.Count()).To(Equal(0))

			b.Click("#fetch")
			Eventually("#status").Should(b.HaveInnerText("503"))
			Eventually(mod.Count).Should(Equal(1))

			Expect(miss.Count()).To(Equal(0))
		})

		// Handlers are first-match-wins, and Prepare() is OncePerOrdered - so inside an Ordered
		// container a handler registered by an earlier It is still registered and shadows an
		// identical handler registered by a later It.  This pins that (surprising) behavior.
		Describe("first-match-wins across an Ordered container", Ordered, func() {
			It("registers a handler in the first It", func() {
				b.ModifyResponse(ContainSubstring("/api/echo")).WithBody("from the first It")
				b.Click("#fetch")
				Eventually("#body").Should(b.HaveInnerText("from the first It"))
			})

			It("silently shadows an identical handler registered in the second It", func() {
				b.ModifyResponse(ContainSubstring("/api/echo")).WithBody("from the second It")
				b.Navigate(fixtureServer + "/network-interception.html")
				Eventually("#hello").Should(b.Exist())
				b.Click("#fetch")
				// the first It's handler still wins - the second It's never runs
				Eventually("#body").Should(b.HaveInnerText("from the first It"))
			})
		})
	})

	// A handler that is never consulted fails DOWNSTREAM - at whatever was waiting on it - and points
	// nowhere near the registration that lost.  Biloba notes every handler that also matched a
	// dispatch someone else claimed, and surfaces the ones that never ran at all when a spec fails.
	Describe("the shadowed-handler diagnostic", func() {
		Describe("response-stage handlers shadowed across an Ordered container", Ordered, func() {
			var firstLoc, secondLoc string

			BeforeEach(func() {
				b.Navigate(fixtureServer + "/network-interception.html")
				Eventually("#hello").Should(b.Exist())
			})

			It("stays silent for the handler registered by the first It", func() {
				b.ModifyResponse(ContainSubstring("/api/echo")).WithBody("from the first It")
				firstLoc = lineAbove()
				b.Click("#fetch")
				Eventually("#body").Should(b.HaveInnerText("from the first It"))
				Expect(b.ShadowedHandlersNoteForTest()).To(BeEmpty())
			})

			It("flags the identical handler registered by the second It, naming both registration sites", func() {
				b.ModifyResponse(ContainSubstring("/api/echo")).WithBody("from the second It")
				secondLoc = lineAbove()
				b.Click("#fetch")
				Eventually("#body").Should(b.HaveInnerText("from the first It"))

				Expect(b.ShadowedHandlersNoteForTest()).To(SatisfyAll(
					ContainSubstring("⚠ A ModifyResponse handler registered at "+secondLoc+" never ran — an earlier ModifyResponse handler"),
					MatchRegexp(`\(registered at `+regexp.QuoteMeta(firstLoc)+`\) claimed \d+ matching response\(s\) first\.`),
					ContainSubstring("first-match-wins in registration order"),
					ContainSubstring("OncePerOrdered"),
				))
				// and the handler that DID run is not itself flagged
				Expect(strings.Count(b.ShadowedHandlersNoteForTest(), "⚠")).To(Equal(1))
			})
		})

		Describe("request-stage handlers shadowed across an Ordered container", Ordered, func() {
			var firstLoc, secondLoc string

			It("stays silent for the stub registered by the first It", func() {
				b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{"from":"the first It"}`})
				firstLoc = lineAbove()
				b.Click("#fetch-users")
				Eventually("#result").Should(b.HaveInnerText(ContainSubstring("the first It")))
				Expect(b.ShadowedHandlersNoteForTest()).To(BeEmpty())
			})

			It("flags the identical stub registered by the second It", func() {
				b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{"from":"the second It"}`})
				secondLoc = lineAbove()
				b.Click("#fetch-users")
				Eventually("#result").Should(b.HaveInnerText(ContainSubstring("the first It")))

				Expect(b.ShadowedHandlersNoteForTest()).To(SatisfyAll(
					ContainSubstring("⚠ A StubRequest handler registered at "+secondLoc+" never ran — an earlier StubRequest handler"),
					MatchRegexp(`\(registered at `+regexp.QuoteMeta(firstLoc)+`\) claimed \d+ matching request\(s\) first\.`),
				))
			})
		})

		// The same silent-dead-code situation, arrived at within a single spec.  It is still reported:
		// the note names both sites, so a deliberate ordering reads as instantly self-explanatory.
		It("flags a handler registered after a broader catch-all in the same spec", func() {
			b.StubRequest(ContainSubstring("/api/"), biloba.StubResponse{Body: `{"who":"catch-all"}`})
			catchAllLoc := lineAbove()
			b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{"who":"specific"}`})
			specificLoc := lineAbove()

			b.Click("#fetch-users")
			Eventually("#result").Should(b.HaveInnerText(ContainSubstring("catch-all")))

			Expect(b.ShadowedHandlersNoteForTest()).To(SatisfyAll(
				ContainSubstring("⚠ A StubRequest handler registered at "+specificLoc+" never ran"),
				ContainSubstring("(registered at "+catchAllLoc+")"),
			))
		})

		// Count is a fact about the handler that actually claimed the dispatch, not about the URL: a
		// handler that only ever loses to an earlier one never fires, no matter how many matching
		// requests go by.
		It("keeps a shadowed handler's Count at 0 while the handler that claims the dispatch climbs", func() {
			catchAll := b.StubRequest(ContainSubstring("/api/"), biloba.StubResponse{Body: `{"who":"catch-all"}`})
			specific := b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{"who":"specific"}`})

			b.Click("#fetch-users")
			Eventually("#result").Should(b.HaveInnerText(ContainSubstring("catch-all")))

			Eventually(catchAll.Count).Should(Equal(1))
			Expect(specific.Count()).To(Equal(0))
		})

		It("names the API each handler was registered with", func() {
			b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{}`})
			b.AbortRequest(ContainSubstring("/api/users"))
			abortLoc := lineAbove()

			b.Click("#fetch-users")
			// the stub wins, so the page sees the stubbed body rather than a failed fetch
			Eventually("#result").Should(b.HaveInnerText("{}"))

			Expect(b.ShadowedHandlersNoteForTest()).To(ContainSubstring("⚠ An AbortRequest handler registered at " + abortLoc + " never ran — an earlier StubRequest handler"))
		})

		It("says nothing about a handler whose URL was simply never requested", func() {
			b.StubRequest(ContainSubstring("/api/widgets"), biloba.StubResponse{Body: "nope"})
			b.Click("#fetch-users")
			Eventually("#result").Should(b.HaveInnerText(ContainSubstring("/api/users")))
			Expect(b.ShadowedHandlersNoteForTest()).To(BeEmpty())
		})

		It("says nothing when the only handler won every dispatch", func() {
			b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{"stubbed":true}`})
			b.Click("#fetch-users")
			Eventually("#result").Should(b.HaveInnerText(ContainSubstring("stubbed")))
			Expect(b.ShadowedHandlersNoteForTest()).To(BeEmpty())
		})

		// Shadowed-at-least-once is not enough: a catch-all that loses one URL but wins another is
		// doing exactly what it was written to do.  Only never-ran-at-all is the smoking gun.
		It("says nothing about a shadowed handler that won a dispatch of its own", func() {
			b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{"who":"specific"}`})
			b.StubRequest(ContainSubstring("/api/"), biloba.StubResponse{Body: `{"who":"catch-all"}`})

			b.Click("#fetch-users") // the specific stub wins; the catch-all is shadowed here
			Eventually("#result").Should(b.HaveInnerText(ContainSubstring("specific")))
			Expect(b.ShadowedHandlersNoteForTest()).To(ContainSubstring("never ran"))

			b.Click("#fetch-slow") // ...but the catch-all claims this one, so it is not dead code
			Eventually("#result").Should(b.HaveInnerText("slow done"))
			Expect(b.ShadowedHandlersNoteForTest()).To(BeEmpty())
		})

		It("says nothing when interception is on but nothing is shadowed", func() {
			b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{"who":"users"}`})
			b.StubRequest(ContainSubstring("/api/slow"), biloba.StubResponse{Body: `{}`})

			// fetchUsers and fetchSlow BOTH write #result, so the two fetches must be serialized:
			// firing them back-to-back is a last-writer-wins race, and a users response that lands
			// after the slow one leaves #result stuck on the users body for the rest of the poll.
			b.Click("#fetch-users")
			Eventually("#result").Should(b.HaveInnerText(ContainSubstring("users")))
			b.Click("#fetch-slow")
			Eventually("#result").Should(b.HaveInnerText("slow done"))

			Expect(b.ShadowedHandlersNoteForTest()).To(BeEmpty())
		})

		It("is scoped to the tab that registered the handlers", func() {
			b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{"who":"first"}`})
			b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{"who":"second"}`})
			b.Click("#fetch-users")
			Eventually("#result").Should(b.HaveInnerText(ContainSubstring("first")))
			Expect(b.ShadowedHandlersNoteForTest()).To(ContainSubstring("never ran"))

			tab := b.NewTab()
			Expect(tab.ShadowedHandlersNoteForTest()).To(BeEmpty())
		})
	})

	// A view of a tab - Realistic(), WithTimeout(), ViewportOnly(), ... - is a shallow COPY of the
	// Biloba struct, and every view's doc promises it is "the same tab".  Handler lists used to be
	// plain slice fields, so registering through a view appended to the copy and the handler was
	// registered nowhere: b.WithTimeout(d).HoldResponse(url) intercepted nothing and Await burned
	// exactly the tuned deadline.  These pin both the write side and the read side.
	Describe("registering through a view of the tab", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/network-interception.html")
			Eventually("#hello").Should(b.Exist())
		})

		// The shape that was reported: WithTimeout is the one poll-config knob HoldResponse accepts,
		// so it is the only view a guardConfig-protected registrar lets through.
		It("holds a response registered through WithTimeout", func() {
			hold := b.WithTimeout(5 * time.Second).HoldResponse(ContainSubstring("/api/echo"))
			b.Click("#fetch")

			response := hold.Await()
			Expect(response.Status).To(Equal(200))
			Expect(hold.Count()).To(Equal(1))
			hold.Release()
			Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo")))
		})

		It("stubs a request registered through an inline Realistic() view", func() {
			b.Realistic().StubRequest(ContainSubstring("/api/echo"), biloba.StubResponse{Body: "from a view"})
			b.Click("#fetch")
			Eventually("#body").Should(b.HaveInnerText("from a view"))
		})

		// The usage that actually occurs in the wild: a view made once and held across several
		// statements, with a registration among them.  Views made for a single interaction and thrown
		// away never reach a registrar, which is why this survived so long unnoticed.
		It("stubs a request registered through a held Realistic() view, and the tab sees it", func() {
			rb := b.Realistic()
			stub := rb.StubRequest(ContainSubstring("/api/echo"), biloba.StubResponse{Body: "held view"})
			rb.Click("#fetch")

			Eventually("#body").Should(b.HaveInnerText("held view"))
			Eventually(stub.Count).Should(Equal(1))
			// and the count is visible from the bare tab as well as the view - one tab, one list
			Expect(b.AllRequests().Find(b.RequestMatching(ContainSubstring("/api/echo")))).NotTo(BeNil())
		})

		It("aborts and modifies through a view too", func() {
			b.Realistic().AbortRequest(ContainSubstring("/api/echo"))
			b.Click("#fetch")
			Eventually("#error").Should(b.HaveInnerText(ContainSubstring("fetch failed")))
		})

		It("modifies a response registered through a view", func() {
			// no WithTimeout here: ModifyResponse rejects every poll knob by design, so Realistic() is
			// the view that reaches it
			b.Realistic().ModifyResponse(ContainSubstring("/api/echo")).WithStatus(503)
			b.Click("#fetch")
			Eventually("#status").Should(b.HaveInnerText("503"))
		})

		// The read side: a view copies the slice header, so a list read through a view used to be
		// frozen at the instant the view was made.
		It("reads the tab's request list through a view made before the requests happened", func() {
			// the page load itself has already made requests, so the assertion is that the view sees
			// the list GROW - a copied slice header would stay frozen at whatever it held here
			rb := b.Realistic()
			before := len(rb.AllRequests())
			b.Click("#fetch")
			Eventually("#status").Should(b.HaveInnerText("200"))
			Eventually(func() int { return len(rb.AllRequests()) }).Should(BeNumerically(">", before))
		})
	})

	Describe("HoldResponse", func() {
		BeforeEach(func() {
			b.Navigate(fixtureServer + "/network-interception.html")
			Eventually("#hello").Should(b.Exist())
		})

		It("holds a matching response until it is released, then passes it through unchanged", func() {
			hold := b.HoldResponse(ContainSubstring("/api/echo"))
			b.Click("#fetch")

			response := hold.Await()
			Expect(response.Status).To(Equal(200))
			Expect(response.Body).To(ContainSubstring("/api/echo"))
			// the page is still waiting on the held response
			Consistently("#status", 100*time.Millisecond).Should(b.HaveInnerText(""))

			hold.Release()
			// the fixture awaits response.text() BETWEEN writing #status and writing #body, so #status
			// landing does not imply #body has - gate on #body (the last write) rather than reading it
			// once off the back of the #status gate.
			Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo")))
			Expect("#status").To(b.HaveInnerText("200"))
			Expect(hold.Count()).To(Equal(1))
		})

		It("counts matching responses and can be polled", func() {
			hold := b.HoldResponse(ContainSubstring("/api/echo"))
			Expect(hold.Count()).To(Equal(0))
			b.Click("#fetch")
			Eventually(hold.Count).Should(Equal(1))
			hold.Release()
			Eventually("#status").Should(b.HaveInnerText("200"))
		})

		It("stops holding future matches once released, and is idempotent", func() {
			hold := b.HoldResponse(ContainSubstring("/api/echo"))
			b.Click("#fetch")
			hold.Await()
			hold.Release()
			hold.Release() // idempotent
			Eventually("#status").Should(b.HaveInnerText("200"))

			// the one response this hold ever saw was frozen, so it counts against Held, not PassedThrough
			Expect(hold.Held()).To(Equal(1))
			Expect(hold.PassedThrough()).To(Equal(0))

			// subsequent matches pass straight through - no hold, no await
			b.Click("#fetch")
			Eventually(hold.Count).Should(Equal(2))
			Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo")))
			Expect("#status").To(b.HaveInnerText("200"))

			// ...and that one is recorded on the passed-through side, not held again
			Expect(hold.Held()).To(Equal(1))
			Eventually(hold.PassedThrough).Should(Equal(1))
		})

		It("is safe to release when nothing is being held", func() {
			hold := b.HoldResponse(ContainSubstring("/api/echo"))
			hold.Release()
			Expect(hold.Count()).To(Equal(0))
			b.Click("#fetch")
			Eventually("#status").Should(b.HaveInnerText("200"))
		})

		Describe("holding several responses", func() {
			// The fixture funnels every response into the same #status/#body, so once two responses are
			// in flight the DOM alone cannot say which of them landed when - and landing ORDER is the
			// entire point of these specs.  fetchPath drives the fixture's doFetch (so the page still
			// visibly reacts to each response) and records the path in window.__landed at the moment
			// that response actually lands.
			fetchPath := func(path string) {
				GinkgoHelper()
				b.Run(fmt.Sprintf(`void doFetch(%q).then(() => window.__landed.push(%q))`, path, path))
			}

			BeforeEach(func() {
				b.Run(`window.__landed = []`)
			})

			It("holds every matching response by default, and a bare Release frees them all at once", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo"))

				fetchPath("/api/echo/one")
				hold.Await()
				fetchPath("/api/echo/two")
				Eventually(hold.Count).Should(Equal(2))

				// the default hold is all-or-nothing: the SECOND response is frozen too
				Consistently("#status", 100*time.Millisecond).Should(b.HaveInnerText(""))
				Expect(`window.__landed.join(",")`).To(b.EvaluateTo(""))

				// a default (unlimited) hold freezes everything it intercepts: Held equals Count, and
				// nothing has ever passed straight through
				Expect(hold.Held()).To(Equal(hold.Count()))
				Expect(hold.PassedThrough()).To(Equal(0))

				hold.Release()
				Eventually(`window.__landed.slice().sort().join(",")`).Should(b.EvaluateTo("/api/echo/one,/api/echo/two"))
			})

			// This is the headline behavior Limit exists for: PassedThrough lets a spec state "#2 was
			// NOT frozen" directly, instead of inferring it from Count plus the limit.
			It("splits Count into Held and PassedThrough under Limit(1)", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo")).Limit(1)

				fetchPath("/api/echo/one")
				hold.Await() // #1 is frozen
				fetchPath("/api/echo/two")
				Eventually(hold.PassedThrough).Should(Equal(1)) // #2 flew past

				Expect(hold.Held()).To(Equal(1))
				Expect(hold.Count()).To(Equal(2))
				Expect(hold.Held() + hold.PassedThrough()).To(Equal(hold.Count()))

				hold.Release()
				Eventually(`window.__landed.slice().sort().join(",")`).Should(b.EvaluateTo("/api/echo/one,/api/echo/two"))
			})

			It("holds at most Limit(n) responses at a time - the rest fly past while the held one stays frozen", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo")).Limit(1)

				fetchPath("/api/echo/one")
				first := hold.Await()
				Expect(first.Body).To(ContainSubstring("/api/echo/one"))

				// the hold is at capacity, so this one is never held: it lands while the first is frozen
				fetchPath("/api/echo/two")
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/two")))
				Expect(`window.__landed.join(",")`).To(b.EvaluateTo("/api/echo/two"))

				// every match still counts, but Await keeps returning the one actually being held
				Expect(hold.Count()).To(Equal(2))
				Expect(hold.Await()).To(Equal(first))
				Consistently("#body", 100*time.Millisecond).Should(b.HaveInnerText(ContainSubstring("/api/echo/two")))

				// ...and now the FIRST response lands LAST - the ordering a default hold cannot produce
				hold.Release()
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/one")))
				Expect(`window.__landed.join(",")`).To(b.EvaluateTo("/api/echo/two,/api/echo/one"))
			})

			It("frees up capacity as held responses are released", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo")).Limit(1)

				fetchPath("/api/echo/one")
				hold.Await()
				Expect(hold.Held()).To(Equal(1))
				hold.ReleaseNext()
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/one")))

				// the hold is back under its limit, so the next match is held again rather than let by
				fetchPath("/api/echo/two")
				Expect(hold.Await().Body).To(ContainSubstring("/api/echo/two"))
				Consistently("#body", 100*time.Millisecond).Should(b.HaveInnerText(""))

				// released capacity means the second match was held too - it increments Held, not
				// PassedThrough, even though the first one already came and went
				Expect(hold.Held()).To(Equal(2))
				Expect(hold.PassedThrough()).To(Equal(0))

				hold.ReleaseNext()
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/two")))
				Expect(`window.__landed.join(",")`).To(b.EvaluateTo("/api/echo/one,/api/echo/two"))
			})

			It("releases just the response it is passed, and stays armed", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo"))

				fetchPath("/api/echo/one")
				first := hold.Await()
				fetchPath("/api/echo/two")
				Eventually(hold.Count).Should(Equal(2))

				hold.Release(first)
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/one")))
				// the second response is untouched by that release - still frozen
				Consistently("#body", 100*time.Millisecond).Should(b.HaveInnerText(ContainSubstring("/api/echo/one")))

				second := hold.Await()
				Expect(second.Body).To(ContainSubstring("/api/echo/two"))
				hold.Release(second)
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/two")))

				// and the hold is still armed - a targeted Release is not terminal
				fetchPath("/api/echo/three")
				Expect(hold.Await().Body).To(ContainSubstring("/api/echo/three"))
				Consistently("#body", 100*time.Millisecond).Should(b.HaveInnerText(""))
				hold.Release()
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/three")))
			})

			It("releases the oldest held response with ReleaseNext", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo"))

				fetchPath("/api/echo/one")
				hold.Await()
				fetchPath("/api/echo/two")
				Eventually(hold.Count).Should(Equal(2))

				hold.ReleaseNext()
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/one")))
				// Await now returns the oldest response still being held
				Expect(hold.Await().Body).To(ContainSubstring("/api/echo/two"))

				hold.ReleaseNext()
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/two")))
				Expect(`window.__landed.join(",")`).To(b.EvaluateTo("/api/echo/one,/api/echo/two"))
			})

			It("can hold, release, and then hold the NEXT response for the same URL", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo"))

				fetchPath("/api/echo/one")
				Expect(hold.Await().Body).To(ContainSubstring("/api/echo/one"))
				hold.ReleaseNext()
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/one")))

				// the same hold re-arms - Await blocks until the NEXT response is being held
				fetchPath("/api/echo/two")
				second := hold.Await()
				Expect(second.Body).To(ContainSubstring("/api/echo/two"))
				Consistently("#body", 100*time.Millisecond).Should(b.HaveInnerText(""))

				hold.Release(second)
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/two")))
				Expect(hold.Count()).To(Equal(2))
			})

			It("fails when asked to release a response it is not holding", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo"))
				fetchPath("/api/echo/one")
				first := hold.Await()

				hold.Release(biloba.InterceptedResponse{Status: 200, Body: "never happened", Headers: map[string]string{}})
				ExpectFailures(SatisfyAll(
					ContainSubstring("Release() was passed a response that this hold has never held"),
					ContainSubstring("never happened"),
					ContainSubstring("only accepts responses returned by Await()"),
				))

				hold.Release(first)
				Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/one")))

				hold.Release(first)
				ExpectFailures(ContainSubstring("Release() was passed a response that this hold has already released"))
			})

			It("fails when ReleaseNext is called with nothing held", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo"))
				hold.ReleaseNext()
				ExpectFailures(SatisfyAll(
					ContainSubstring("ReleaseNext() was called but this hold is not holding any responses with URL matching"),
					ContainSubstring("0 matching response(s) have been intercepted so far."),
					ContainSubstring("Await() until a response is actually being held"),
				))
			})

			It("renders the plain tally with no parenthetical when nothing has passed through", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo"))

				fetchPath("/api/echo/one")
				first := hold.Await()
				hold.Release(first) // targeted release, not terminal - still 0 passed through

				hold.ReleaseNext()
				ExpectFailures(SatisfyAll(
					ContainSubstring("ReleaseNext() was called but this hold is not holding any responses with URL matching"),
					ContainSubstring("1 matching response(s) have been intercepted so far."),
					ContainSubstring("Await() until a response is actually being held"),
				))

				Expect(hold.Held()).To(Equal(1))
				Expect(hold.PassedThrough()).To(Equal(0))
			})

			It("appends the held/passed-through breakdown to the tally once something has passed through", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo"))

				fetchPath("/api/echo/one")
				hold.Await()
				hold.Release() // terminal: releases the one held response and stops holding future matches

				fetchPath("/api/echo/two")
				Eventually(hold.PassedThrough).Should(Equal(1))

				hold.ReleaseNext()
				ExpectFailures(SatisfyAll(
					ContainSubstring("ReleaseNext() was called but this hold is not holding any responses with URL matching"),
					ContainSubstring("2 matching response(s) have been intercepted so far (1 held, 1 passed straight through)."),
					ContainSubstring("Await() until a response is actually being held"),
				))
			})

			It("rejects a Limit below 1", func() {
				b.HoldResponse(ContainSubstring("/api/echo")).Limit(0)
				ExpectFailures(SatisfyAll(
					ContainSubstring("Limit(0) is invalid"),
					ContainSubstring("hold at least one response"),
				))
			})

			Describe("deadlock safety with per-response releases", Ordered, func() {
				It("can leave one response individually released and another still held", func() {
					hold := b.HoldResponse(ContainSubstring("/api/echo"))
					fetchPath("/api/echo/one")
					first := hold.Await()
					fetchPath("/api/echo/two")
					Eventually(hold.Count).Should(Equal(2))

					hold.Release(first)
					Eventually("#body").Should(b.HaveInnerText(ContainSubstring("/api/echo/one")))
					// the second is deliberately never released: forceRelease must free it without
					// tripping over the entry that was already released individually
				})

				It("does not wedge the tab for the spec that follows", func() {
					// Prepare() does not run between Ordered Its, so this exercises the DeferCleanup path
					b.Click("#fetch")
					Eventually("#status").Should(b.HaveInnerText("200"))
				})
			})
		})

		It("fails the spec with a directive message when Await times out", func() {
			b.WithTimeout(60 * time.Millisecond).HoldResponse(ContainSubstring("/api/never-requested")).Await()
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after 60ms waiting for HoldResponse to intercept a response with URL matching"),
				ContainSubstring("/api/never-requested"),
				ContainSubstring("0 matching response(s) have been intercepted so far."),
			))
		})

		It("rejects the poll-config knobs it does not support", func() {
			b.WithPolling(time.Millisecond).HoldResponse(ContainSubstring("/api/echo"))
			ExpectFailures(ContainSubstring("HoldResponse does not support WithPolling"))

			b.Immediate().HoldResponse(ContainSubstring("/api/echo"))
			ExpectFailures(ContainSubstring("HoldResponse does not support Immediate"))
		})

		Describe("deadlock safety", Ordered, func() {
			It("can leave a response held at the end of a spec", func() {
				hold := b.HoldResponse(ContainSubstring("/api/echo"))
				b.Click("#fetch")
				hold.Await()
				// deliberately never released - the DeferCleanup HoldResponse registers must free it
			})

			It("does not wedge the tab for the spec that follows", func() {
				// Prepare() does not run between Ordered Its, so this exercises the DeferCleanup path
				b.Navigate(fixtureServer + "/network-interception.html")
				Eventually("#hello").Should(b.Exist())
				b.Click("#fetch")
				Eventually("#status").Should(b.HaveInnerText("200"))
			})
		})
	})

	Describe("interception and the HTTP cache", func() {
		var cacheServer string
		var serverHits int32

		BeforeEach(func() {
			atomic.StoreInt32(&serverHits, 0)
			s := ghttp.NewServer()
			s.RouteToHandler("GET", "/cacheable.json", func(w http.ResponseWriter, r *http.Request) {
				hit := atomic.AddInt32(&serverHits, 1)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "max-age=3600")
				fmt.Fprintf(w, `{"hit": %d}`, hit)
			})
			s.RouteToHandler("GET", "/network-cache.html", func(w http.ResponseWriter, r *http.Request) {
				fixture, err := os.ReadFile("./fixtures/network-cache.html")
				Expect(err).NotTo(HaveOccurred())
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Cache-Control", "no-store")
				w.Write(fixture)
			})
			// the high-fidelity lane's full Chrome asks for /favicon.ico; this server only routes
			// the two things the spec cares about, so let everything else 404 quietly
			s.AllowUnhandledRequests = true
			s.UnhandledRequestStatusCode = http.StatusNotFound
			cacheServer = s.URL()
			DeferCleanup(s.Close)

			b.Navigate(cacheServer + "/network-cache.html")
			Eventually("#hello").Should(b.Exist())
		})

		// A response served out of Chrome's cache never traverses the network stack, so it raises no
		// Fetch event and would silently skip every handler.  Interception disables the cache, so a
		// cache-eligible resource is intercepted - with the *real* response - every single time.
		It("intercepts a cache-eligible response every time it is requested", func() {
			lock := &sync.Mutex{}
			bodies := []string{}
			b.ModifyResponse(ContainSubstring("/cacheable.json")).Using(func(r biloba.InterceptedResponse) biloba.StubResponse {
				lock.Lock()
				bodies = append(bodies, r.Body)
				lock.Unlock()
				return biloba.StubResponse{Status: r.Status, Body: r.Body, Headers: r.Headers}
			})

			b.Click("#fetch-cacheable")
			Eventually("#body").Should(b.HaveInnerText(`{"hit": 1}`))
			b.Click("#fetch-cacheable")
			Eventually("#body").Should(b.HaveInnerText(`{"hit": 2}`))

			// two interceptions, each of the live response - not one interception and one cache hit
			lock.Lock()
			defer lock.Unlock()
			Expect(bodies).To(Equal([]string{`{"hit": 1}`, `{"hit": 2}`}))
			Expect(atomic.LoadInt32(&serverHits)).To(Equal(int32(2)))
		})

		It("restores the cache once interception is torn down by Prepare", func() {
			b.ModifyResponse(ContainSubstring("/cacheable.json")).WithStatus(200)
			b.Click("#fetch-cacheable")
			Eventually("#body").Should(b.HaveInnerText(`{"hit": 1}`))
			Expect(atomic.LoadInt32(&serverHits)).To(Equal(int32(1)))

			b.Prepare() // tears down interception and turns the cache back on

			b.Navigate(cacheServer + "/network-cache.html")
			Eventually("#hello").Should(b.Exist())
			b.Click("#fetch-cacheable")
			// the cache is live again, so this is answered without troubling the server
			Eventually("#body").Should(b.HaveInnerText(`{"hit": 1}`))
			Consistently(func() int32 { return atomic.LoadInt32(&serverHits) }, 100*time.Millisecond).Should(Equal(int32(1)))
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

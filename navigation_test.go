package biloba_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Navigation", func() {
	Describe("navigating to a new page", func() {
		It("succeeds with http.StatusOK if all is well", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually("body a").Should(b.Exist())
		})

		It("fails with the relevant status code otherwise", func() {
			b.Navigate(fixtureServer + "/non-existing")
			ExpectFailures(fmt.Sprintf("failed to navigate to %s: expected status code 200, got 404", fixtureServer+"/non-existing"))
		})

		It("succeeds when given a status", func() {
			b.NavigateWithStatus(fixtureServer+"/non-existing", http.StatusNotFound)
		})

		It("errors when the url is malformed", func() {
			b.Navigate("floop")
			ExpectFailures(ContainSubstring("Cannot navigate to invalid URL"))
		})

		It("succeeds when navigating to about:blank", func() {
			b.Navigate("about:blank")
		})

		It("fails fast when a navigation wedges instead of hanging the whole suite", func() {
			// A server that accepts the connection but never responds leaves chromedp.Navigate blocked on
			// the load event - the exact wedge that used to consume the entire Ginkgo suite timeout. With a
			// bounded navigation it should instead fail promptly with a legible message.
			block := make(chan struct{})
			hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-block:
				case <-r.Context().Done():
				}
			}))
			DeferCleanup(func() {
				close(block)
				hang.Close()
			})

			defer biloba.SetNavigationTimeoutForTest(500 * time.Millisecond)()

			b.Navigate(hang.URL)
			ExpectFailures(ContainSubstring(fmt.Sprintf("timed out after 500ms navigating to %s: the navigation never completed", hang.URL)))
		})
	})

	Describe("title", func() {
		It("returns the page's title", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually(b.Title).Should(Equal("Nav-A Testpage"))
		})
	})

	Describe("location", func() {
		It("returns the page's url", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually(b.Location).Should(Equal(fixtureServer + "/nav-a.html"))
		})
	})

	Describe("HaveURL", func() {
		It("matches against the tab's location", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually(b).Should(b.HaveURL(fixtureServer + "/nav-a.html"))
			Eventually(b).Should(b.HaveURL(HaveSuffix("nav-a.html")))
			Ω(b).ShouldNot(b.HaveURL(HaveSuffix("nav-b.html")))
		})

		It("can be used to poll for navigation", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually("#to-b").Should(b.Click())
			Eventually(b).Should(b.HaveURL(fixtureServer + "/nav-b.html"))
		})
	})

	Describe("HaveTitle", func() {
		It("matches against the tab's title", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually(b).Should(b.HaveTitle("Nav-A Testpage"))
			Eventually(b).Should(b.HaveTitle(HavePrefix("Nav-A")))
			Ω(b).ShouldNot(b.HaveTitle("Nav-B Testpage"))
		})
	})

	Describe("navigation flow", func() {
		It("allows the user to navigate across urls", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually("#to-b").Should(b.Click())
			Eventually(b.Location).Should(Equal(fixtureServer + "/nav-b.html"))
			Eventually(b.Title).Should(Equal("Nav-B Testpage"))
			Eventually("#to-a").Should(b.Exist())

			b.Click("#to-a")
			Eventually(b.Location).Should(Equal(fixtureServer + "/nav-a.html"))
			Eventually(b.Title).Should(Equal("Nav-A Testpage"))
		})

		It("allows the user to navigate to new tabs", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually("#to-b-new").Should(b.Click())
			Eventually(b).Should(b.HaveSpawnedTab().WithTitle("Nav-B Testpage"))
			Ω(b.Location()).Should(Equal(fixtureServer + "/nav-a.html"))
			Ω(b.AllSpawnedTabs().Find(b.TabMatching().WithTitle("Nav-B Testpage")).Location()).Should(HaveSuffix("nav-b.html"))
		})
	})
})

package biloba_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// transientCDPError is the sort of error a live navigation can legitimately throw while the target is
// swapped mid-navigation - it should be treated as a retryable miss, not a fatal.
const transientCDPError = "Inspected target navigated or closed (-32000)"

// failNTimes returns a func() error, suitable for biloba.SetTransientReadErrorForTest, that returns
// transientCDPError for its first n invocations and nil thereafter - simulating a transient CDP error
// that clears on its own after a few polls.
func failNTimes(n int) func() error {
	var count int32
	return func() error {
		if atomic.AddInt32(&count, 1) <= int32(n) {
			return errors.New(transientCDPError)
		}
		return nil
	}
}

// alwaysFail is suitable for biloba.SetTransientReadErrorForTest and simulates a CDP error that never
// clears, so callers can exercise the timeout/fail-fast paths.
func alwaysFail() error {
	return errors.New(transientCDPError)
}

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
			Eventually(b.GetTitle).Should(Equal("Nav-A Testpage"))
		})

		It("polls: a transient CDP error is a retryable miss, not a fatal - the call succeeds once the error clears", func() {
			// This is the regression case: a bare gt.Fatalf on a transient CDP error would panic out of
			// the poll instead of being retried. GetTitle must swallow a few failed attempts and return
			// the real value once simulateTransientReadError stops erroring.
			b.Navigate(fixtureServer + "/nav-a.html")
			defer biloba.SetTransientReadErrorForTest(failNTimes(3))()
			Ω(b.GetTitle()).Should(Equal("Nav-A Testpage"))
		})

		It("times out and surfaces the underlying CDP error if it never clears", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			defer biloba.SetTransientReadErrorForTest(alwaysFail)()
			b.WithTimeout(time.Millisecond * 60).GetTitle()
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring(transientCDPError),
			))
		})

		It("fails fast under Immediate() instead of polling", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			defer biloba.SetTransientReadErrorForTest(alwaysFail)()
			Ω(b.Immediate().GetTitle()).Should(Equal(""))
			ExpectFailures(ContainSubstring(transientCDPError))
		})
	})

	Describe("location", func() {
		It("returns the page's url", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually(b.GetLocation).Should(Equal(fixtureServer + "/nav-a.html"))
		})

		It("polls: a transient CDP error is a retryable miss, not a fatal - the call succeeds once the error clears", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			defer biloba.SetTransientReadErrorForTest(failNTimes(3))()
			Ω(b.GetLocation()).Should(Equal(fixtureServer + "/nav-a.html"))
		})

		It("times out and surfaces the underlying CDP error if it never clears", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			defer biloba.SetTransientReadErrorForTest(alwaysFail)()
			b.WithTimeout(time.Millisecond * 60).GetLocation()
			ExpectFailures(SatisfyAll(
				ContainSubstring("Timed out after"),
				ContainSubstring(transientCDPError),
			))
		})

		It("fails fast under Immediate() instead of polling", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			defer biloba.SetTransientReadErrorForTest(alwaysFail)()
			Ω(b.Immediate().GetLocation()).Should(Equal(""))
			ExpectFailures(ContainSubstring(transientCDPError))
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

		It("treats a transient CDP error as a retryable miss instead of aborting the poll", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			defer biloba.SetTransientReadErrorForTest(failNTimes(3))()
			Eventually(b).Should(b.HaveURL(fixtureServer + "/nav-a.html"))
		})
	})

	Describe("HaveTitle", func() {
		It("matches against the tab's title", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually(b).Should(b.HaveTitle("Nav-A Testpage"))
			Eventually(b).Should(b.HaveTitle(HavePrefix("Nav-A")))
			Ω(b).ShouldNot(b.HaveTitle("Nav-B Testpage"))
		})

		It("treats a transient CDP error as a retryable miss instead of aborting the poll", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			defer biloba.SetTransientReadErrorForTest(failNTimes(3))()
			Eventually(b).Should(b.HaveTitle("Nav-A Testpage"))
		})
	})

	Describe("navigation flow", func() {
		It("allows the user to navigate across urls", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually("#to-b").Should(b.Click())
			Eventually(b.GetLocation).Should(Equal(fixtureServer + "/nav-b.html"))
			Eventually(b.GetTitle).Should(Equal("Nav-B Testpage"))
			Eventually("#to-a").Should(b.Exist())

			b.Click("#to-a")
			Eventually(b.GetLocation).Should(Equal(fixtureServer + "/nav-a.html"))
			Eventually(b.GetTitle).Should(Equal("Nav-A Testpage"))
		})

		It("allows the user to navigate to new tabs", func() {
			b.Navigate(fixtureServer + "/nav-a.html")
			Eventually("#to-b-new").Should(b.Click())
			Eventually(b).Should(b.HaveSpawnedTab().WithTitle("Nav-B Testpage"))
			Ω(b.GetLocation()).Should(Equal(fixtureServer + "/nav-a.html"))
			Ω(b.AllSpawnedTabs().Find(b.TabMatching().WithTitle("Nav-B Testpage")).GetLocation()).Should(HaveSuffix("nav-b.html"))
		})
	})
})

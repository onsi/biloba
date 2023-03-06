package biloba_test

import (
	"fmt"
	"net/http"

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
			Eventually(b).Should(b.HaveSpawnedTab(b.TabWithTitle("Nav-B Testpage")))
			Ω(b.Location()).Should(Equal(fixtureServer + "/nav-a.html"))
			Ω(b.FindSpawnedTab(b.TabWithTitle("Nav-B Testpage")).Location()).Should(HaveSuffix("nav-b.html"))
		})
	})
})

package biloba_test

import (
	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Prepare resets per-spec state so it does not leak between specs", func() {
	// The root tab is reused across specs within a process, so Prepare() (which runs in the
	// BeforeEach) must reset everything a spec might have touched. These exercise Prepare()
	// directly rather than relying on cross-spec ordering, which -p --randomize-all does not
	// guarantee.

	It("clears cookies, localStorage, and sessionStorage", func() {
		// Cookies and web storage live on the browser context / origin and survive an about:blank
		// navigation, so Prepare() must clear them explicitly.
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())

		b.SetCookie(biloba.Cookie{Name: "spec_cookie", Value: "v"})
		b.LocalStorage().Set("spec_ls", "v")
		b.SessionStorage().Set("spec_ss", "v")

		Expect(b).To(b.HaveCookie("spec_cookie"))
		Expect(b).To(b.HaveLocalStorageItem("spec_ls"))
		Expect(b).To(b.HaveSessionStorageItem("spec_ss"))

		b.Prepare()
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())

		Expect(b).To(b.HaveNumCookies(0))
		Expect(b).To(b.HaveNumLocalStorageItems(0))
		Expect(b).To(b.HaveNumSessionStorageItems(0))
	})

	It("closes tabs opened during the spec", func() {
		b.NewTab()
		b.NewTab()
		Expect(len(b.AllTabs())).To(BeNumerically(">", 1))

		b.Prepare()
		Expect(b.AllTabs()).To(HaveLen(1)) // only the reusable root tab remains
	})

	It("clears the recorded dialogs list", func() {
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())
		b.Run("alert('between-spec dialog')") // recorded and auto-handled
		Eventually(b.Dialogs).ShouldNot(BeEmpty())

		b.Prepare()
		Expect(b.Dialogs()).To(BeEmpty())
	})

	It("does not attribute the outgoing page's beforeunload to the next spec", func() {
		// Prepare ends by navigating to about:blank, which unloads the page the previous spec left
		// behind - and dialogs.html registers a beforeunload handler.  That dialog is raised after the
		// state reset, so it used to land in the next spec's Dialogs() and print "you should add an
		// explicit dialog handler" against a spec that never opened a dialog.  That is what flaked
		// DialogHandling's `Ω(gt.buffer).ShouldNot(gbytes.Say("unhandled dialog"))` whenever
		// --randomize-all happened to schedule a dialogs.html spec immediately before it.
		b.Navigate(fixtureServer + "/dialogs.html")
		Eventually("#status").Should(b.HaveInnerText("Green Alert!"))
		gt.reset()

		b.Prepare()

		Expect(b.Dialogs()).To(BeEmpty())
		Expect(string(gt.buffer.Contents())).NotTo(ContainSubstring("unhandled dialog"))
	})

	It("resets dialog handlers", func() {
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())
		// register a handler that would ACCEPT confirms (the default is to cancel)
		b.HandleConfirmDialogs().WithResponse(true)

		b.Prepare()
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())

		// with the handler reset, the confirm falls back to the default (cancel) and returns false;
		// if the handler had leaked across Prepare it would have returned true
		var accepted bool
		b.Run("confirm('proceed?')", &accepted)
		Expect(accepted).To(BeFalse())
	})

	It("clears tracked downloads", func() {
		b.Navigate(fixtureServer + "/downloads.html")
		Eventually("#download").Should(b.Exist())
		b.Click("#download")
		Eventually(b.AllCompleteDownloads).Should(HaveLen(1))

		b.Prepare()
		Expect(b.AllDownloads()).To(BeEmpty())
	})

	It("clears observed network requests", func() {
		b.Navigate(fixtureServer + "/network.html")
		Eventually("#hello").Should(b.Exist())
		b.Click("#fetch-users")
		Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/users")))

		b.Prepare()
		Expect(b.AllRequests()).To(BeEmpty())
	})

	It("clears request stubs and disables interception", func() {
		b.Navigate(fixtureServer + "/network.html")
		Eventually("#hello").Should(b.Exist())
		b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{Body: `{"stubbed": true}`})
		b.Click("#fetch-users")
		Eventually("#result").Should(b.HaveInnerText(ContainSubstring("stubbed")))

		b.Prepare()
		b.Navigate(fixtureServer + "/network.html")
		Eventually("#hello").Should(b.Exist())

		// with the stub cleared, the request now reaches the real echoing backend
		b.Click("#fetch-users")
		Eventually("#result").Should(b.HaveInnerText(ContainSubstring("/api/users")))
		Expect("#result").NotTo(b.HaveInnerText(ContainSubstring("stubbed")))
	})
})

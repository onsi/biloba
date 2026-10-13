package biloba_test

import (
	"strings"
	"sync/atomic"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
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

	It("drops what the previous spec's page raises after Prepare navigates it away", func() {
		b.Navigate(fixtureServer + "/stragglers.html")
		Eventually("#hello").Should(b.Exist())

		// Watch the raw CDP events.  This listener is registered after Biloba's, and chromedp calls a
		// target's listeners in order, so once it has seen an event Biloba has already handled it.
		var watching atomic.Bool
		watching.Store(true)
		DeferCleanup(func() { watching.Store(false) })
		var hangID, stragglerID atomic.Value
		seen := make(chan string, 20)
		chromedp.ListenTarget(b.Context, func(ev any) {
			if !watching.Load() {
				return
			}
			switch ev := ev.(type) {
			case *network.EventRequestWillBeSent:
				if strings.HasSuffix(ev.Request.URL, "/hang") {
					hangID.Store(ev.RequestID)
				}
				if strings.HasSuffix(ev.Request.URL, "/api/straggler") {
					stragglerID.Store(ev.RequestID)
					seen <- "request"
				}
			case *network.EventLoadingFinished, *network.EventLoadingFailed:
				var id network.RequestID
				if finished, ok := ev.(*network.EventLoadingFinished); ok {
					id = finished.RequestID
				} else {
					id = ev.(*network.EventLoadingFailed).RequestID
				}
				if hang, ok := hangID.Load().(network.RequestID); ok && hang == id {
					seen <- "hang ended"
				}
				if straggler, ok := stragglerID.Load().(network.RequestID); ok && straggler == id {
					seen <- "straggler ended"
				}
			case *runtime.EventConsoleAPICalled:
				seen <- "console"
			case *runtime.EventExceptionThrown:
				if ev.ExceptionDetails != nil && ev.ExceptionDetails.Exception != nil && strings.Contains(ev.ExceptionDetails.Exception.Description, "straggler") {
					seen <- "thrown"
				}
			}
		})

		// a fetch that never answers stays in flight into Prepare
		b.Run(`window.hang()`)
		Eventually(b).ShouldNot(b.BeNetworkIdle())

		b.Prepare()
		// the page logs, throws, and sends a request as Prepare navigates it away, and Chrome then
		// aborts its two requests
		var events []string
		Eventually(func() []string {
			for {
				select {
				case event := <-seen:
					events = append(events, event)
				default:
					return events
				}
			}
		}).Should(ContainElements("console", "thrown", "request", "hang ended"))

		Ω(b.AllPageErrors()).Should(BeEmpty())
		Ω(b.AllConsoleMessages()).Should(BeEmpty())
		Ω(b.AllRequests()).Should(BeEmpty())
		Ω(b).Should(b.BeNetworkIdle())
		Ω(b.SafeAllTabConsoleErrorsForTest()).Should(BeEmpty())
		Ω(string(gt.buffer.Contents())).ShouldNot(ContainSubstring("straggler"))

		// and the next page's own events still count
		b.Navigate(fixtureServer + "/page-errors.html")
		Eventually("#hello").Should(b.Exist())
		Eventually(b.AllPageErrors).Should(HaveLen(2))
		Eventually(b.AllRequests).ShouldNot(BeEmpty())
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

package biloba_test

import (
	"github.com/onsi/biloba"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Prepare resets browsing state between specs", func() {
	// The root tab is reused across specs within a process. Cookies and web storage live on the
	// browser context / origin and survive an about:blank navigation, so Prepare() must clear them
	// or state would leak from one spec into the next. This exercises Prepare() directly (rather
	// than relying on cross-spec ordering, which -p --randomize-all does not guarantee).
	It("clears cookies, localStorage, and sessionStorage", func() {
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())

		b.SetCookie(biloba.Cookie{Name: "spec_cookie", Value: "v"})
		b.LocalStorage().Set("spec_ls", "v")
		b.SessionStorage().Set("spec_ss", "v")

		// sanity check: the state is actually there
		Expect(b).To(b.HaveCookie("spec_cookie"))
		Expect(b).To(b.HaveLocalStorageItem("spec_ls"))
		Expect(b).To(b.HaveSessionStorageItem("spec_ss"))

		// Prepare() is what runs in the BeforeEach between every spec - it must wipe the slate
		b.Prepare()
		b.Navigate(fixtureServer + "/dom.html")
		Eventually("#hello").Should(b.Exist())

		Expect(b).To(b.HaveNumCookies(0))
		Expect(b).To(b.HaveNumLocalStorageItems(0))
		Expect(b).To(b.HaveNumSessionStorageItems(0))
	})
})

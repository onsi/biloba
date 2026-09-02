package biloba_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Biloba's bridge is only usable if window._biloba is in the document before anything asks for it.
// Chrome holds that invariant: biloba.js is registered once per tab to run at the start of every
// document the tab creates.  These specs assert the guarantee from inside the page - the only place
// that can tell "it was already there" apart from "Biloba noticed it was missing and put it back".
var _ = Describe("installing biloba.js at document start", func() {
	BeforeEach(func() {
		b.Navigate(fixtureServer + "/early_biloba.html")
		Eventually("#ready").Should(b.Exist())
	})

	It("is installed before the page's own scripts run", func() {
		// __bilobaAtParse is captured by an inline script at the top of <head>, so this is the page
		// reporting what it saw at its earliest possible moment.  A lazily-installed bridge reads
		// "undefined" here and only becomes "object" later, which is exactly the window a
		// self-navigating page can land a command in.
		var atParse string
		b.Run("window.__bilobaAtParse", &atParse)
		Expect(atParse).To(Equal("object"))
	})

	It("is installed in same-origin child frames too", func() {
		// The registration covers every frame, not just the main one.  Biloba reaches into a
		// same-origin frame from the main frame (see pierceRoot), so it does not need the frame's own
		// copy - this asserts the documented behavior rather than a requirement.
		Eventually("#frame >>> #frame-ready").Should(b.Exist())
		var atParse string
		b.Run(`document.getElementById("frame").contentWindow.__bilobaAtParse`, &atParse)
		Expect(atParse).To(Equal("object"))
	})

	It("survives a navigation without Biloba reinstalling it", func() {
		// The point of the registration: the next document has the bridge before Biloba touches it, so
		// nothing has to notice a navigation happened and repair it.
		b.Navigate(fixtureServer + "/early_biloba.html")
		Eventually("#ready").Should(b.Exist())
		var atParse string
		b.Run("window.__bilobaAtParse", &atParse)
		Expect(atParse).To(Equal("object"))
	})
})

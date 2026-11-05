package engine

import (
	"errors"
	"fmt"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/chromedp"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// Both clients reinstall biloba.js and run a handler again only for BilobaMissing: a retry for any
// other failure, a target that went away above all, sends a command nothing will answer.
var _ = ginkgo.Describe("BilobaMissing", ginkgo.Label("no-browser"), func() {
	ginkgo.It("is a page without biloba.js, or one that cleared it, and nothing else", func() {
		gone := cdproto.Error{Code: -32000, Message: "Inspected target navigated or closed"}
		detached := fmt.Errorf("target T1: %w: inspected target navigated or closed", chromedp.ErrTargetDetached)
		gomega.Expect(BilobaMissing(errors.New("exception: ReferenceError: _biloba is not defined"), "click")).To(gomega.BeTrue())
		gomega.Expect(BilobaMissing(fmt.Errorf("evaluate: %w", errors.New("_biloba is not defined")), "")).To(gomega.BeTrue(), "a script's own call into biloba.js")
		cleared := errors.New("TypeError: Cannot read properties of undefined (reading 'getProperty')")
		gomega.Expect(BilobaMissing(cleared, "getProperty")).To(gomega.BeTrue(), "the page set _biloba to undefined")

		gomega.Expect(BilobaMissing(nil, "click")).To(gomega.BeFalse())
		gomega.Expect(BilobaMissing(cleared, "click")).To(gomega.BeFalse(), "a TypeError inside the handler, not reading the handler itself")
		gomega.Expect(BilobaMissing(cleared, "")).To(gomega.BeFalse(), "a script's own TypeError")
		gomega.Expect(BilobaMissing(&gone, "click")).To(gomega.BeFalse(), "Chrome's answer for a target that went away")
		gomega.Expect(BilobaMissing(detached, "click")).To(gomega.BeFalse(), "chromedp's, for a command Chrome never answered")
		gomega.Expect(BilobaMissing(&cdproto.Error{Code: -32000, Message: "Cannot find context with specified id"}, "click")).To(gomega.BeFalse())
		gomega.Expect(BilobaMissing(&Error{Code: CodeSessionClosed, Message: "the session's target was closed"}, "click")).To(gomega.BeFalse())
	})
})

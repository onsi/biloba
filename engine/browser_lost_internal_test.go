package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/chromedp/chromedp"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// chromedp cancels the contexts under a lost browser from a goroutine of its own, so a command can
// fail on the lost connection while the session's context still reports no error.  These hold the
// connection closed and the context live - that window - and check the browser is still reported gone.
var _ = ginkgo.Describe("a browser whose connection is lost", ginkgo.Label("no-browser"), func() {
	var ctx context.Context
	var lost chan struct{}

	ginkgo.BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = chromedp.NewContext(context.Background())
		ginkgo.DeferCleanup(cancel)
		lost = make(chan struct{})
		chromedp.FromContext(ctx).Browser = &chromedp.Browser{LostConnection: lost}
	})

	ginkgo.It("is reported lost from its connection alone", func() {
		gomega.Expect(BrowserLost(ctx)).To(gomega.BeFalse())
		close(lost)
		gomega.Expect(BrowserLost(ctx)).To(gomega.BeTrue())
		gomega.Expect(ctx.Err()).NotTo(gomega.HaveOccurred())
		gomega.Expect(BrowserLost(context.Background())).To(gomega.BeFalse(), "a context with no browser is not one that lost it")
	})

	ginkgo.It("fails an operation as browser_gone, not as the caller's cancellation", func() {
		close(lost)
		session := &Session{ctx: ctx}
		err := session.serial(context.Background(), "evaluate", func(context.Context) error {
			return fmt.Errorf("lost the connection to the browser: EOF: %w", context.Canceled)
		})
		var engineErr *Error
		gomega.Expect(errors.As(err, &engineErr)).To(gomega.BeTrue())
		gomega.Expect(engineErr.Code).To(gomega.Equal(CodeBrowserGone))
	})
})

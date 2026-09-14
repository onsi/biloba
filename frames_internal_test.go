package biloba

import (
	"context"
	"errors"
	"fmt"

	"github.com/chromedp/cdproto"
	"github.com/onsi/biloba/engine"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("frame validation polling", ginkgo.Label("no-browser"), func() {
	ginkgo.It("retries transient errors but stops for a lost document", func() {
		tab := &Biloba{Context: context.Background()}
		gomega.Expect(tab.frameValidationStopsPolling(&engine.Error{Code: engine.CodeDeadline})).To(gomega.BeFalse())
		gomega.Expect(tab.frameValidationStopsPolling(errors.New("temporary transport failure"))).To(gomega.BeFalse())
		gomega.Expect(tab.frameValidationStopsPolling(fmt.Errorf("wrapped: %w", &engine.Error{Code: engine.CodeFrameDetached}))).To(gomega.BeTrue())
		// A candidate can disappear during discovery without invalidating the subject.
		candidateGone := &cdproto.Error{Code: -32000, Message: "Inspected target navigated or closed"}
		gomega.Expect(tab.frameValidationStopsPolling(candidateGone)).To(gomega.BeFalse())
		frame := &Biloba{Context: context.Background(), frame: &frameScope{}}
		gomega.Expect(frame.frameValidationStopsPolling(candidateGone)).To(gomega.BeFalse())

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		tab.Context = ctx
		gomega.Expect(tab.frameValidationStopsPolling(context.Canceled)).To(gomega.BeTrue())
	})
})

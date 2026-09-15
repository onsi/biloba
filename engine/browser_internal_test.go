package engine

import (
	"context"

	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("executorContext", ginkgo.Label("no-browser"), func() {
	ginkgo.It("is already cancelled when the request context was cancelled before the call", func() {
		requestCtx, cancelRequest := context.WithCancel(context.Background())
		cancelRequest()

		ctx, cancel := executorContext(context.Background(), requestCtx)
		defer cancel()
		gomega.Expect(ctx.Err()).To(gomega.HaveOccurred())
	})

	ginkgo.It("follows a request context cancelled later", func() {
		requestCtx, cancelRequest := context.WithCancel(context.Background())
		defer cancelRequest()

		ctx, cancel := executorContext(context.Background(), requestCtx)
		defer cancel()
		gomega.Expect(ctx.Err()).NotTo(gomega.HaveOccurred())

		cancelRequest()
		gomega.Eventually(ctx.Done()).Should(gomega.BeClosed())
	})
})

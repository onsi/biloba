package engine

import (
	"context"
	"testing"

	"github.com/onsi/gomega"
)

// executorContext ties a command's context to requestCtx's cancellation via a goroutine, so a
// caller that races ahead and dispatches a command against the returned context right after this
// call can - if requestCtx was already done at entry - still win that race and reach Chrome before
// the goroutine gets scheduled. That is exactly what let Session.tabs's Target.getTargets sometimes
// succeed against an already-cancelled Prepare context under --race on CI, surfacing the raw
// "context canceled" instead of the wrapped "list tabs" failure lifecycle_foundations_test.go
// expects. executorContext must notice a requestCtx that is already done synchronously, before
// returning, rather than only eventually via the goroutine.
func TestExecutorContextCancelsSynchronouslyWhenRequestContextAlreadyDone(t *testing.T) {
	g := gomega.NewWithT(t)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()

	ctx, cancel := executorContext(context.Background(), requestCtx)
	defer cancel()

	g.Expect(ctx.Err()).To(gomega.HaveOccurred(), "an already-done requestCtx must be reflected immediately, not after an async goroutine gets scheduled")
}

func TestExecutorContextStillTracksRequestContextCancelledLater(t *testing.T) {
	g := gomega.NewWithT(t)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()

	ctx, cancel := executorContext(context.Background(), requestCtx)
	defer cancel()
	g.Expect(ctx.Err()).NotTo(gomega.HaveOccurred())

	cancelRequest()
	<-ctx.Done()
	g.Expect(ctx.Err()).To(gomega.HaveOccurred())
}

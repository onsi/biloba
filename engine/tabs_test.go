package engine_test

import (
	"context"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("attaching a tab the page spawned", func() {
	var root *engine.Session
	var rootCtx context.Context
	var popup target.ID

	listed := func() bool {
		targets, err := chromedp.Targets(rootCtx)
		Expect(err).NotTo(HaveOccurred())
		for _, info := range targets {
			if info.TargetID == popup {
				return true
			}
		}
		return false
	}

	BeforeEach(func(ctx SpecContext) {
		var err error
		root, err = browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		rootCtx = engine.SessionContextForTest(root)
		created, err := chromedp.CallBrowser(rootCtx, target.CreateTarget, target.CreateTargetParams{
			URL: "about:blank", BrowserContextID: engine.SessionBrowserContextForTest(root), NewWindow: new(true),
		})
		Expect(err).NotTo(HaveOccurred())
		popup = created.TargetID
		DeferCleanup(func() {
			// gone before the root closes, so closing it does not find the popup still listed
			_ = engine.CloseTargetContext(rootCtx, popup)
			Eventually(listed).Should(BeFalse())
		})
	})

	It("leaves the tab open when the caller gives up before the attach gets anywhere", func(ctx SpecContext) {
		gaveUp, cancel := context.WithCancel(ctx)
		cancel()
		tab, err := engine.AttachSpawnedTabForTest(gaveUp, root, popup)
		Expect(tab).To(BeNil())
		Expect(err).To(HaveOccurred())

		// Chrome answers closeTarget before the target leaves the list, so watch for a while
		Consistently(listed, 300*time.Millisecond, 20*time.Millisecond).Should(BeTrue(), "the popup is the page's: an attach that never happened must not close it")
	})

	It("closes the tab when the caller gives up while an attach that got through is still setting up", func(ctx SpecContext) {
		stalled, _, release, err := engine.StallAttachForTest(ctx, root, popup)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(release)
		// The attempt returns as soon as the caller gives up.  Holding it there for longer than the
		// detach takes means the cleanup only sees the attach if it waits for the attempt.
		DeferCleanup(engine.SetAttachAttemptReturnedForTest(func() { time.Sleep(300 * time.Millisecond) }))

		gaveUp, cancel := context.WithCancel(ctx)
		go func() {
			select {
			case <-stalled:
			case <-ctx.Done():
			}
			cancel()
		}()
		tab, err := engine.AttachSpawnedTabForTest(gaveUp, root, popup)
		Expect(tab).To(BeNil())
		Expect(err).To(HaveOccurred())
		Expect(stalled).To(BeClosed(), "the attach got through and stalled in chromedp's setup")
		release()

		Eventually(listed).Should(BeFalse(), "the attach got through, so discarding it closes the popup")
	})
})

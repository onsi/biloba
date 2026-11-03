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
	It("leaves the tab open when the caller gives up before the attach gets anywhere", func(ctx SpecContext) {
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		rootCtx := engine.SessionContextForTest(root)
		popup, err := chromedp.CallBrowser(rootCtx, target.CreateTarget, target.CreateTargetParams{
			URL: "about:blank", BrowserContextID: engine.SessionBrowserContextForTest(root), NewWindow: new(true),
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			// gone before the root closes, so closing it does not find the popup still listed
			_ = engine.CloseTargetContext(rootCtx, popup.TargetID)
			Eventually(func() bool {
				targets, err := chromedp.Targets(rootCtx)
				if err != nil {
					return true
				}
				for _, info := range targets {
					if info.TargetID == popup.TargetID {
						return false
					}
				}
				return true
			}).Should(BeTrue())
		})

		gaveUp, cancel := context.WithCancel(ctx)
		cancel()
		tab, err := engine.AttachSpawnedTabForTest(gaveUp, root, popup.TargetID)
		Expect(tab).To(BeNil())
		Expect(err).To(HaveOccurred())

		// Chrome answers closeTarget before the target leaves the list, so watch for a while
		Consistently(func() []target.ID {
			targets, err := chromedp.Targets(rootCtx)
			Expect(err).NotTo(HaveOccurred())
			ids := []target.ID{}
			for _, info := range targets {
				ids = append(ids, info.TargetID)
			}
			return ids
		}, 300*time.Millisecond, 20*time.Millisecond).Should(ContainElement(popup.TargetID), "the popup is the page's: an attach that never happened must not close it")
	})
})

package engine_test

import (
	"context"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("event listeners on an allocator's connections", func() {
	It("keep a connection's listeners when another connection from the same allocator goes away", func() {
		// A retried bootstrap is the case: one allocator, a first connection given up on, a second kept.
		allocCtx, cancelAllocator := engine.NewRemoteAllocator(context.Background(), browser.WebSocketURL(), false, nil)
		DeferCleanup(cancelAllocator)
		first, cancelFirst := chromedp.NewContext(allocCtx)
		Expect(chromedp.Do(first)).To(Succeed())
		second, cancelSecond := chromedp.NewContext(allocCtx)
		DeferCleanup(cancelSecond)
		Expect(chromedp.Do(second)).To(Succeed())
		Expect(chromedp.FromContext(first).Browser).NotTo(BeIdenticalTo(chromedp.FromContext(second).Browser), "each context should have a connection of its own")

		seen := make(chan string, 10)
		Expect(engine.ListenTarget(second, func(ev any) {
			if called, ok := ev.(*runtime.EventConsoleAPICalled); ok && len(called.Args) > 0 {
				seen <- string(called.Args[0].Value)
			}
		}, runtime.ConsoleAPICalled.Method)).To(Succeed())

		lost := chromedp.FromContext(first).Browser.LostConnection
		cancelFirst()
		Eventually(lost).WithTimeout(5 * time.Second).Should(BeClosed())
		time.Sleep(100 * time.Millisecond) // the lost connection's cleanup runs on a goroutine of its own

		Expect(chromedp.Do(second, chromedp.Evaluate[chromedp.Void](`console.log("still listening")`))).To(Succeed())
		Eventually(seen).Should(Receive(Equal(`"still listening"`)))
	})
})

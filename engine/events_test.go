package engine_test

import (
	"context"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

var _ = Describe("two attachments to one target", func() {
	It("each track the target's documents, from what it already has, independently of the other", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		Expect(session.Navigate(ctx, server.URL+"/first")).To(Succeed())
		mainFrame := cdp.FrameID(session.TargetID())

		// attached at once, as two goroutines discovering the same frame target would
		type attachment struct {
			ctx    context.Context
			detach func()
			err    error
		}
		results := make(chan attachment, 2)
		for range 2 {
			go func() {
				attachedCtx, detach, err := engine.AttachFrameTargetContext(ctx, engine.SessionContextForTest(session), session.TargetID())
				results <- attachment{attachedCtx, detach, err}
			}()
		}
		first, second := <-results, <-results
		Expect(first.err).NotTo(HaveOccurred())
		Expect(second.err).NotTo(HaveOccurred())
		DeferCleanup(second.detach)

		location := func(attached context.Context) string {
			scoped, err := engine.ScopeToFrameContext(attached, mainFrame)
			if err != nil {
				return err.Error()
			}
			var href string
			if err := engine.EvaluateContext(scoped, "location.href", false, &href); err != nil {
				return err.Error()
			}
			return href
		}
		Expect(location(first.ctx)).To(HaveSuffix("/first"), "the document loaded before the attachment is known")
		Expect(location(second.ctx)).To(HaveSuffix("/first"))

		first.detach()
		Expect(session.Navigate(ctx, server.URL+"/second")).To(Succeed())
		Eventually(func() string { return location(second.ctx) }).Should(HaveSuffix("/second"), "the other attachment follows the navigation")
	})
})

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

package engine_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
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

// Biloba's listeners rely on two things chromedp.Listen promises: the events of a target in the order
// Chrome sent them, across event types, and every event that arrived before a command's answer handed
// over by the time the command returns.
var _ = Describe("a target's listeners", func() {
	It("have seen, in wire order, every event that arrived before the command that returns", func(ctx SpecContext) {
		site := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			_, _ = response.Write([]byte(`<!doctype html><script>console.log("loading")</script>`))
		}))
		DeferCleanup(site.Close)
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)

		var lock sync.Mutex
		var seen []string
		Expect(engine.ListenTarget(engine.SessionContextForTest(session), func(ev any) {
			lock.Lock()
			defer lock.Unlock()
			switch ev := ev.(type) {
			case *network.EventRequestWillBeSent:
				if ev.Type == network.ResourceTypeDocument {
					seen = append(seen, "request")
				}
			case *network.EventResponseReceived:
				if ev.Type == network.ResourceTypeDocument {
					seen = append(seen, "response")
				}
			case *runtime.EventConsoleAPICalled:
				if len(ev.Args) > 0 {
					seen = append(seen, string(ev.Args[0].Value))
				}
			}
		})).To(Succeed())

		Expect(session.Navigate(ctx, site.URL)).To(Succeed())
		lock.Lock()
		Expect(seen).To(Equal([]string{"request", "response", `"loading"`}), "the load event Navigate waits for came after these")
		lock.Unlock()

		_, err = session.Evaluate(ctx, `console.log("evaluated"), 1`)
		Expect(err).NotTo(HaveOccurred())
		lock.Lock()
		defer lock.Unlock()
		Expect(seen).To(HaveLen(4), "the console call came before the evaluation's answer")
		Expect(seen[3]).To(Equal(`"evaluated"`))
	})
})

var _ = Describe("a command whose target goes away before Chrome answers it", func() {
	It("fails as soon as Chrome detaches the target, rather than waiting for an answer that cannot come", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		tab, err := session.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())

		swallowed, restore := engine.SwallowNextCommandForTest(tab, runtime.Evaluate.Method)
		DeferCleanup(restore)
		done := make(chan error, 1)
		go func() {
			_, err := tab.Evaluate(ctx, `1`)
			done <- err
		}()
		Eventually(swallowed).Should(BeClosed())
		Consistently(done, 200*time.Millisecond).ShouldNot(Receive(), "Chrome never saw the command, so nothing answers it")

		_, err = chromedp.CallBrowser(engine.SessionContextForTest(session), target.CloseTarget, target.CloseTargetParams{TargetID: tab.TargetID()})
		Expect(err).NotTo(HaveOccurred())
		var evaluated error
		Eventually(done).WithTimeout(5 * time.Second).Should(Receive(&evaluated))
		Expect(evaluated).To(MatchError(ContainSubstring("Inspected target navigated or closed")))
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
		})).To(Succeed())

		lost := chromedp.FromContext(first).Browser.LostConnection
		cancelFirst()
		Eventually(lost).WithTimeout(5 * time.Second).Should(BeClosed())
		time.Sleep(100 * time.Millisecond) // the lost connection's cleanup runs on a goroutine of its own

		Expect(chromedp.Do(second, chromedp.Evaluate[chromedp.Void](`console.log("still listening")`))).To(Succeed())
		Eventually(seen).Should(Receive(Equal(`"still listening"`)))
	})
})

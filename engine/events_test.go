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

// targetGone matches the two ways an operation on a target that went away fails: the connection's
// answer (Chrome's own target-gone error), or the session closing once it learns the target was
// destroyed, whichever reaches the operation first.
func targetGone() OmegaMatcher {
	return SatisfyAny(
		MatchError(ContainSubstring("Inspected target navigated or closed")),
		MatchError(chromedp.ErrTargetDetached),
		WithTransform(pointerErrorCode, Equal(engine.CodeSessionClosed)),
	)
}

var _ = Describe("a command whose target goes away before Chrome answers it", func() {
	It("fails as soon as Chrome detaches the target, rather than waiting for an answer that cannot come", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		tab, err := session.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())

		swallowed, _, restore := engine.SwallowNextCommandForTest(tab, runtime.Evaluate.Method)
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
		Expect(evaluated).To(targetGone())
	})

	It("ends an operation still waiting on a target Chrome destroys, though nothing will answer it", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		tab, err := session.NewTab(ctx)
		Expect(err).NotTo(HaveOccurred())

		// Chrome never answers it: the engine reports the target closed, not chromedp's error
		lost, restore := engine.LoseNextCommandForTest(tab, runtime.Evaluate.Method)
		DeferCleanup(restore)
		done := make(chan error, 1)
		go func() {
			_, err := tab.Evaluate(ctx, `1`)
			done <- err
		}()
		Eventually(lost).Should(BeClosed())

		_, err = chromedp.CallBrowser(engine.SessionContextForTest(session), target.CloseTarget, target.CloseTargetParams{TargetID: tab.TargetID()})
		Expect(err).NotTo(HaveOccurred())
		var evaluated error
		Eventually(done).WithTimeout(5*time.Second).Should(Receive(&evaluated), "the operation holds the session's lock, which the cleanup of a destroyed target takes")
		Expect(pointerErrorCode(evaluated)).To(Equal(engine.CodeSessionClosed))
	})

	It("does not run a click again on a popup that closed under it", func(ctx SpecContext) {
		site := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			response.Header().Set("Content-Type", "text/html")
			if request.URL.Path == "/popup" {
				_, _ = response.Write([]byte(`<!doctype html><button id="close" onclick="window.close()">close</button>`))
				return
			}
			_, _ = response.Write([]byte(`<!doctype html><title>opener</title>`))
		}))
		DeferCleanup(site.Close)
		root, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(root.Close)
		Expect(root.Navigate(ctx, site.URL)).To(Succeed())
		_, err = root.Evaluate(ctx, `void window.open("/popup", "_blank")`)
		Expect(err).NotTo(HaveOccurred())
		popup, err := root.WaitForTab(ctx, engine.TabQuery{
			SpawnedOnly: true, HasElement: selectorPtr(engine.CSS("#close")),
		}, engine.PollPolicy{Timeout: 2 * time.Second, Interval: 5 * time.Millisecond})
		Expect(err).NotTo(HaveOccurred())

		// The click's evaluation goes unanswered, as when the window closes before Chrome answers it,
		// and the popup goes away: chromedp ends it with ErrTargetDetached.
		swallowed, sentAfter, restore := engine.SwallowNextCommandForTest(popup, runtime.Evaluate.Method)
		DeferCleanup(restore)
		done := make(chan error, 1)
		go func() { done <- popup.Click(ctx, engine.CSS("#close")) }()
		Eventually(swallowed).Should(BeClosed())
		_, err = chromedp.CallBrowser(engine.SessionContextForTest(root), target.CloseTarget, target.CloseTargetParams{TargetID: popup.TargetID()})
		Expect(err).NotTo(HaveOccurred())

		var clicked error
		Eventually(done).WithTimeout(5 * time.Second).Should(Receive(&clicked))
		Expect(clicked).To(targetGone())
		// The retry this guards against can also be cut short by the session learning the target was
		// destroyed, so this catches it often rather than always; BilobaMissing's own spec pins the rule.
		Expect(sentAfter()).NotTo(ContainElement(runtime.Evaluate.Method), "a target that went away is not a page missing biloba.js: nothing reinstalls it and clicks again")
	})
})

var _ = Describe("watching a target go away", func() {
	It("tells a watch of the target's detach, once, whether it came after the watch or before it", func(ctx SpecContext) {
		session, err := browser.OpenSession(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(session.Close)
		sessionCtx := engine.SessionContextForTest(session)
		created, err := chromedp.CallBrowser(sessionCtx, target.CreateTarget, target.CreateTargetParams{
			URL: "about:blank", BrowserContextID: engine.SessionBrowserContextForTest(session), NewWindow: new(true),
		})
		Expect(err).NotTo(HaveOccurred())
		attached, detach := engine.NewTabContext(sessionCtx, created.TargetID)
		DeferCleanup(detach)
		Expect(engine.TargetGone(attached)).To(BeFalse(), "a context not attached yet is not gone")
		Expect(chromedp.FromContext(attached).Target).To(BeNil(), "and asking does not attach it")
		Expect(chromedp.Do(attached, chromedp.Evaluate[chromedp.Void]("1"))).To(Succeed())
		Expect(engine.TargetGone(attached)).To(BeFalse())

		var lock sync.Mutex
		calls := map[string]int{}
		watch := func(name string) {
			Expect(engine.WatchTargetGone(attached, func() {
				lock.Lock()
				defer lock.Unlock()
				calls[name]++
			})).To(Succeed())
		}
		count := func(name string) func() int {
			return func() int {
				lock.Lock()
				defer lock.Unlock()
				return calls[name]
			}
		}
		watch("before")
		Consistently(count("before"), 100*time.Millisecond).Should(BeZero())

		Expect(engine.CloseTargetContext(sessionCtx, created.TargetID)).To(Succeed())
		Eventually(func() bool { return engine.TargetGone(attached) }).WithTimeout(5 * time.Second).Should(BeTrue())
		Eventually(count("before")).Should(Equal(1))
		_, err = chromedp.Run(attached, chromedp.Evaluate[chromedp.Void]("1"))
		Expect(err).To(MatchError(chromedp.ErrTargetDetached), "a command on the detached target fails at once")

		watch("after")
		Eventually(count("after")).Should(Equal(1), "a watch of a target that already went is told too")
		Consistently(func() []int { return []int{count("before")(), count("after")()} }, 100*time.Millisecond).Should(Equal([]int{1, 1}))
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

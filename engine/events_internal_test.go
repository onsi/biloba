package engine

import (
	"context"
	"sync"
	"time"

	"github.com/chromedp/cdproto"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// The hub is what keeps ListenTarget's promises - one order across event types, listeners called
// before chromedp sees the message, sessions bound before their first event - so these drive it with
// protocol messages directly, one rule at a time.
var _ = ginkgo.Describe("eventHub", ginkgo.Label("no-browser"), func() {
	var hub *eventHub
	var ctx context.Context
	var cancel context.CancelFunc

	event := func(session target.SessionID, method, params string) *cdproto.Message {
		return &cdproto.Message{SessionID: session, Method: cdproto.MethodType(method), Params: jsonv2.Value(params)}
	}
	detached := func(id target.ID) *cdproto.Message {
		return event("S1", page.FrameDetached.Method, `{"frameId":"`+string(id)+`","reason":"remove"}`)
	}
	methodOf := func(ev any) string {
		switch ev.(type) {
		case *page.EventFrameDetached:
			return "detached"
		case *runtime.EventExecutionContextsCleared:
			return "cleared"
		case *inspector.EventTargetCrashed:
			return "crashed"
		}
		return "other"
	}

	ginkgo.BeforeEach(func() {
		hub = newEventHub(nil)
		ctx, cancel = context.WithCancel(context.Background())
		ginkgo.DeferCleanup(cancel)
	})

	ginkgo.It("calls a session's listeners in registration order with its events in wire order, across methods", func() {
		var seen []string
		hub.add("S1", &eventListener{ctx: ctx, fn: func(ev any) { seen = append(seen, "first:"+methodOf(ev)) }})
		hub.add("S1", &eventListener{ctx: ctx, fn: func(ev any) { seen = append(seen, "second:"+methodOf(ev)) }})
		hub.add("S2", &eventListener{ctx: ctx, fn: func(ev any) { seen = append(seen, "other session") }})

		hub.received(event("S1", runtime.ExecutionContextsCleared.Method, `{}`))
		hub.received(detached("F"))
		hub.received(event("S1", inspector.TargetCrashed.Method, `{}`))

		gomega.Expect(seen).To(gomega.Equal([]string{
			"first:cleared", "second:cleared", "first:detached", "second:detached", "first:crashed", "second:crashed",
		}))
	})

	ginkgo.It("calls a listener only for the methods it asked for, and hands a raw listener the undecoded params", func() {
		var decoded []string
		var raw []string
		hub.add("S1", &eventListener{ctx: ctx, fn: func(ev any) { decoded = append(decoded, methodOf(ev)) }, methods: []string{inspector.TargetCrashed.Method}})
		hub.add("S1", &eventListener{ctx: ctx, raw: func(method string, params jsonv2.Value) { raw = append(raw, method+" "+string(params)) }, methods: []string{page.FrameDetached.Method}})

		hub.received(detached("F"))
		hub.received(event("S1", inspector.TargetCrashed.Method, `{}`))

		gomega.Expect(decoded).To(gomega.Equal([]string{"crashed"}))
		gomega.Expect(raw).To(gomega.Equal([]string{page.FrameDetached.Method + ` {"frameId":"F","reason":"remove"}`}))
	})

	ginkgo.It("binds a listener registered before the attach to the session the attachment's reply names", func() {
		var seen []string
		hub.pending["T1"] = []*eventListener{{ctx: ctx, fn: func(ev any) { seen = append(seen, methodOf(ev)) }}}

		hub.sent(&cdproto.Message{ID: 7, Method: cdproto.MethodType(target.AttachToTarget.Method), Params: jsonv2.Value(`{"targetId":"T1","flatten":true}`)})
		hub.received(&cdproto.Message{ID: 7, Result: jsonv2.Value(`{"sessionId":"S1"}`)})
		hub.received(event("S1", runtime.ExecutionContextsCleared.Method, `{}`))

		gomega.Expect(seen).To(gomega.Equal([]string{"cleared"}))
		gomega.Expect(hub.pending).To(gomega.BeEmpty())
		gomega.Expect(hub.attaching).To(gomega.BeEmpty())
	})

	ginkgo.It("binds on the browser connection's attachedToTarget, which Chrome sends before the reply, and only once", func() {
		var seen []string
		hub.pending["T1"] = []*eventListener{{ctx: ctx, fn: func(ev any) { seen = append(seen, methodOf(ev)) }}}

		hub.sent(&cdproto.Message{ID: 7, Method: cdproto.MethodType(target.AttachToTarget.Method), Params: jsonv2.Value(`{"targetId":"T1","flatten":true}`)})
		hub.received(event("", target.AttachedToTarget.Method, `{"sessionId":"S1","targetInfo":{"targetId":"T1","type":"page"},"waitingForDebugger":false}`))
		hub.received(event("S1", runtime.ExecutionContextsCleared.Method, `{}`))
		hub.received(&cdproto.Message{ID: 7, Result: jsonv2.Value(`{"sessionId":"S1"}`)})
		hub.received(event("S1", inspector.TargetCrashed.Method, `{}`))

		gomega.Expect(seen).To(gomega.Equal([]string{"cleared", "crashed"}))
	})

	ginkgo.It("leaves a waiting listener alone when Chrome auto-attaches the target under a page's session", func() {
		var seen []string
		hub.pending["T1"] = []*eventListener{{ctx: ctx, fn: func(ev any) { seen = append(seen, methodOf(ev)) }}}

		hub.received(event("PAGE", target.AttachedToTarget.Method, `{"sessionId":"AUTO","targetInfo":{"targetId":"T1","type":"iframe"},"waitingForDebugger":false}`))
		hub.received(event("AUTO", runtime.ExecutionContextsCleared.Method, `{}`))

		gomega.Expect(seen).To(gomega.BeEmpty())
		gomega.Expect(hub.pending).To(gomega.HaveKey(target.ID("T1")))
	})

	ginkgo.It("drops a session's listeners when Chrome detaches it, and ignores what still arrives for it", func() {
		var seen []string
		hub.add("S1", &eventListener{ctx: ctx, fn: func(ev any) { seen = append(seen, methodOf(ev)) }})

		hub.received(event("", target.DetachedFromTarget.Method, `{"sessionId":"S1","targetId":"T1"}`))
		gomega.Expect(func() { hub.received(detached("F")) }).NotTo(gomega.Panic())

		gomega.Expect(seen).To(gomega.BeEmpty())
		gomega.Expect(hub.listeners).NotTo(gomega.HaveKey(target.SessionID("S1")))
	})

	ginkgo.It("lets a listener cancel itself and register another while it runs", func() {
		var seen []string
		listenerCtx, cancelListener := context.WithCancel(ctx)
		hub.add("S1", &eventListener{ctx: listenerCtx, fn: func(ev any) {
			seen = append(seen, "once:"+methodOf(ev))
			cancelListener()
			hub.add("S1", &eventListener{ctx: ctx, fn: func(ev any) { seen = append(seen, "later:"+methodOf(ev)) }})
		}})

		hub.received(detached("F"))
		gomega.Eventually(func() int {
			hub.mu.Lock()
			defer hub.mu.Unlock()
			return len(hub.listeners["S1"])
		}).Should(gomega.Equal(1), "the cancelled listener is removed")
		hub.received(event("S1", inspector.TargetCrashed.Method, `{}`))

		gomega.Expect(seen).To(gomega.Equal([]string{"once:detached", "later:crashed"}))
	})

	ginkgo.It("reports a listener that holds the reader too long, when debugging is on", func() {
		var lock sync.Mutex
		var reports []string
		hub = newEventHub(func(format string, args ...any) {
			lock.Lock()
			defer lock.Unlock()
			reports = append(reports, format)
		})
		hub.add("S1", &eventListener{ctx: ctx, fn: func(any) { time.Sleep(slowListenerThreshold + 20*time.Millisecond) }})

		hub.received(detached("F"))

		lock.Lock()
		defer lock.Unlock()
		gomega.Expect(reports).To(gomega.HaveLen(1))
	})
})

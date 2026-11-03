package engine

import (
	"context"
	"maps"
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

	// a and b stand in for two chromedp contexts: the hub keys a waiting registration by its context.
	a, b := new(int), new(int)
	attachCommand := func(id int64) *cdproto.Message {
		return &cdproto.Message{ID: id, Method: cdproto.MethodType(target.AttachToTarget.Method), Params: jsonv2.Value(`{"targetId":"T1","flatten":true}`)}
	}
	attachReply := func(id int64, session string) *cdproto.Message {
		return &cdproto.Message{ID: id, Result: jsonv2.Value(`{"sessionId":"` + session + `"}`)}
	}
	attachedEvent := func(session string) *cdproto.Message {
		return event("", target.AttachedToTarget.Method, `{"sessionId":"`+session+`","targetInfo":{"targetId":"T1","type":"page"},"waitingForDebugger":false}`)
	}
	runtimeEnable := func(session target.SessionID) *cdproto.Message {
		return &cdproto.Message{ID: 99, SessionID: session, Method: cdproto.MethodType(runtime.Enable.Method)}
	}
	unclaimed := func() int {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		return len(hub.unclaimed)
	}

	ginkgo.It("queues a new session's events while a context waits, and hands them over in order when it confirms", func() {
		var seen []string
		hub.pend(a, &eventListener{ctx: ctx, fn: func(ev any) { seen = append(seen, methodOf(ev)) }})

		hub.sent(attachCommand(7))
		hub.received(attachedEvent("S1"))
		hub.received(attachReply(7, "S1"))
		hub.sent(runtimeEnable("S1")) // chromedp's first command on the new session
		hub.received(event("S1", runtime.ExecutionContextsCleared.Method, `{}`))
		hub.received(event("S1", inspector.TargetCrashed.Method, `{}`))
		gomega.Expect(seen).To(gomega.BeEmpty(), "nothing says whose session it is until its context confirms")

		hub.confirm(a, "S1")
		gomega.Expect(seen).To(gomega.Equal([]string{"cleared", "crashed"}))
		hub.received(detached("F"))
		gomega.Expect(seen).To(gomega.Equal([]string{"cleared", "crashed", "detached"}))
		gomega.Expect(hub.pending).To(gomega.BeEmpty())
		gomega.Expect(unclaimed()).To(gomega.BeZero())
	})

	ginkgo.It("keeps two contexts attaching one target apart, however their attachments, registrations, and replies interleave", func() {
		var first, second []string
		hub.pend(a, &eventListener{ctx: ctx, fn: func(ev any) { first = append(first, methodOf(ev)) }})
		hub.sent(attachCommand(7))
		hub.received(attachedEvent("S1")) // the first attachment's event...
		hub.pend(b, &eventListener{ctx: ctx, fn: func(ev any) { second = append(second, methodOf(ev)) }})
		hub.sent(attachCommand(8))
		hub.received(attachReply(7, "S1")) // ...and its reply, with the second registered in between
		hub.sent(runtimeEnable("S1"))
		hub.received(event("S1", runtime.ExecutionContextsCleared.Method, `{}`))
		hub.received(attachedEvent("S2"))
		hub.received(attachReply(8, "S2"))
		hub.sent(runtimeEnable("S2"))
		hub.received(event("S2", inspector.TargetCrashed.Method, `{}`))

		hub.confirm(b, "S2")
		hub.confirm(a, "S1")
		hub.received(event("", target.DetachedFromTarget.Method, `{"sessionId":"S1","targetId":"T1"}`)) // the first is discarded
		hub.received(event("S2", page.FrameDetached.Method, `{"frameId":"F","reason":"remove"}`))

		gomega.Expect(first).To(gomega.Equal([]string{"cleared"}))
		gomega.Expect(second).To(gomega.Equal([]string{"crashed", "detached"}), "discarding the first attachment leaves the second's listeners in place")
	})

	ginkgo.It("lets a context retry its first Run before confirming, whether or not the failed attempt attached", func() {
		var seen []string
		hub.pend(a, &eventListener{ctx: ctx, fn: func(ev any) { seen = append(seen, methodOf(ev)) }})
		hub.sent(attachCommand(7))
		hub.received(&cdproto.Message{ID: 7, Error: &cdproto.Error{Code: -32000, Message: "No target with given id found"}})
		hub.sent(attachCommand(8)) // the retry attaches
		hub.received(attachReply(8, "S1"))
		hub.sent(runtimeEnable("S1"))
		hub.received(event("S1", runtime.ExecutionContextsCleared.Method, `{}`))
		hub.sent(&cdproto.Message{ID: 100, SessionID: "S1", Method: "Runtime.evaluate"}) // a probe that fails, retried
		hub.received(event("S1", inspector.TargetCrashed.Method, `{}`))

		hub.confirm(a, "S1")
		gomega.Expect(seen).To(gomega.Equal([]string{"cleared", "crashed"}))
	})

	ginkgo.It("never hands a waiting context a session it does not confirm, and forgets one nobody confirms", func() {
		var seen []string
		hub.pend(a, &eventListener{ctx: ctx, fn: func(ev any) { seen = append(seen, methodOf(ev)) }})

		hub.received(event("PAGE", target.AttachedToTarget.Method, `{"sessionId":"AUTO","targetInfo":{"targetId":"T1","type":"iframe"},"waitingForDebugger":false}`))
		hub.sent(&cdproto.Message{ID: 9, SessionID: "AUTO", Method: "Runtime.runIfWaitingForDebugger"})
		hub.received(event("AUTO", runtime.ExecutionContextsCleared.Method, `{}`))
		hub.sent(runtimeEnable("S1"))
		hub.confirm(a, "S1")

		gomega.Expect(seen).To(gomega.BeEmpty())
		gomega.Expect(unclaimed()).To(gomega.BeZero(), "with nobody waiting, the auto-attached session's queue is dropped")
	})

	ginkgo.It("never waits to register, and lets go of a registration whose attachment failed when its context ends", func() {
		failedCtx, abandon := context.WithCancel(ctx)
		hub.pend(a, &eventListener{ctx: failedCtx, fn: func(any) {}})
		hub.sent(attachCommand(7))
		hub.received(&cdproto.Message{ID: 7, Error: &cdproto.Error{Code: -32000, Message: "No target with given id found"}})
		hub.pend(b, &eventListener{ctx: ctx, fn: func(any) {}}) // returns at once

		abandon()
		gomega.Eventually(func() map[any]*pendingAttach {
			hub.mu.Lock()
			defer hub.mu.Unlock()
			return maps.Clone(hub.pending)
		}).Should(gomega.And(gomega.HaveLen(1), gomega.HaveKey(b)))
	})

	ginkgo.It("keeps an event the reader receives during the confirmation's replay behind the replayed ones", func() {
		var lock sync.Mutex
		var seen []string
		record := func(name string) {
			lock.Lock()
			defer lock.Unlock()
			seen = append(seen, name)
		}
		hub.pend(a, &eventListener{ctx: ctx, fn: func(ev any) {
			record(methodOf(ev))
			if methodOf(ev) == "cleared" {
				// the reader goroutine receives the next event while the replay is under way
				done := make(chan struct{})
				go func() {
					hub.received(event("S1", inspector.TargetCrashed.Method, `{}`))
					close(done)
				}()
				<-done
			}
		}})
		hub.sent(runtimeEnable("S1"))
		hub.received(event("S1", runtime.ExecutionContextsCleared.Method, `{}`))

		hub.confirm(a, "S1")
		hub.received(detached("F"))

		lock.Lock()
		defer lock.Unlock()
		gomega.Expect(seen).To(gomega.Equal([]string{"cleared", "crashed", "detached"}))
	})

	ginkgo.It("still calls the raw and later listeners of an event that does not decode, and says so when debugging", func() {
		var reports []string
		hub = newEventHub(func(format string, args ...any) { reports = append(reports, format) })
		var seen []string
		hub.add("S1", &eventListener{ctx: ctx, fn: func(ev any) { seen = append(seen, "decoded") }})
		hub.add("S1", &eventListener{ctx: ctx, raw: func(string, jsonv2.Value) { seen = append(seen, "raw") }})

		hub.received(event("S1", page.FrameDetached.Method, `{"frameId":42}`))

		gomega.Expect(seen).To(gomega.Equal([]string{"raw"}))
		gomega.Expect(reports).To(gomega.ConsistOf(gomega.ContainSubstring("could not decode")))
	})

	ginkgo.It("tells a listener its session ended, when Chrome detaches it or the connection is lost", func() {
		var ended []string
		hub.add("S1", &eventListener{ctx: ctx, fn: func(any) {}, ended: func() { ended = append(ended, "S1") }})
		hub.add("S2", &eventListener{ctx: ctx, fn: func(any) {}, ended: func() { ended = append(ended, "S2") }})

		hub.received(event("", target.DetachedFromTarget.Method, `{"sessionId":"S1","targetId":"T1"}`))
		gomega.Expect(ended).To(gomega.Equal([]string{"S1"}))
		hub.connectionLost()
		gomega.Expect(ended).To(gomega.Equal([]string{"S1", "S2"}))
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

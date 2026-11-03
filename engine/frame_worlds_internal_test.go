package engine

import (
	"context"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// The hub keeps every session's frame-world registry from the session's first message, so nothing has
// to ask Chrome to announce the contexts a target already had when it was attached.
var _ = ginkgo.Describe("the hub's frame-world registries", ginkgo.Label("no-browser"), func() {
	var hub *eventHub

	ginkgo.BeforeEach(func() {
		hub = newEventHub(nil)
	})

	event := func(session target.SessionID, method, params string) *cdproto.Message {
		return &cdproto.Message{SessionID: session, Method: cdproto.MethodType(method), Params: jsonv2.Value(params)}
	}
	created := func(session target.SessionID, frame string, id int, unique string, isDefault bool) *cdproto.Message {
		def := "false"
		if isDefault {
			def = "true"
		}
		return event(session, runtime.ExecutionContextCreated.Method,
			`{"context":{"id":`+jsonv2Int(id)+`,"origin":"","name":"","uniqueId":"`+unique+`","auxData":{"isDefault":`+def+`,"type":"default","frameId":"`+frame+`"}}}`)
	}
	destroyed := func(session target.SessionID, id int, unique string) *cdproto.Message {
		return event(session, runtime.ExecutionContextDestroyed.Method, `{"executionContextId":`+jsonv2Int(id)+`,"executionContextUniqueId":"`+unique+`"}`)
	}
	world := func(session target.SessionID, frame string) frameWorld {
		worlds := hub.worldsFor(session)
		worlds.mu.RLock()
		defer worlds.mu.RUnlock()
		return worlds.byFrame[cdp.FrameID(frame)]
	}
	tracked := func(session target.SessionID) bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		_, ok := hub.worlds[session]
		return ok
	}

	ginkgo.It("records each frame's page world from the session's first message, before the listeners run", func() {
		seenByListener := frameWorld{}
		hub.add("S1", &eventListener{ctx: context.Background(), raw: func(string, jsonv2.Value) {
			seenByListener = world("S1", "F1")
		}, methods: []string{runtime.ExecutionContextCreated.Method}})

		hub.dispatch(created("S1", "F1", 7, "u-7", true))
		gomega.Expect(seenByListener).To(gomega.Equal(frameWorld{id: 7, uniqueID: "u-7"}), "a listener sees the world its own event announced")

		hub.dispatch(created("S1", "F1", 8, "u-8", false))
		gomega.Expect(world("S1", "F1")).To(gomega.Equal(frameWorld{id: 7, uniqueID: "u-7"}), "an isolated world is not the frame's page world")

		hub.dispatch(created("S1", "F2", 9, "u-9", true))
		hub.dispatch(destroyed("S1", 7, "u-7"))
		gomega.Expect(world("S1", "F1")).To(gomega.BeZero())
		gomega.Expect(world("S1", "F2")).To(gomega.Equal(frameWorld{id: 9, uniqueID: "u-9"}))

		hub.dispatch(event("S1", runtime.ExecutionContextsCleared.Method, `{}`))
		gomega.Expect(world("S1", "F2")).To(gomega.BeZero())
	})

	ginkgo.It("keeps two sessions on one target apart, so overlapping attaches each get their own", func() {
		hub.dispatch(created("S1", "F1", 7, "u-7", true))
		hub.dispatch(created("S2", "F1", 3, "u-7", true))
		gomega.Expect(world("S1", "F1")).To(gomega.Equal(frameWorld{id: 7, uniqueID: "u-7"}))
		gomega.Expect(world("S2", "F1")).To(gomega.Equal(frameWorld{id: 3, uniqueID: "u-7"}), "each session's context ids are its own")

		hub.sessionEnded("S1")
		gomega.Expect(tracked("S1")).To(gomega.BeFalse())
		gomega.Expect(world("S2", "F1")).To(gomega.Equal(frameWorld{id: 3, uniqueID: "u-7"}), "the other attach keeps its registry")
	})

	ginkgo.It("drops a session's registry when the session ends, and every registry when the connection is lost", func() {
		hub.dispatch(created("S1", "F1", 7, "u-7", true))
		hub.dispatch(created("S2", "F1", 3, "u-3", true))
		hub.sessionEnded("S1")
		gomega.Expect(tracked("S1")).To(gomega.BeFalse())
		gomega.Expect(tracked("S2")).To(gomega.BeTrue())

		hub.connectionLost()
		gomega.Expect(tracked("S2")).To(gomega.BeFalse())
	})

	ginkgo.It("makes no registry for the browser's own events, or for a session's other events", func() {
		hub.dispatch(event("", runtime.ExecutionContextsCleared.Method, `{}`))
		hub.dispatch(event("S1", "Page.frameNavigated", `{}`))
		gomega.Expect(hub.worlds).To(gomega.BeEmpty())
	})
})

func jsonv2Int(n int) string {
	out, _ := jsonv2.Marshal(n)
	return string(out)
}

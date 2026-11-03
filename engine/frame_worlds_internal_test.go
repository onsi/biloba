package engine

import (
	"context"
	"io"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// scriptedConnection is a Transport whose reads are the messages a spec queues and whose writes it
// records; reading past the queue is the connection failing.
type scriptedConnection struct {
	messages []*cdproto.Message
	written  []*cdproto.Message
}

func (c *scriptedConnection) Read(_ context.Context, msg *cdproto.Message) error {
	if len(c.messages) == 0 {
		return io.EOF
	}
	*msg = *c.messages[0]
	c.messages = c.messages[1:]
	return nil
}
func (c *scriptedConnection) Write(_ context.Context, msg *cdproto.Message) error {
	c.written = append(c.written, msg)
	return nil
}
func (c *scriptedConnection) Close() error { return nil }

func protocolEvent(session target.SessionID, method, params string) *cdproto.Message {
	return &cdproto.Message{SessionID: session, Method: cdproto.MethodType(method), Params: jsonv2.Value(params)}
}

func contextCreated(session target.SessionID, frame string, id int, unique string, isDefault bool) *cdproto.Message {
	def := "false"
	if isDefault {
		def = "true"
	}
	return protocolEvent(session, runtime.ExecutionContextCreated.Method,
		`{"context":{"id":`+jsonv2Int(id)+`,"origin":"","name":"","uniqueId":"`+unique+`","auxData":{"isDefault":`+def+`,"type":"default","frameId":"`+frame+`"}}}`)
}

func contextDestroyed(session target.SessionID, id int, unique string) *cdproto.Message {
	return protocolEvent(session, runtime.ExecutionContextDestroyed.Method, `{"executionContextId":`+jsonv2Int(id)+`,"executionContextUniqueId":"`+unique+`"}`)
}

// The connection records each session's frame worlds from the session's first message, so nothing has
// to ask Chrome to announce the contexts a target already had when it was attached.
var _ = ginkgo.Describe("the connection's frame-world records", ginkgo.Label("no-browser"), func() {
	var script *scriptedConnection
	var conn *connection

	ginkgo.BeforeEach(func() {
		script = &scriptedConnection{}
		conn = newConnection(script, nil)
	})

	// read hands the messages to the connection the way chromedp's reader would
	read := func(messages ...*cdproto.Message) {
		script.messages = append(script.messages, messages...)
		for range messages {
			var msg cdproto.Message
			gomega.ExpectWithOffset(1, conn.Read(context.Background(), &msg)).To(gomega.Succeed())
		}
	}
	world := func(session target.SessionID, frame string) frameWorld {
		conn.mu.Lock()
		defer conn.mu.Unlock()
		if entry := conn.sessions[session]; entry != nil {
			return entry.seen[cdp.FrameID(frame)]
		}
		return frameWorld{}
	}
	recorded := func(session target.SessionID) bool {
		conn.mu.Lock()
		defer conn.mu.Unlock()
		_, ok := conn.sessions[session]
		return ok
	}

	ginkgo.It("records each frame's page world as the reader reads it", func() {
		read(contextCreated("S1", "F1", 7, "u-7", true))
		gomega.Expect(world("S1", "F1")).To(gomega.Equal(frameWorld{id: 7, uniqueID: "u-7"}))

		read(contextCreated("S1", "F1", 8, "u-8", false))
		gomega.Expect(world("S1", "F1")).To(gomega.Equal(frameWorld{id: 7, uniqueID: "u-7"}), "an isolated world is not the frame's page world")

		read(contextCreated("S1", "F2", 9, "u-9", true), contextDestroyed("S1", 7, "u-7"))
		gomega.Expect(world("S1", "F1")).To(gomega.BeZero())
		gomega.Expect(world("S1", "F2")).To(gomega.Equal(frameWorld{id: 9, uniqueID: "u-9"}))

		read(protocolEvent("S1", runtime.ExecutionContextsCleared.Method, `{}`))
		gomega.Expect(world("S1", "F2")).To(gomega.BeZero())
	})

	ginkgo.It("keeps two sessions on one target apart, so overlapping attaches each get their own", func() {
		read(contextCreated("S1", "F1", 7, "u-7", true), contextCreated("S2", "F1", 3, "u-7", true))
		gomega.Expect(world("S1", "F1")).To(gomega.Equal(frameWorld{id: 7, uniqueID: "u-7"}))
		gomega.Expect(world("S2", "F1")).To(gomega.Equal(frameWorld{id: 3, uniqueID: "u-7"}), "each session's context ids are its own")
	})

	ginkgo.It("drops a session's record when Chrome detaches it, on the browser's session or under another", func() {
		read(contextCreated("S1", "F1", 7, "u-7", true), contextCreated("S2", "F1", 3, "u-3", true), contextCreated("S3", "F1", 4, "u-4", true))
		read(protocolEvent("", target.DetachedFromTarget.Method, `{"sessionId":"S1","targetId":"T1"}`))
		gomega.Expect(recorded("S1")).To(gomega.BeFalse())
		gomega.Expect(world("S2", "F1")).To(gomega.Equal(frameWorld{id: 3, uniqueID: "u-3"}), "the other session keeps its record")

		// a session Chrome auto-attached under S3 is detached on S3
		read(protocolEvent("S3", target.DetachedFromTarget.Method, `{"sessionId":"S2","targetId":"T2"}`))
		gomega.Expect(recorded("S2")).To(gomega.BeFalse())
		gomega.Expect(recorded("S3")).To(gomega.BeTrue())
	})

	ginkgo.It("makes no record for the browser's own events, a session's other events, or a context going away", func() {
		read(protocolEvent("", runtime.ExecutionContextsCleared.Method, `{}`), protocolEvent("S1", "Page.frameNavigated", `{}`))
		read(contextDestroyed("S2", 7, "u-7"), protocolEvent("S3", runtime.ExecutionContextsCleared.Method, `{}`))
		gomega.Expect(conn.sessions).To(gomega.BeEmpty())
	})

	ginkgo.It("hands chromedp every message it reads, unchanged", func() {
		messages := []*cdproto.Message{contextCreated("S1", "F1", 7, "u-7", true), {ID: 3, SessionID: "S1", Result: jsonv2.Value(`{}`)}}
		script.messages = append(script.messages, messages...)
		for _, sent := range messages {
			var msg cdproto.Message
			gomega.Expect(conn.Read(context.Background(), &msg)).To(gomega.Succeed())
			gomega.Expect(msg).To(gomega.Equal(*sent))
		}
		var msg cdproto.Message
		gomega.Expect(conn.Read(context.Background(), &msg)).To(gomega.MatchError(io.EOF), "and reports the connection failing")
	})
})

// A registry's listener applies the same events to it, in the order its target hands them over.
var _ = ginkgo.Describe("a frame-world registry", ginkgo.Label("no-browser"), func() {
	decode := func(msg *cdproto.Message) any {
		ev, err := cdproto.UnmarshalMessage(msg, chromedp.DefaultUnmarshalOptions)
		gomega.ExpectWithOffset(1, err).NotTo(gomega.HaveOccurred())
		return ev
	}

	ginkgo.It("follows its target's context events and ignores the rest", func() {
		registry := newFrameWorlds()
		registry.apply(decode(contextCreated("S1", "F1", 7, "u-7", true)))
		registry.apply(decode(protocolEvent("S1", inspector.TargetCrashed.Method, `{}`)))
		gomega.Expect(registry.byFrame).To(gomega.Equal(map[cdp.FrameID]frameWorld{"F1": {id: 7, uniqueID: "u-7"}}))

		// applying an event the seed already holds again leaves it as it was
		registry.apply(decode(contextCreated("S1", "F1", 7, "u-7", true)))
		gomega.Expect(registry.byFrame).To(gomega.Equal(map[cdp.FrameID]frameWorld{"F1": {id: 7, uniqueID: "u-7"}}))

		registry.apply(decode(contextDestroyed("S1", 7, "u-7")))
		gomega.Expect(registry.byFrame).To(gomega.BeEmpty())
	})
})

func jsonv2Int(n int) string {
	out, _ := jsonv2.Marshal(n)
	return string(out)
}

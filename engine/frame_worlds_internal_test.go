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

var _ = ginkgo.Describe("a frame-world registry", ginkgo.Label("no-browser"), func() {
	decode := func(msg *cdproto.Message) any {
		ev, err := cdproto.UnmarshalMessage(msg, chromedp.DefaultUnmarshalOptions)
		gomega.ExpectWithOffset(1, err).NotTo(gomega.HaveOccurred())
		return ev
	}

	ginkgo.It("follows each frame's page world through its target's context events and ignores the rest", func() {
		registry := newFrameWorlds()
		registry.apply(decode(contextCreated("S1", "F1", 7, "u-7", true)))
		registry.apply(decode(protocolEvent("S1", inspector.TargetCrashed.Method, `{}`)))
		gomega.Expect(registry.byFrame).To(gomega.Equal(map[cdp.FrameID]frameWorld{"F1": {id: 7, uniqueID: "u-7"}}))

		registry.apply(decode(contextCreated("S1", "F1", 8, "u-8", false)))
		gomega.Expect(registry.byFrame).To(gomega.Equal(map[cdp.FrameID]frameWorld{"F1": {id: 7, uniqueID: "u-7"}}), "an isolated world is not the frame's page world")

		registry.apply(decode(contextCreated("S1", "F2", 9, "u-9", true)))
		registry.apply(decode(contextDestroyed("S1", 7, "u-7")))
		gomega.Expect(registry.byFrame).To(gomega.Equal(map[cdp.FrameID]frameWorld{"F2": {id: 9, uniqueID: "u-9"}}))

		registry.apply(decode(protocolEvent("S1", runtime.ExecutionContextsCleared.Method, `{}`)))
		gomega.Expect(registry.byFrame).To(gomega.BeEmpty())
	})
})

func jsonv2Int(n int) string {
	out, _ := jsonv2.Marshal(n)
	return string(out)
}

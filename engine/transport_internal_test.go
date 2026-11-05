package engine

import (
	"context"
	"time"

	"github.com/chromedp/cdproto"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/target"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("a connection's writes", ginkgo.Label("no-browser"), func() {
	ginkgo.It("lets a hook see every command, hold some back, and send them later in order", func() {
		script := &scriptedConnection{}
		conn := newConnection(script, nil)
		command := func(id int64, session target.SessionID) *cdproto.Message {
			return &cdproto.Message{ID: id, SessionID: session, Method: "Runtime.evaluate"}
		}
		var seen []int64
		var held []*cdproto.Message
		hook := func(_ *connection, msg *cdproto.Message) bool {
			seen = append(seen, msg.ID)
			if msg.SessionID == "S2" {
				held = append(held, msg)
				return true
			}
			return false
		}
		previous := writeHook.Swap(&hook)
		ginkgo.DeferCleanup(func() { writeHook.Store(previous) })

		for _, msg := range []*cdproto.Message{command(1, "S1"), command(2, "S2"), command(3, "S1"), command(4, "S2")} {
			gomega.Expect(conn.Write(context.Background(), msg)).To(gomega.Succeed())
		}
		gomega.Expect(seen).To(gomega.Equal([]int64{1, 2, 3, 4}))
		written := func() []int64 {
			var ids []int64
			for _, msg := range script.written {
				ids = append(ids, msg.ID)
			}
			return ids
		}
		gomega.Expect(written()).To(gomega.Equal([]int64{1, 3}))

		conn.sendHeld(context.Background(), func() []*cdproto.Message { return held })
		gomega.Expect(written()).To(gomega.Equal([]int64{1, 3, 2, 4}))
	})
})

var _ = ginkgo.Describe("a session that detaches", ginkgo.Label("no-browser"), func() {
	readAll := func(conn *connection) []cdproto.Message {
		var got []cdproto.Message
		for {
			var msg cdproto.Message
			if conn.Read(context.Background(), &msg) != nil {
				return got
			}
			got = append(got, msg)
		}
	}

	ginkgo.It("has a barrier between Chrome's own last answer and the detach", func() {
		script := &scriptedConnection{}
		conn := newConnection(script, nil)
		answer := &cdproto.Message{ID: 7, SessionID: "S1", Result: jsonv2.Value(`{}`)}
		detach := protocolEvent("", target.DetachedFromTarget.Method, `{"sessionId":"S1","targetId":"T1"}`)
		later := &cdproto.Message{ID: 10, SessionID: "S2", Result: jsonv2.Value(`{}`)}
		script.messages = []*cdproto.Message{answer, detach, later}
		gomega.Expect(readAll(conn)).To(gomega.Equal([]cdproto.Message{
			*answer,
			{ID: barrierMessageID, SessionID: "S1", Result: jsonv2.Value(`{}`)},
			*detach,
			*later,
		}), "chromedp's routing loop could otherwise take the detach ahead of the answer and drop the answer")
	})

	ginkgo.It("has no barrier for a session Chrome detaches under another, which chromedp does not drive", func() {
		script := &scriptedConnection{}
		conn := newConnection(script, nil)
		nested := protocolEvent("S1", target.DetachedFromTarget.Method, `{"sessionId":"S2","targetId":"T2"}`)
		script.messages = []*cdproto.Message{nested}
		gomega.Expect(readAll(conn)).To(gomega.Equal([]cdproto.Message{*nested}))
	})
})

var _ = ginkgo.Describe("the sessions a connection saw detach", ginkgo.Label("no-browser"), func() {
	ginkgo.It("are remembered as the reader reads the detach, the most recent of them only", func() {
		script := &scriptedConnection{}
		conn := newConnection(script, nil)
		for i := range rememberedDetachedSessions + 1 {
			script.messages = append(script.messages, protocolEvent("", target.DetachedFromTarget.Method, `{"sessionId":"S`+jsonv2Int(i)+`","targetId":"T"}`))
		}
		// nested: a session Chrome auto-attached under another, which chromedp does not drive
		script.messages = append(script.messages, protocolEvent("S1", target.DetachedFromTarget.Method, `{"sessionId":"NESTED","targetId":"T"}`))
		for {
			var msg cdproto.Message
			if conn.Read(context.Background(), &msg) != nil {
				break
			}
		}
		gomega.Expect(conn.sessionDetached("S0")).To(gomega.BeFalse(), "the oldest is forgotten")
		gomega.Expect(conn.sessionDetached("S1")).To(gomega.BeTrue())
		gomega.Expect(conn.sessionDetached(target.SessionID("S" + jsonv2Int(rememberedDetachedSessions)))).To(gomega.BeTrue())
		gomega.Expect(conn.sessionDetached("NESTED")).To(gomega.BeFalse())
		gomega.Expect(conn.detached).To(gomega.HaveLen(rememberedDetachedSessions))
	})
})

var _ = ginkgo.Describe("timing listeners", ginkgo.Label("no-browser"), func() {
	ginkgo.It("reports a listener that holds its target's messages too long, when debugging is on", func() {
		var reports []string
		conn := newConnection(&scriptedConnection{}, func(format string, args ...any) { reports = append(reports, format) })
		conn.timed(func(any) {})("fast")
		conn.timed(func(any) { time.Sleep(slowListenerThreshold + 20*time.Millisecond) })("slow")
		gomega.Expect(reports).To(gomega.ConsistOf(gomega.ContainSubstring("held its target's CDP messages")))
	})
})

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

var _ = ginkgo.Describe("a session that detaches with commands unanswered", ginkgo.Label("no-browser"), func() {
	ginkgo.It("has them answered with Chrome's target-gone error, then a barrier, before chromedp sees the detach", func() {
		script := &scriptedConnection{}
		conn := newConnection(script, nil)
		for _, msg := range []*cdproto.Message{
			{ID: 7, SessionID: "S1", Method: "Runtime.evaluate"},
			{ID: 8, SessionID: "S1", Method: "Runtime.evaluate"},
			{ID: 9, SessionID: "S1", Method: "Page.reload"},
			{ID: 10, SessionID: "S2", Method: "Runtime.evaluate"},
			{ID: 11, Method: "Target.getTargets"},
		} {
			gomega.Expect(conn.Write(context.Background(), msg)).To(gomega.Succeed())
		}
		detach := protocolEvent("", target.DetachedFromTarget.Method, `{"sessionId":"S1","targetId":"T1"}`)
		script.messages = []*cdproto.Message{
			{ID: 8, SessionID: "S1", Result: jsonv2.Value(`{}`)}, // answered before the detach
			detach,
			{ID: 10, SessionID: "S2", Result: jsonv2.Value(`{}`)},
		}
		var got []cdproto.Message
		for {
			var msg cdproto.Message
			if conn.Read(context.Background(), &msg) != nil {
				break
			}
			got = append(got, msg)
		}
		gone := detachedSessionGoneError
		gomega.Expect(got).To(gomega.Equal([]cdproto.Message{
			{ID: 8, SessionID: "S1", Result: jsonv2.Value(`{}`)},
			{ID: 7, SessionID: "S1", Error: &gone},
			{ID: 9, SessionID: "S1", Error: &gone},
			{ID: barrierMessageID, SessionID: "S1", Result: jsonv2.Value(`{}`)},
			*detach,
			{ID: 10, SessionID: "S2", Result: jsonv2.Value(`{}`)},
		}))
		gomega.Expect(conn.unanswered).To(gomega.BeEmpty(), "what was answered, and the detached session, are forgotten")
	})

	ginkgo.It("puts a barrier between Chrome's own last answer and the detach, though nothing is left unanswered", func() {
		script := &scriptedConnection{}
		conn := newConnection(script, nil)
		gomega.Expect(conn.Write(context.Background(), &cdproto.Message{ID: 7, SessionID: "S1", Method: "Runtime.evaluate"})).To(gomega.Succeed())
		answer := &cdproto.Message{ID: 7, SessionID: "S1", Result: jsonv2.Value(`{}`)}
		detach := protocolEvent("", target.DetachedFromTarget.Method, `{"sessionId":"S1","targetId":"T1"}`)
		script.messages = []*cdproto.Message{answer, detach}
		var got []cdproto.Message
		for {
			var msg cdproto.Message
			if conn.Read(context.Background(), &msg) != nil {
				break
			}
			got = append(got, msg)
		}
		gomega.Expect(got).To(gomega.Equal([]cdproto.Message{
			*answer,
			{ID: barrierMessageID, SessionID: "S1", Result: jsonv2.Value(`{}`)},
			*detach,
		}), "chromedp's routing loop could otherwise take the detach ahead of the answer and drop the answer")
	})

	ginkgo.It("keeps a bounded number of the commands nobody will hear back about, dropping the oldest", func() {
		conn := newConnection(&scriptedConnection{}, nil)
		for id := int64(1); id <= maxUnanswered+1; id++ {
			gomega.Expect(conn.Write(context.Background(), &cdproto.Message{ID: id, SessionID: "S1", Method: "Runtime.evaluate"})).To(gomega.Succeed())
		}
		pending := conn.unanswered["S1"]
		gomega.Expect(len(pending)).To(gomega.BeNumerically("<=", maxUnanswered))
		gomega.Expect(pending).To(gomega.HaveKey(int64(maxUnanswered+1)), "the newest stay")
		gomega.Expect(pending).NotTo(gomega.HaveKey(int64(1)), "the oldest go")
	})

	ginkgo.It("answers nothing for a session Chrome detaches under another, which chromedp does not drive", func() {
		script := &scriptedConnection{}
		conn := newConnection(script, nil)
		gomega.Expect(conn.Write(context.Background(), &cdproto.Message{ID: 7, SessionID: "S2", Method: "Runtime.evaluate"})).To(gomega.Succeed())
		nested := protocolEvent("S1", target.DetachedFromTarget.Method, `{"sessionId":"S2","targetId":"T2"}`)
		script.messages = []*cdproto.Message{nested}
		var msg cdproto.Message
		gomega.Expect(conn.Read(context.Background(), &msg)).To(gomega.Succeed())
		gomega.Expect(msg).To(gomega.Equal(*nested))
		gomega.Expect(conn.Read(context.Background(), &msg)).NotTo(gomega.Succeed())
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

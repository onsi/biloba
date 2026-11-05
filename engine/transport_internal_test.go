package engine

import (
	"context"
	"time"

	"github.com/chromedp/cdproto"
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

var _ = ginkgo.Describe("timing listeners", ginkgo.Label("no-browser"), func() {
	ginkgo.It("reports a listener that holds its target's messages too long, when debugging is on", func() {
		var reports []string
		conn := newConnection(&scriptedConnection{}, func(format string, args ...any) { reports = append(reports, format) })
		conn.timed(func(any) {})("fast")
		conn.timed(func(any) { time.Sleep(slowListenerThreshold + 20*time.Millisecond) })("slow")
		gomega.Expect(reports).To(gomega.ConsistOf(gomega.ContainSubstring("held its target's CDP messages")))
	})
})

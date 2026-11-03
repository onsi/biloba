package engine

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/chromedp/cdproto"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/target"
	ginkgo "github.com/onsi/ginkgo/v2"
	gomega "github.com/onsi/gomega"
)

// scriptedConnection is a Transport whose reads are the messages a spec feeds it.
type scriptedConnection struct {
	messages chan *cdproto.Message
}

func (c *scriptedConnection) Read(_ context.Context, msg *cdproto.Message) error {
	next, ok := <-c.messages
	if !ok {
		return io.EOF
	}
	*msg = *next
	return nil
}
func (c *scriptedConnection) Write(context.Context, *cdproto.Message) error { return nil }
func (c *scriptedConnection) Close() error                                 { return nil }

// orderedTransport is what chromedp reads through: a session's messages reach chromedp in order, each
// after that session's listeners have run, and a session that stalls holds up no other.
var _ = ginkgo.Describe("orderedTransport", ginkgo.Label("no-browser"), func() {
	var hub *eventHub
	var connection *scriptedConnection
	var transport *orderedTransport
	var ctx context.Context

	reply := func(session target.SessionID, id int64) *cdproto.Message {
		return &cdproto.Message{SessionID: session, ID: id, Result: jsonv2.Value(`{}`)}
	}
	event := func(session target.SessionID, method string) *cdproto.Message {
		return &cdproto.Message{SessionID: session, Method: cdproto.MethodType(method), Params: jsonv2.Value(`{}`)}
	}
	readOne := func() *cdproto.Message {
		var msg cdproto.Message
		gomega.ExpectWithOffset(1, transport.Read(ctx, &msg)).To(gomega.Succeed())
		return &msg
	}

	ginkgo.BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ginkgo.DeferCleanup(cancel)
		hub = newEventHub(nil)
		connection = &scriptedConnection{messages: make(chan *cdproto.Message, 100)}
		transport = newOrderedTransport(connection, hub)
		ginkgo.DeferCleanup(func() { close(connection.messages) })
	})

	ginkgo.It("hands chromedp a session's messages in order, each after its session's listeners ran", func() {
		var lock sync.Mutex
		handled := []string{}
		hub.add("S1", &eventListener{ctx: ctx, fn: func(ev any) {
			lock.Lock()
			defer lock.Unlock()
			handled = append(handled, "crashed")
		}})
		connection.messages <- event("S1", inspector.TargetCrashed.Method)
		connection.messages <- reply("S1", 7)

		first := readOne()
		gomega.Expect(string(first.Method)).To(gomega.Equal(inspector.TargetCrashed.Method))
		second := readOne()
		gomega.Expect(second.ID).To(gomega.Equal(int64(7)))
		lock.Lock()
		defer lock.Unlock()
		gomega.Expect(handled).To(gomega.Equal([]string{"crashed"}), "the event was handled before the reply behind it reached chromedp")
	})

	ginkgo.It("lets other sessions through while one session's listener waits", func() {
		release := make(chan struct{})
		hub.add("S1", &eventListener{ctx: ctx, fn: func(any) { <-release }})
		connection.messages <- event("S1", inspector.TargetCrashed.Method)
		connection.messages <- reply("S1", 7)
		connection.messages <- reply("S2", 8)
		connection.messages <- reply("", 9)

		// different sessions keep no order between them
		gomega.Expect([]int64{readOne().ID, readOne().ID}).To(gomega.ConsistOf(int64(8), int64(9)), "another tab's reply and the browser's are not held up")
		close(release)
		gomega.Expect(string(readOne().Method)).To(gomega.Equal(inspector.TargetCrashed.Method))
		gomega.Expect(readOne().ID).To(gomega.Equal(int64(7)))
	})

	ginkgo.It("forgets a detached session's listeners only once the session's earlier messages are handled", func() {
		release := make(chan struct{})
		var lock sync.Mutex
		var seen []string
		ended := false
		hub.add("S1", &eventListener{ctx: ctx, fn: func(ev any) {
			if _, ok := ev.(*inspector.EventTargetCrashed); ok {
				<-release
			}
			lock.Lock()
			defer lock.Unlock()
			seen = append(seen, string(methodName(ev)))
		}, ended: func() {
			lock.Lock()
			defer lock.Unlock()
			ended = true
		}})
		connection.messages <- event("S1", inspector.TargetCrashed.Method)
		connection.messages <- &cdproto.Message{Method: cdproto.MethodType(target.DetachedFromTarget.Method), Params: jsonv2.Value(`{"sessionId":"S1","targetId":"T1"}`)}

		detached := readOne()
		gomega.Expect(string(detached.Method)).To(gomega.Equal(target.DetachedFromTarget.Method), "the browser's own message is not held up by the session")
		lock.Lock()
		gomega.Expect(ended).To(gomega.BeFalse())
		lock.Unlock()

		close(release)
		gomega.Expect(string(readOne().Method)).To(gomega.Equal(inspector.TargetCrashed.Method))
		gomega.Eventually(func() bool {
			lock.Lock()
			defer lock.Unlock()
			return ended
		}).Should(gomega.BeTrue())
		lock.Lock()
		defer lock.Unlock()
		gomega.Expect(seen).To(gomega.Equal([]string{inspector.TargetCrashed.Method}))
		transport.mu.Lock()
		defer transport.mu.Unlock()
		gomega.Expect(transport.workers).NotTo(gomega.HaveKey(target.SessionID("S1")))
	})

	ginkgo.It("runs a session's worker only while it has work", func() {
		connection.messages <- reply("S1", 7)
		readOne()
		worker := transport.worker("S1")
		gomega.Eventually(func() bool {
			worker.mu.Lock()
			defer worker.mu.Unlock()
			return worker.running
		}).Should(gomega.BeFalse())
	})

	ginkgo.It("reports the connection's failure, and the reader's own cancellation", func() {
		connection.messages <- reply("S1", 7)
		gomega.Expect(readOne().ID).To(gomega.Equal(int64(7)))

		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		var msg cdproto.Message
		gomega.Expect(transport.Read(cancelled, &msg)).To(gomega.MatchError(context.Canceled))

		lost := newOrderedTransport(&scriptedConnection{messages: func() chan *cdproto.Message {
			closed := make(chan *cdproto.Message)
			close(closed)
			return closed
		}()}, hub)
		gomega.Expect(lost.Read(ctx, &msg)).To(gomega.MatchError(io.EOF))
	})
})

// methodName names a decoded event for a spec.
func methodName(ev any) string {
	switch ev.(type) {
	case *inspector.EventTargetCrashed:
		return inspector.TargetCrashed.Method
	case *page.EventFrameDetached:
		return page.FrameDetached.Method
	}
	return "other"
}

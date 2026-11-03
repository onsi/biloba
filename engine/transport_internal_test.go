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

// scriptedConnection is a Transport whose reads are the messages a spec feeds it; closing messages
// is the connection failing.
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
func (c *scriptedConnection) Close() error                                  { return nil }

// orderedTransport is what chromedp reads through: a session's messages reach chromedp in order, each
// after that session's listeners have run; a session that stalls holds up no other; and the browser's
// own listeners run in the order its messages arrive, before anything read after them.
var _ = ginkgo.Describe("orderedTransport", ginkgo.Label("no-browser"), func() {
	var hub *eventHub
	var connection *scriptedConnection
	var transport *orderedTransport
	var ctx context.Context
	var feed func(...*cdproto.Message)
	var failConnection func()

	reply := func(session target.SessionID, id int64) *cdproto.Message {
		return &cdproto.Message{SessionID: session, ID: id, Result: jsonv2.Value(`{}`)}
	}
	event := func(session target.SessionID, method, params string) *cdproto.Message {
		return &cdproto.Message{SessionID: session, Method: cdproto.MethodType(method), Params: jsonv2.Value(params)}
	}
	crashed := func(session target.SessionID) *cdproto.Message {
		return event(session, inspector.TargetCrashed.Method, `{}`)
	}
	detached := func(session string) *cdproto.Message {
		return event("", target.DetachedFromTarget.Method, `{"sessionId":"`+session+`","targetId":"T1"}`)
	}
	// describe names a message the way the specs compare them
	describe := func(msg *cdproto.Message) string {
		if msg.Method != "" {
			return string(msg.SessionID) + " " + string(msg.Method)
		}
		return string(msg.SessionID) + " reply"
	}
	readOne := func() string {
		var msg cdproto.Message
		gomega.ExpectWithOffset(1, transport.Read(ctx, &msg)).To(gomega.Succeed())
		return describe(&msg)
	}
	// reads collects what Read hands chromedp, from a goroutine, so a spec can hold a listener up
	type reading struct {
		lock sync.Mutex
		got  []string
		err  error
	}
	readAll := func() *reading {
		r := &reading{}
		reader, readCtx := transport, ctx // this spec's, not a later one's
		go func() {
			for {
				var msg cdproto.Message
				err := reader.Read(readCtx, &msg)
				r.lock.Lock()
				if err != nil {
					r.err = err
					r.lock.Unlock()
					return
				}
				r.got = append(r.got, describe(&msg))
				r.lock.Unlock()
			}
		}()
		return r
	}
	got := func(r *reading) func() []string {
		return func() []string {
			r.lock.Lock()
			defer r.lock.Unlock()
			return append([]string(nil), r.got...)
		}
	}
	readErr := func(r *reading) func() error {
		return func() error {
			r.lock.Lock()
			defer r.lock.Unlock()
			return r.err
		}
	}
	blocked := func(release chan struct{}) func(any) {
		return func(ev any) {
			if _, ok := ev.(*inspector.EventTargetCrashed); ok {
				<-release
			}
		}
	}

	ginkgo.BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		ginkgo.DeferCleanup(cancel)
		hub = newEventHub(nil)
		connection = &scriptedConnection{messages: make(chan *cdproto.Message, 100)}
		transport = newOrderedTransport(connection, hub)
		var once sync.Once
		failConnection = func() { once.Do(func() { close(connection.messages) }) }
		feed = func(messages ...*cdproto.Message) {
			for _, msg := range messages {
				connection.messages <- msg
			}
		}
		ginkgo.DeferCleanup(func() { failConnection(); _ = transport.Close() })
	})

	ginkgo.It("hands chromedp a session's messages in order, each after its session's listeners ran", func() {
		var lock sync.Mutex
		handled := []string{}
		hub.add("S1", &eventListener{ctx: ctx, fn: func(any) {
			lock.Lock()
			defer lock.Unlock()
			handled = append(handled, "crashed")
		}})
		feed(crashed("S1"), reply("S1", 7))

		gomega.Expect(readOne()).To(gomega.Equal("S1 " + inspector.TargetCrashed.Method))
		gomega.Expect(readOne()).To(gomega.Equal("S1 reply"))
		lock.Lock()
		defer lock.Unlock()
		gomega.Expect(handled).To(gomega.Equal([]string{"crashed"}), "the event was handled before the reply behind it reached chromedp")
	})

	ginkgo.It("lets other sessions through while one session's listener waits", func() {
		release := make(chan struct{})
		hub.add("S1", &eventListener{ctx: ctx, fn: blocked(release)})
		feed(crashed("S1"), reply("S1", 7), reply("S2", 8), reply("", 9))

		// different sessions keep no order between them
		gomega.Expect([]string{readOne(), readOne()}).To(gomega.ConsistOf("S2 reply", " reply"), "another tab's reply and the browser's are not held up")
		close(release)
		gomega.Expect(readOne()).To(gomega.Equal("S1 " + inspector.TargetCrashed.Method))
		gomega.Expect(readOne()).To(gomega.Equal("S1 reply"))
	})

	// Codex round 5, finding 1: the detach used to reach chromedp ahead of the session's queued
	// reply, and chromedp then dropped the reply.
	ginkgo.It("hands chromedp a session's detach only after everything of the session that came before it", func() {
		release := make(chan struct{})
		ended := make(chan struct{})
		hub.add("S1", &eventListener{ctx: ctx, fn: blocked(release), ended: func() { close(ended) }})
		feed(crashed("S1"), reply("S1", 7), detached("S1"), reply("", 9))

		reads := readAll()
		gomega.Eventually(got(reads)).Should(gomega.Equal([]string{" reply"}), "the browser's later reply is not held up by the session")
		gomega.Consistently(got(reads), "50ms").Should(gomega.HaveLen(1), "nor does the detach overtake the session's queued messages")
		gomega.Expect(ended).NotTo(gomega.BeClosed())

		close(release)
		gomega.Eventually(got(reads)).Should(gomega.Equal([]string{" reply", "S1 " + inspector.TargetCrashed.Method, "S1 reply", " " + target.DetachedFromTarget.Method}))
		gomega.Expect(ended).To(gomega.BeClosed(), "the session's listeners are forgotten once its earlier messages are handled")
	})

	// Codex round 5, finding 2: on a failed connection, Read used to report the failure at once,
	// dropping the replies and events already read.
	ginkgo.It("hands chromedp what was already read before reporting the connection's failure", func() {
		release := make(chan struct{})
		hub.add("S1", &eventListener{ctx: ctx, fn: blocked(release)})
		feed(crashed("S1"), reply("S1", 7))
		failConnection()

		reads := readAll()
		gomega.Consistently(readErr(reads), "50ms").Should(gomega.BeNil(), "the failure waits for the session's queued messages")
		close(release)
		gomega.Eventually(readErr(reads)).Should(gomega.MatchError(io.EOF))
		gomega.Expect(got(reads)()).To(gomega.Equal([]string{"S1 " + inspector.TargetCrashed.Method, "S1 reply"}))
	})

	// Codex round 5, finding 3: a message for a session still draining after its detach used to start
	// a second worker, whose listener calls ran alongside the first's.
	ginkgo.It("keeps a message that arrives for a detached session behind the session's queue", func() {
		release := make(chan struct{})
		var lock sync.Mutex
		var calls []string
		running := 0
		overlapped := false
		hub.add("S1", &eventListener{ctx: ctx, fn: func(ev any) {
			lock.Lock()
			running++
			overlapped = overlapped || running > 1
			calls = append(calls, methodName(ev))
			lock.Unlock()
			if _, ok := ev.(*inspector.EventTargetCrashed); ok {
				<-release
			}
			lock.Lock()
			running--
			lock.Unlock()
		}})
		feed(crashed("S1"), detached("S1"), event("S1", page.FrameDetached.Method, `{"frameId":"F","reason":"remove"}`))

		reads := readAll()
		callsSoFar := func() []string {
			lock.Lock()
			defer lock.Unlock()
			return append([]string(nil), calls...)
		}
		gomega.Eventually(callsSoFar).Should(gomega.Equal([]string{inspector.TargetCrashed.Method}))
		gomega.Consistently(callsSoFar, "50ms").Should(gomega.Equal([]string{inspector.TargetCrashed.Method}), "the late message waits behind the queue")
		close(release)
		gomega.Eventually(got(reads)).Should(gomega.Equal([]string{"S1 " + inspector.TargetCrashed.Method, " " + target.DetachedFromTarget.Method, "S1 " + page.FrameDetached.Method}))
		lock.Lock()
		defer lock.Unlock()
		gomega.Expect(overlapped).To(gomega.BeFalse())
		gomega.Expect(calls).To(gomega.Equal([]string{inspector.TargetCrashed.Method}), "and it reaches no listener: the session is gone")
		transport.mu.Lock()
		defer transport.mu.Unlock()
		gomega.Expect(transport.workers).NotTo(gomega.HaveKey(target.SessionID("S1")), "the detached session's worker goes once its queue is empty")
	})

	// Codex round 5, finding 4: the browser's listeners used to run on a worker of their own, so a
	// browser-level crash could be handled after a later navigation of the crashed tab.
	ginkgo.It("runs the browser's listeners before any message read after theirs", func() {
		release := make(chan struct{})
		var lock sync.Mutex
		var order []string
		record := func(what string) {
			lock.Lock()
			defer lock.Unlock()
			order = append(order, what)
		}
		hub.add("", &eventListener{ctx: ctx, fn: func(ev any) {
			switch ev.(type) {
			case *target.EventTargetCreated:
				<-release // a browser listener still busy with an earlier event
			case *target.EventTargetCrashed:
				record("browser: crashed")
			}
		}})
		hub.add("S1", &eventListener{ctx: ctx, fn: func(ev any) {
			if _, ok := ev.(*page.EventFrameNavigated); ok {
				record("tab: navigated")
			}
		}})
		feed(event("", target.TargetCreated.Method, `{"targetInfo":{"targetId":"T2","type":"page","title":"","url":"","attached":false,"canAccessOpener":false}}`),
			event("", target.TargetCrashed.Method, `{"targetId":"T1","status":"crashed","errorCode":0}`),
			event("S1", page.FrameNavigated.Method, `{"frame":{"id":"F","loaderId":"L","url":"about:blank","domainAndRegistry":"","securityOrigin":"","mimeType":"","secureContextType":"Secure","crossOriginIsolatedContextType":"NotIsolated","gatedAPIFeatures":[]},"type":"Navigation"}`))

		reads := readAll()
		gomega.Consistently(func() []string {
			lock.Lock()
			defer lock.Unlock()
			return append([]string(nil), order...)
		}, "50ms").Should(gomega.BeEmpty(), "nothing read after the busy browser listener's event is handled yet")
		close(release)
		gomega.Eventually(got(reads)).Should(gomega.HaveLen(3))
		lock.Lock()
		defer lock.Unlock()
		gomega.Expect(order).To(gomega.Equal([]string{"browser: crashed", "tab: navigated"}))
	})

	ginkgo.It("runs a session's worker only while it has work", func() {
		feed(reply("S1", 7))
		readOne()
		gomega.Eventually(func() bool {
			transport.mu.Lock()
			defer transport.mu.Unlock()
			worker := transport.workers["S1"]
			return worker != nil && !worker.running && transport.pending == 0
		}).Should(gomega.BeTrue())
	})

	ginkgo.It("reports the reader's own cancellation", func() {
		feed(reply("S1", 7))
		gomega.Expect(readOne()).To(gomega.Equal("S1 reply"))
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		var msg cdproto.Message
		gomega.Expect(transport.Read(cancelled, &msg)).To(gomega.MatchError(context.Canceled))
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

package engine

import (
	"context"
	"sync"
	"time"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

/*
orderedTransport runs the hub's listeners for every message read from Chrome before chromedp sees it,
in the shape chromedp's own Browser.run had before v0.17: the browser's own messages are handled on
the reader, in the order they arrive, and each flattened session's on a worker of its own.

  - A session's messages reach chromedp in the order Chrome sent them, each after that session's
    listeners have run, so its earlier events are recorded before chromedp acts on a reply or event
    behind them.
  - A listener that waits holds up only its own session.  The browser's listeners run on the reader,
    so they must not wait on anything (Biloba's only set flags or hand off to a goroutine).
  - A browser-level listener runs before any later message of any session is handled.

chromedp routes a session's messages to that session's target and depends on nothing ordered across
sessions but one thing: once it sees the browser-level Target.detachedFromTarget for a session it
drops whatever comes for that session afterwards.  So that message reaches chromedp from the end of
the session's own queue, after the session's earlier messages (the hub forgets the session's
listeners there too), while the browser's later messages go on without it.  A message that still
arrives for the session joins the same queue behind it.

When the connection fails, what was already read is handed over before Read reports the failure,
waiting at most connectionDrainTimeout for listeners still running.

Everything here exists because chromedp keeps its ordered listener (Target.listen) unexported; with
an exported one, the hub could register there and this transport could go.
*/
type orderedTransport struct {
	chromedp.Transport
	hub *eventHub

	start sync.Once
	// out carries handled messages to Read.
	out chan *cdproto.Message
	// ended is closed once the connection's read has failed and what it read has been handed over (or
	// connectionDrainTimeout passed); readErr is the failure, valid once ended is closed.
	ended   chan struct{}
	readErr error
	// closing is closed by Close: nothing will Read any more.
	closing   chan struct{}
	closeOnce sync.Once

	// mu guards workers and every sessionWorker's fields, and pending: the work queued and not yet done.
	mu      sync.Mutex
	workers map[target.SessionID]*sessionWorker
	pending int
}

// connectionDrainTimeout bounds how long a failed connection waits for its sessions' queued
// messages: a listener can be waiting on a reply that will never come now.
const connectionDrainTimeout = 2 * time.Second

// sessionWorker is one session's queue.  Its goroutine runs only while the queue has work, so an
// idle session holds none.  The queue has no bound: the reader never waits on a session, and a
// listener that stalls holds up only its own session's messages.
type sessionWorker struct {
	queue    []func()
	running  bool
	detached bool // Chrome detached the session; the worker goes once its queue is empty
}

func newOrderedTransport(tr chromedp.Transport, hub *eventHub) *orderedTransport {
	return &orderedTransport{
		Transport: tr, hub: hub,
		out: make(chan *cdproto.Message), ended: make(chan struct{}), closing: make(chan struct{}),
		workers: map[target.SessionID]*sessionWorker{},
	}
}

// Read returns the next message whose listeners have run.  chromedp calls it from one goroutine;
// the connection itself is read by the transport's own.
func (t *orderedTransport) Read(ctx context.Context, msg *cdproto.Message) error {
	t.start.Do(func() { go t.readConnection() })
	select {
	case handled := <-t.out:
		*msg = *handled
		return nil
	case <-t.ended:
		return t.readErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *orderedTransport) Close() error {
	t.closeOnce.Do(func() { close(t.closing) })
	return t.Transport.Close()
}

func (t *orderedTransport) readConnection() {
	for {
		msg := new(cdproto.Message)
		if err := t.Transport.Read(context.Background(), msg); err != nil {
			t.drain()
			t.readErr = err
			close(t.ended)
			return
		}
		if msg.SessionID != "" {
			t.enqueue(msg.SessionID, func() {
				t.hub.dispatch(msg)
				t.release(msg)
			})
			continue
		}
		// the browser's own message: its listeners run here, before anything read after it
		t.hub.dispatch(msg)
		if session := detachedSession(msg); session != "" {
			if t.enqueueDetach(session, func() {
				t.hub.sessionEnded(session)
				t.release(msg)
			}) {
				continue
			}
			t.hub.sessionEnded(session) // nothing of the session's is queued
		}
		t.release(msg)
	}
}

// release hands msg to Read, unless nothing will read it any more.
func (t *orderedTransport) release(msg *cdproto.Message) {
	select {
	case t.out <- msg:
	case <-t.ended:
	case <-t.closing:
	}
}

// enqueue adds work to session's queue, starting its worker when it is idle.
func (t *orderedTransport) enqueue(session target.SessionID, work func()) {
	t.mu.Lock()
	worker := t.workers[session]
	if worker == nil {
		worker = &sessionWorker{}
		t.workers[session] = worker
	}
	t.pushLocked(session, worker, work)
	t.mu.Unlock()
}

// enqueueDetach queues work at the end of session's queue and marks the session detached, so that its
// worker goes once the queue is empty.  It reports false, queueing nothing, when the session has no
// worker: nothing of it is waiting.
func (t *orderedTransport) enqueueDetach(session target.SessionID, work func()) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	worker := t.workers[session]
	if worker == nil {
		return false
	}
	worker.detached = true
	t.pushLocked(session, worker, work)
	return true
}

func (t *orderedTransport) pushLocked(session target.SessionID, worker *sessionWorker, work func()) {
	worker.queue = append(worker.queue, work)
	t.pending++
	if !worker.running {
		worker.running = true
		go t.run(session, worker)
	}
}

func (t *orderedTransport) run(session target.SessionID, worker *sessionWorker) {
	for {
		t.mu.Lock()
		if len(worker.queue) == 0 {
			worker.running = false
			if worker.detached && t.workers[session] == worker {
				delete(t.workers, session)
			}
			t.mu.Unlock()
			return
		}
		work := worker.queue[0]
		worker.queue[0] = nil
		worker.queue = worker.queue[1:]
		t.mu.Unlock()
		work()
		t.mu.Lock()
		t.pending--
		t.mu.Unlock()
	}
}

// drain waits, for at most connectionDrainTimeout, until every queued message has been handed over.
func (t *orderedTransport) drain() {
	deadline := time.Now().Add(connectionDrainTimeout)
	for time.Now().Before(deadline) {
		t.mu.Lock()
		pending := t.pending
		t.mu.Unlock()
		if pending == 0 {
			return
		}
		select {
		case <-t.closing:
			return
		case <-time.After(time.Millisecond):
		}
	}
}

// SetDebugf passes chromedp's protocol logger through to the connection, which is what writes it.
func (t *orderedTransport) SetDebugf(f func(string, ...any)) {
	if s, ok := t.Transport.(interface{ SetDebugf(func(string, ...any)) }); ok {
		s.SetDebugf(f)
	}
}

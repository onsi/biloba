package engine

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/chromedp/cdproto"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

/*
Biloba needs every event a target raises in the order Chrome sent it, across event types: the
Prepare boundary, the in-flight tracker, and the frame-world registry all depend on it.  chromedp
offers that only through an unexported listener (its own Navigate uses one); its exported
Events/Subscribe give one queue per method, each drained by a goroutine of its own, so neither the
order across methods nor the timing below survives them.

So Biloba listens on the connection.  An orderedTransport wraps the websocket and runs the
connection's eventHub on every message it reads, before chromedp sees it, on one worker per session
(see orderedTransport):

  - listeners see their session's events in wire order, across methods;
  - every event Chrome sent on a session before a command's reply, or before the load event Navigate
    waits for, has been handled by the time chromedp acts on that later message, so a Navigate that
    returns has its events recorded.

A listener runs on its session's worker, which is also what delivers that session's replies: it must
not wait on a reply on its own session, and anything it waits on holds up that session's messages.
Other sessions carry on, as each target's goroutine did in chromedp before v0.17.  Hand slow work to
a goroutine (listenRaw exists for events with big payloads).

Listeners attach to a context that is already attached.  A listener that needs what the target
already had when it attached - the frame-world registry - gets Chrome to announce it again
(trackFrameWorlds).
*/

// eventHub dispatches one connection's events to Biloba's listeners.
type eventHub struct {
	mu sync.Mutex
	// listeners are keyed by session; the browser's own events have the empty session ID.  A list is
	// never modified in place, so received can call a snapshot of it without holding mu.
	listeners map[target.SessionID][]*eventListener
	// slowf, when set, is told about a listener that held its session's worker too long.  It is only
	// set for a connection with debug logging on, so timing costs nothing otherwise.
	slowf func(string, ...any)
}

type eventListener struct {
	ctx     context.Context
	fn      func(any)
	raw     func(method string, params jsonv2.Value)
	methods []string // empty means every event
	// ended, when set, is called once Chrome detaches the listener's session or the connection to the
	// browser is lost: either way no more events will come.
	ended func()
}

func (l *eventListener) wants(method string) bool {
	return l.ctx.Err() == nil && (len(l.methods) == 0 || slices.Contains(l.methods, method))
}

// slowListenerThreshold is how long a listener may hold its session's worker before a debug-enabled
// connection reports it.
const slowListenerThreshold = 100 * time.Millisecond

func newEventHub(slowf func(string, ...any)) *eventHub {
	return &eventHub{
		listeners: map[target.SessionID][]*eventListener{},
		slowf:     slowf,
	}
}

var hubs sync.Map // *chromedp.Browser -> *eventHub

func registerHub(browser *chromedp.Browser, hub *eventHub) {
	hubs.Store(browser, hub)
	go func() {
		<-browser.LostConnection
		hubs.Delete(browser)
		hub.connectionLost()
	}()
}

// connectionLost ends every listener: no session sends anything more.
func (h *eventHub) connectionLost() {
	h.mu.Lock()
	var ended []func()
	for session, list := range h.listeners {
		for _, l := range list {
			if l.ended != nil {
				ended = append(ended, l.ended)
			}
		}
		delete(h.listeners, session)
	}
	h.mu.Unlock()
	for _, end := range ended {
		end()
	}
}

func hubFor(ctx context.Context) (*eventHub, *chromedp.Context, error) {
	c := chromedp.FromContext(ctx)
	if c == nil || c.Browser == nil {
		return nil, nil, errors.New("listening for events needs a context whose browser is allocated")
	}
	hub, ok := hubs.Load(c.Browser)
	if !ok {
		return nil, nil, errors.New("listening for events needs a browser that Biloba connected to")
	}
	return hub.(*eventHub), c, nil
}

/*
ListenTarget calls fn for each event of the target that ctx is attached to, in the order Chrome sent
them, until ctx is done.  Listeners on one target run in the order they were registered.  methods
limits fn to those events (and spares decoding the rest); none means every event.

fn runs on the session's worker (see orderedTransport): it must not wait on a reply on that session.
*/
func ListenTarget(ctx context.Context, fn func(any), methods ...string) error {
	return listen(ctx, &eventListener{ctx: ctx, fn: fn, methods: methods})
}

// listenRaw is ListenTarget for a listener that hands its events to another goroutine: it gets the
// undecoded params, so a big payload (a screencast frame) is not decoded on the session's worker.
// ended is called once the session can send nothing more (see eventListener.ended), so the goroutine
// it hands events to can stop even when nobody stops it.
func listenRaw(ctx context.Context, fn func(method string, params jsonv2.Value), ended func(), methods ...string) error {
	return listen(ctx, &eventListener{ctx: ctx, raw: fn, ended: ended, methods: methods})
}

func listen(ctx context.Context, l *eventListener) error {
	hub, c, err := hubFor(ctx)
	if err != nil {
		return err
	}
	if c.Target == nil {
		return errors.New("listening for target events needs an attached target: run the context once first")
	}
	hub.add(c.Target.SessionID, l)
	return nil
}

// ListenBrowser is ListenTarget for the browser's own events (target discovery, crashes).
func ListenBrowser(ctx context.Context, fn func(any), methods ...string) error {
	hub, _, err := hubFor(ctx)
	if err != nil {
		return err
	}
	hub.add("", &eventListener{ctx: ctx, fn: fn, methods: methods})
	return nil
}

func (h *eventHub) add(session target.SessionID, l *eventListener) {
	h.mu.Lock()
	// Clip so the append copies: received may be calling a snapshot of the old list.
	h.listeners[session] = append(slices.Clip(h.listeners[session]), l)
	h.mu.Unlock()
	context.AfterFunc(l.ctx, func() {
		h.mu.Lock()
		h.removeLocked(l)
		h.mu.Unlock()
	})
}

func (h *eventHub) removeLocked(l *eventListener) {
	for session, list := range h.listeners {
		if i := slices.Index(list, l); i >= 0 {
			if len(list) == 1 {
				delete(h.listeners, session)
			} else {
				h.listeners[session] = slices.Delete(slices.Clone(list), i, i+1)
			}
			return
		}
	}
}

// received runs the listeners for one message from Chrome, and drops a detached session's listeners.
func (h *eventHub) received(msg *cdproto.Message) {
	h.dispatch(msg)
	if session := detachedSession(msg); session != "" {
		h.sessionEnded(session)
	}
}

// dispatch runs the listeners of msg's session for msg.  orderedTransport calls it on that session's
// worker, before chromedp sees the message.
func (h *eventHub) dispatch(msg *cdproto.Message) {
	if msg.Method == "" {
		return
	}
	h.mu.Lock()
	list := h.listeners[msg.SessionID]
	h.mu.Unlock()
	h.deliver(msg, list)
}

// detachedSession is the session a browser-level Target.detachedFromTarget names, or "".
func detachedSession(msg *cdproto.Message) target.SessionID {
	if msg.SessionID != "" || string(msg.Method) != target.DetachedFromTarget.Method {
		return ""
	}
	var params struct {
		SessionID target.SessionID `json:"sessionId"`
	}
	if jsonv2.Unmarshal(msg.Params, &params, chromedp.DefaultUnmarshalOptions) != nil {
		return ""
	}
	return params.SessionID
}

// deliver runs the listeners in list for msg, without the hub's lock.
func (h *eventHub) deliver(msg *cdproto.Message, list []*eventListener) {
	method := string(msg.Method)
	var event any
	var decodeErr error
	decoded := false
	for _, l := range list {
		if !l.wants(method) {
			continue
		}
		started := time.Time{}
		if h.slowf != nil {
			started = time.Now()
		}
		if l.raw != nil {
			l.raw(method, msg.Params)
		} else {
			if !decoded {
				decoded = true
				if event, decodeErr = cdproto.UnmarshalMessage(msg, chromedp.DefaultUnmarshalOptions); decodeErr != nil && h.slowf != nil {
					h.slowf("biloba: could not decode a %s event for its listeners: %v", method, decodeErr)
				}
			}
			// an event that does not decode skips the listeners that need it decoded, and only those
			if decodeErr != nil {
				continue
			}
			l.fn(event)
		}
		if h.slowf != nil {
			if elapsed := time.Since(started); elapsed > slowListenerThreshold {
				h.slowf("biloba: a %s listener held its session's CDP messages for %s", method, elapsed.Round(time.Millisecond))
			}
		}
	}
}

// sessionDetached drops a session's listeners: Chrome sends it nothing more, and a message that
// still arrives for it finds no listeners.
func (h *eventHub) sessionEnded(session target.SessionID) {
	h.mu.Lock()
	list := h.listeners[session]
	delete(h.listeners, session)
	h.mu.Unlock()
	// (on the session's worker: an ended hook must not block)
	for _, l := range list {
		if l.ended != nil {
			l.ended()
		}
	}
}

/*
orderedTransport runs the hub's listeners for every message read from Chrome before chromedp sees
it, one worker per session.  A session's messages are handled and handed to chromedp in the order
Chrome sent them, so its earlier events are recorded before chromedp acts on a reply or event behind
them, and a listener that waits - on a lock, or on a spec's matcher that calls another tab - holds
up only its own session, as each target's goroutine did in chromedp before v0.17.

chromedp routes each session's messages to that session's target and depends on nothing ordered
across sessions: it registers a new session only after the reply to its attach, and sends the
session its first command only after that, so nothing can arrive for a session chromedp does not
know yet.  A browser-level Target.detachedFromTarget is the one message that concerns another
session: chromedp drops whatever arrives for that session after it, as it always could (the target
and browser queues were never ordered against each other), and the hub forgets the session's
listeners on the session's own worker, once its earlier messages have been handled.
*/
type orderedTransport struct {
	chromedp.Transport
	hub *eventHub

	start sync.Once
	// out carries handled messages to Read.
	out chan *cdproto.Message
	// ended is closed when the connection's read fails; readErr is the failure, valid after.
	ended   chan struct{}
	readErr error

	mu      sync.Mutex
	workers map[target.SessionID]*sessionWorker
}

func newOrderedTransport(tr chromedp.Transport, hub *eventHub) *orderedTransport {
	return &orderedTransport{
		Transport: tr, hub: hub,
		out: make(chan *cdproto.Message), ended: make(chan struct{}),
		workers: map[target.SessionID]*sessionWorker{},
	}
}

// Read returns the next message whose session's listeners have run.  chromedp calls it from one
// goroutine; the connection itself is read by the transport's own.
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

func (t *orderedTransport) readConnection() {
	for {
		msg := new(cdproto.Message)
		if err := t.Transport.Read(context.Background(), msg); err != nil {
			t.readErr = err
			close(t.ended) // workers stop handing messages over; Read reports err
			return
		}
		if session := detachedSession(msg); session != "" {
			t.mu.Lock()
			worker := t.workers[session]
			delete(t.workers, session)
			t.mu.Unlock()
			if worker != nil {
				worker.push(func() { t.hub.sessionEnded(session) })
			} else {
				t.hub.sessionEnded(session)
			}
		}
		t.worker(msg.SessionID).push(func() {
			t.hub.dispatch(msg)
			select {
			case t.out <- msg:
			case <-t.ended:
			}
		})
	}
}

func (t *orderedTransport) worker(session target.SessionID) *sessionWorker {
	t.mu.Lock()
	defer t.mu.Unlock()
	worker := t.workers[session]
	if worker == nil {
		worker = &sessionWorker{}
		t.workers[session] = worker
	}
	return worker
}

// sessionWorker runs one session's work in order.  push never blocks - the connection's reader must
// not wait on a session - and the worker's goroutine runs only while it has work, so an idle or
// detached session holds none.  Its queue has no bound: a listener that stalls holds up its own
// session's messages, which wait here, and no other session's.
type sessionWorker struct {
	mu      sync.Mutex
	queue   []func()
	running bool
}

func (w *sessionWorker) push(work func()) {
	w.mu.Lock()
	w.queue = append(w.queue, work)
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()
	go w.run()
}

func (w *sessionWorker) run() {
	for {
		w.mu.Lock()
		if len(w.queue) == 0 {
			w.running = false
			w.mu.Unlock()
			return
		}
		work := w.queue[0]
		w.queue[0] = nil
		w.queue = w.queue[1:]
		w.mu.Unlock()
		work()
	}
}

// SetDebugf passes chromedp's protocol logger through to the connection, which is what writes it.
func (t *orderedTransport) SetDebugf(f func(string, ...any)) {
	if s, ok := t.Transport.(interface{ SetDebugf(func(string, ...any)) }); ok {
		s.SetDebugf(f)
	}
}

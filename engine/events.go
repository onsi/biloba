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

So Biloba listens on the connection.  An orderedTransport wraps the websocket and hands every
message it reads to the connection's eventHub before chromedp sees it.  Listeners run there,
synchronously, on chromedp's reader goroutine:

  - they see a target's events in wire order, across methods (and across sessions);
  - every event Chrome sent before a command's reply, or before the load event Navigate waits for,
    has been handled by the time chromedp acts on that later message, so a Navigate that returns has
    its events recorded.

A listener registered before its context is attached (ListenTargetFromAttach) is bound to that
context's own session when the context confirms it (ConfirmAttached, after its first Run).  Nothing
on the wire says which context an attachment belongs to - two contexts can attach one target at once,
and chromedp hands the transport its own context, not the caller's - and chromedp keeps the lock it
sets a context's Target under to itself, so only the context's own goroutine, once its Run has
returned, can read which session it got.  Until then the hub queues the events of every new session
(one it sees a first command go out on while a context is waiting): chromedp attaches and only then
sends the session Runtime.enable, so nothing the session raises is missed, and the confirmation
replays the queue in order before going live.  A session nobody confirms - chromedp's flatten
auto-attach under a page, which no chromedp context owns and Biloba never listened to under
chromedp.ListenTarget either - is forgotten once nobody is waiting.  Biloba's own out-of-process
frame attachments are contexts of their own, so they confirm.

Running on the reader goroutine means a listener must not block - not on a CDP reply, and not on a
lock that code waiting on one holds - or the whole connection stalls with it, every tab of the
worker included.  Hand slow work to a goroutine (listenRaw exists for events with big payloads).
*/

// eventHub dispatches one connection's events to Biloba's listeners.
type eventHub struct {
	mu sync.Mutex
	// listeners are keyed by session; the browser's own events have the empty session ID.  A list is
	// never modified in place, so received can call a snapshot of it without holding mu.
	listeners map[target.SessionID][]*eventListener
	// pending holds, for each context that is not attached yet, the listeners waiting for its
	// attachment (see pend).
	pending map[any]*pendingAttach
	// unclaimed queues, by session, the events of sessions that may belong to a pending context.
	unclaimed map[target.SessionID]*unclaimedSession
	// slowf, when set, is told about a listener that held the reader goroutine too long.  It is only
	// set for a connection with debug logging on, so timing costs nothing otherwise.
	slowf func(string, ...any)
}

// pendingAttach is one context's listeners, waiting for that context to confirm its attachment.
type pendingAttach struct {
	listeners []*eventListener
}

// unclaimedSession queues the events of a session that may be a waiting context's, until that
// context confirms it (claimed is set from then on) or nobody is waiting any more.
type unclaimedSession struct {
	events  []*cdproto.Message
	claimed bool
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

// slowListenerThreshold is how long a listener may hold the reader goroutine before a debug-enabled
// connection reports it.
const slowListenerThreshold = 100 * time.Millisecond

func newEventHub(slowf func(string, ...any)) *eventHub {
	return &eventHub{
		listeners: map[target.SessionID][]*eventListener{},
		pending:   map[any]*pendingAttach{},
		unclaimed: map[target.SessionID]*unclaimedSession{},
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

fn runs on the connection's reader goroutine: it must not block.
*/
func ListenTarget(ctx context.Context, fn func(any), methods ...string) error {
	return listen(ctx, &eventListener{ctx: ctx, fn: fn, methods: methods})
}

// listenRaw is ListenTarget for a listener that hands its events to another goroutine: it gets the
// undecoded params, so a big payload (a screencast frame) is not decoded on the reader goroutine.
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
		return errors.New("listening for target events needs an attached target; use ListenTargetFromAttach before the first Run")
	}
	hub.add(c.Target.SessionID, l)
	return nil
}

/*
ListenTargetFromAttach is ListenTarget for a context that has not attached yet: it registers fn for
the session ctx's own attachment creates, so fn sees the events the attachment itself provokes
(Runtime.enable announcing the existing execution contexts).  Call ctx's first Run next, and
ConfirmAttached once it has succeeded.  targetID documents which target ctx attaches; the binding
goes by ctx, so contexts attaching one target at once each get their own session.  It never waits.
*/
func ListenTargetFromAttach(ctx context.Context, targetID target.ID, fn func(any), methods ...string) error {
	hub, c, err := hubFor(ctx)
	if err != nil {
		return err
	}
	l := &eventListener{ctx: ctx, fn: fn, methods: methods}
	if c.Target != nil {
		hub.add(c.Target.SessionID, l)
		return nil
	}
	hub.pend(c, l)
	return nil
}

/*
ConfirmAttached binds the listeners ListenTargetFromAttach registered on ctx to the session ctx is now
attached through, and hands them every event that session has raised so far.  Call it once ctx's
first Run has returned successfully, from a goroutine that run happens before (the caller's own, or
one that received its result over a channel): that is what makes reading ctx's chromedp Target safe,
chromedp keeping the lock it writes Target under to itself.  A Run that failed may be retried on ctx
before confirming.
*/
func ConfirmAttached(ctx context.Context) {
	hub, c, err := hubFor(ctx)
	if err != nil || c.Target == nil {
		return
	}
	hub.confirm(c, c.Target.SessionID)
}

// pend registers l to be bound to owner's session when owner confirms it (see ConfirmAttached).  It
// never waits.  A registration whose context never attaches - its attach failed and is not retried -
// stays until its context ends, holding nothing up.
func (h *eventHub) pend(owner any, l *eventListener) {
	h.mu.Lock()
	p := h.pending[owner]
	if p == nil {
		p = &pendingAttach{}
		h.pending[owner] = p
	}
	p.listeners = append(p.listeners, l)
	h.mu.Unlock()
	context.AfterFunc(l.ctx, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.pending[owner] == p {
			p.listeners = slices.DeleteFunc(p.listeners, func(waiting *eventListener) bool { return waiting == l })
			if len(p.listeners) == 0 {
				delete(h.pending, owner)
				h.dropUnclaimedLocked()
			}
		}
		h.removeLocked(l)
	})
}

// confirm binds owner's waiting listeners to session.  The events the session raised before the
// confirmation are delivered first, here, in order; the reader goroutine keeps queueing the session's
// new events behind them until the queue is empty, and only then hands the session over.
func (h *eventHub) confirm(owner any, session target.SessionID) {
	h.mu.Lock()
	p := h.pending[owner]
	if p == nil || session == "" {
		h.mu.Unlock()
		return
	}
	delete(h.pending, owner)
	queued := h.unclaimed[session]
	if queued == nil {
		// nothing queued (the session's events were not held back): it is live from here
		h.listeners[session] = append(slices.Clip(h.listeners[session]), p.listeners...)
		h.dropUnclaimedLocked()
		h.mu.Unlock()
		return
	}
	queued.claimed = true
	h.mu.Unlock()
	for {
		h.mu.Lock()
		var batch []*cdproto.Message
		if queued != nil {
			batch, queued.events = queued.events, nil
		}
		if len(batch) == 0 {
			if h.unclaimed[session] == queued {
				delete(h.unclaimed, session)
			}
			h.listeners[session] = append(slices.Clip(h.listeners[session]), p.listeners...)
			if len(h.pending) == 0 {
				h.dropUnclaimedLocked()
			}
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()
		for _, msg := range batch {
			h.deliver(msg, p.listeners)
		}
	}
}

// dropUnclaimedLocked forgets the events of sessions nobody is waiting to claim any more.
func (h *eventHub) dropUnclaimedLocked() {
	if len(h.pending) > 0 {
		return
	}
	for session, queued := range h.unclaimed {
		if !queued.claimed {
			delete(h.unclaimed, session)
		}
	}
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

// sent starts queueing the events of a session the hub has not seen before, while some context is
// waiting to confirm its attachment: it may be that context's.  chromedp attaches and only then sends
// the new session its first command (Runtime.enable), so every event the session raises arrives after
// the queue exists.
func (h *eventHub) sent(msg *cdproto.Message) {
	if msg.SessionID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.pending) == 0 {
		return
	}
	if _, bound := h.listeners[msg.SessionID]; bound {
		return
	}
	if _, queued := h.unclaimed[msg.SessionID]; !queued {
		h.unclaimed[msg.SessionID] = &unclaimedSession{}
	}
}

// received runs the listeners for one message from Chrome.  It is called on chromedp's reader
// goroutine, before chromedp routes the message.
func (h *eventHub) received(msg *cdproto.Message) {
	if msg.Method == "" {
		return
	}
	if string(msg.Method) == target.DetachedFromTarget.Method {
		defer h.sessionDetached(msg)
	}
	h.mu.Lock()
	if queued := h.unclaimed[msg.SessionID]; queued != nil {
		queued.events = append(queued.events, msg)
		h.mu.Unlock()
		return
	}
	list := h.listeners[msg.SessionID]
	h.mu.Unlock()
	h.deliver(msg, list)
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
				h.slowf("biloba: a %s listener held the CDP reader for %s", method, elapsed.Round(time.Millisecond))
			}
		}
	}
}

// sessionDetached drops a session's listeners: Chrome sends it nothing more, and a message that
// still arrives for it finds no listeners.
func (h *eventHub) sessionDetached(msg *cdproto.Message) {
	var params struct {
		SessionID target.SessionID `json:"sessionId"`
	}
	if jsonv2.Unmarshal(msg.Params, &params, chromedp.DefaultUnmarshalOptions) != nil || params.SessionID == "" {
		return
	}
	h.mu.Lock()
	list := h.listeners[params.SessionID]
	delete(h.listeners, params.SessionID)
	delete(h.unclaimed, params.SessionID)
	h.mu.Unlock()
	// (on the reader goroutine: an ended hook must not block)
	for _, l := range list {
		if l.ended != nil {
			l.ended()
		}
	}
}

// orderedTransport hands every message read from Chrome to the hub before chromedp sees it.
type orderedTransport struct {
	chromedp.Transport
	hub *eventHub
}

func (t *orderedTransport) Read(ctx context.Context, msg *cdproto.Message) error {
	if err := t.Transport.Read(ctx, msg); err != nil {
		return err
	}
	t.hub.received(msg)
	return nil
}

func (t *orderedTransport) Write(ctx context.Context, msg *cdproto.Message) error {
	t.hub.sent(msg)
	return t.Transport.Write(ctx, msg)
}

// SetDebugf passes chromedp's protocol logger through to the connection, which is what writes it.
func (t *orderedTransport) SetDebugf(f func(string, ...any)) {
	if s, ok := t.Transport.(interface{ SetDebugf(func(string, ...any)) }); ok {
		s.SetDebugf(f)
	}
}

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

A listener registered before its target is attached (ListenTargetFromAttach) is bound to the session
of Biloba's own Target.attachToTarget, and never to a session chromedp's flatten auto-attach opens
under a page: no chromedp context owns those, Runtime is not enabled on them, and Biloba never
listened to them under chromedp.ListenTarget either.  Biloba's own out-of-process frame attachments
go through attachToTarget, so they are bound.

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
	// pending holds the listeners waiting for an attachment to a target, at most one attachment per
	// target at a time (see pend).
	pending map[target.ID]*pendingAttach
	// attaching maps an in-flight Target.attachToTarget command to its target.
	attaching map[int64]target.ID
	// slowf, when set, is told about a listener that held the reader goroutine too long.  It is only
	// set for a connection with debug logging on, so timing costs nothing otherwise.
	slowf func(string, ...any)
}

// pendingAttach is one context's listeners, waiting for that context's attachment to a target.
type pendingAttach struct {
	owner     any // the chromedp context that will attach
	listeners []*eventListener
	settled   chan struct{} // closed once the attachment binds them, or they are abandoned
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
		pending:   map[target.ID]*pendingAttach{},
		attaching: map[int64]target.ID{},
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
the session ctx's attachment to targetID creates, so fn sees the events the attachment itself
provokes (Runtime.enable announcing the existing execution contexts).  Call ctx's first Run next.

While another context's attachment to the same target is pending, it waits for that one to land (or
be abandoned) first: two attachments to one target are otherwise indistinguishable on the wire.
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
	return hub.pend(c, targetID, l)
}

// pend registers l to be bound to the session that owner's next attachment to targetID creates.
func (h *eventHub) pend(owner any, targetID target.ID, l *eventListener) error {
	var p *pendingAttach
	for {
		h.mu.Lock()
		p = h.pending[targetID]
		if p == nil {
			p = &pendingAttach{owner: owner, settled: make(chan struct{})}
			h.pending[targetID] = p
		}
		if p.owner == owner {
			p.listeners = append(p.listeners, l)
			h.mu.Unlock()
			break
		}
		h.mu.Unlock()
		select {
		case <-p.settled:
		case <-l.ctx.Done():
			return l.ctx.Err()
		}
	}
	context.AfterFunc(l.ctx, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.pending[targetID] == p {
			p.listeners = slices.DeleteFunc(p.listeners, func(waiting *eventListener) bool { return waiting == l })
			if len(p.listeners) == 0 {
				delete(h.pending, targetID)
				close(p.settled)
			}
		}
		h.removeLocked(l)
	})
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

// bindLocked moves the listeners waiting on targetID to the session that now attaches it.  There is
// only ever one pending attachment per target (see pend), so they are that attachment's.
func (h *eventHub) bindLocked(targetID target.ID, session target.SessionID) {
	p := h.pending[targetID]
	if p == nil || session == "" {
		return
	}
	delete(h.pending, targetID)
	h.listeners[session] = append(slices.Clip(h.listeners[session]), p.listeners...)
	close(p.settled)
}

// sent notes the attachments Biloba's contexts ask for, so their sessions can be matched to the
// listeners waiting on them.
func (h *eventHub) sent(msg *cdproto.Message) {
	if msg.Method != cdproto.MethodType(target.AttachToTarget.Method) || msg.SessionID != "" {
		return
	}
	var params struct {
		TargetID target.ID `json:"targetId"`
	}
	if jsonv2.Unmarshal(msg.Params, &params, chromedp.DefaultUnmarshalOptions) != nil {
		return
	}
	h.mu.Lock()
	if h.pending[params.TargetID] != nil {
		h.attaching[msg.ID] = params.TargetID
	}
	h.mu.Unlock()
}

// received runs the listeners for one message from Chrome.  It is called on chromedp's reader
// goroutine, before chromedp routes the message.
func (h *eventHub) received(msg *cdproto.Message) {
	if msg.Method == "" {
		if msg.ID != 0 {
			h.attachReplied(msg)
		}
		return
	}
	method := string(msg.Method)
	switch method {
	case target.AttachedToTarget.Method:
		h.sessionAttached(msg)
	case target.DetachedFromTarget.Method:
		defer h.sessionDetached(msg)
	}
	h.mu.Lock()
	list := h.listeners[msg.SessionID]
	h.mu.Unlock()
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

// sessionAttached binds the listeners waiting on a target to the session of an attachment the
// browser connection made (Chrome announces it before it replies to Target.attachToTarget).
// Attachments Chrome makes on its own under a page's session (auto-attach) belong to no chromedp
// context and have no domains enabled, so they never receive a waiting listener.
func (h *eventHub) sessionAttached(msg *cdproto.Message) {
	if msg.SessionID != "" {
		return
	}
	var params struct {
		SessionID  target.SessionID `json:"sessionId"`
		TargetInfo struct {
			TargetID target.ID `json:"targetId"`
		} `json:"targetInfo"`
	}
	if jsonv2.Unmarshal(msg.Params, &params, chromedp.DefaultUnmarshalOptions) != nil {
		return
	}
	h.mu.Lock()
	h.bindLocked(params.TargetInfo.TargetID, params.SessionID)
	h.mu.Unlock()
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
	h.mu.Unlock()
	// (on the reader goroutine: an ended hook must not block)
	for _, l := range list {
		if l.ended != nil {
			l.ended()
		}
	}
}

func (h *eventHub) attachReplied(msg *cdproto.Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	targetID, ok := h.attaching[msg.ID]
	if !ok {
		return
	}
	delete(h.attaching, msg.ID)
	if msg.Error != nil {
		return
	}
	var result struct {
		SessionID target.SessionID `json:"sessionId"`
	}
	if jsonv2.Unmarshal(msg.Result, &result, chromedp.DefaultUnmarshalOptions) == nil {
		h.bindLocked(targetID, result.SessionID)
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

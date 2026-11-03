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
connection's eventHub on every message it reads, before chromedp sees it: the browser's own messages
on the connection's reader, each flattened session's on a worker of its own (see orderedTransport in
transport.go):

  - listeners see their session's events in wire order, across methods;
  - every event Chrome sent on a session before a command's reply, or before the load event Navigate
    waits for, has been handled by the time chromedp acts on that later message, so a Navigate that
    returns has its events recorded.

A target's listener runs on its session's worker, which is also what delivers that session's replies:
it must not wait on a reply on its own session, and anything it waits on holds up that session's
messages.  Other sessions carry on, as each target's goroutine did in chromedp before v0.17.  A
browser listener runs on the reader, as chromedp's own did, and must not wait on anything.  Hand slow
work to a goroutine (listenRaw exists for events with big payloads).

Listeners attach to a context that is already attached, so they miss what the target announced while
chromedp attached it.  The one thing Biloba needs from that time, the frame-world registry (the page
JavaScript context of each frame document), the hub keeps itself: for every session, from the
session's first message, updated before any listener runs (see frameWorlds in frame_context.go).
*/

// eventHub dispatches one connection's events to Biloba's listeners.
type eventHub struct {
	mu sync.Mutex
	// listeners are keyed by session; the browser's own events have the empty session ID.  A list is
	// never modified in place, so received can call a snapshot of it without holding mu.
	listeners map[target.SessionID][]*eventListener
	// worlds is each session's frame-world registry, made by the session's first context event (or
	// by trackFrameWorlds asking for it) and dropped when the session ends.
	worlds map[target.SessionID]*frameWorlds
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
		worlds:    map[target.SessionID]*frameWorlds{},
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
	clear(h.worlds)
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
	var worlds *frameWorlds
	if msg.SessionID != "" && frameWorldEvent(string(msg.Method)) {
		worlds = h.worldsLocked(msg.SessionID)
	}
	h.mu.Unlock()
	// before the listeners, so one that asks whether its frame's world is current sees this event
	if worlds != nil {
		worlds.observe(string(msg.Method), msg.Params)
	}
	h.deliver(msg, list)
}

// worldsFor returns a session's frame-world registry, making it if the session has announced no
// context yet.
func (h *eventHub) worldsFor(session target.SessionID) *frameWorlds {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.worldsLocked(session)
}

func (h *eventHub) worldsLocked(session target.SessionID) *frameWorlds {
	worlds := h.worlds[session]
	if worlds == nil {
		worlds = newFrameWorlds()
		h.worlds[session] = worlds
	}
	return worlds
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

// sessionEnded drops a session's listeners and frame-world registry: Chrome sends it nothing more, and
// a message that still arrives for it finds no listeners.
func (h *eventHub) sessionEnded(session target.SessionID) {
	h.mu.Lock()
	list := h.listeners[session]
	delete(h.listeners, session)
	delete(h.worlds, session)
	h.mu.Unlock()
	// (on the session's worker: an ended hook must not block)
	for _, l := range list {
		if l.ended != nil {
			l.ended()
		}
	}
}

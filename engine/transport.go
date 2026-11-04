package engine

import (
	"context"
	"maps"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/cdproto/cdp"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

/*
connection wraps the websocket to Chrome.  chromedp runs Biloba's listeners itself, in order
(chromedp.Listen; see events.go), so the connection does only what a listener cannot.

A listener can only be registered on a target chromedp has attached, and chromedp's attach enables
Runtime before it returns: Chrome announces the JavaScript contexts the target already has in reply,
and no listener sees them.  The frame-world registry needs those contexts (a popup's frames, an
out-of-process frame's document), so the connection records each session's from the session's first
message, on chromedp's reader, until trackFrameWorlds takes the session's registry over: from then on
a listener keeps it, in the order the session's other listeners see its events.  (The reader runs
ahead of a target's listeners, so a registry it kept would tell a listener handling a frame's last
console message that the frame's document is already gone.)  A session's record goes when Chrome
detaches the session.

A command chromedp sends on a session returns only when its answer arrives (or its context ends), and
chromedp drops whatever arrives for a session once it has seen the session detach.  Chrome does not
always answer a command whose target goes away under it (an evaluation that closes its own window),
and chromedp can let the detach overtake an answer Chrome did send.  So the connection keeps the
commands each session has not answered, and when Chrome detaches the session it hands chromedp an
answer for each, with the error Chrome gives a command whose target goes away, and then a message
that only makes sure those answers reached the session before the detach does.

Write passes every command through writeHook, the seam specs use to see and hold commands.
*/
type connection struct {
	chromedp.Transport
	// slowf, when set, is told about a listener that held its target's messages too long.  It is only
	// set for a connection with debug logging on, so timing costs nothing otherwise.
	slowf func(string, ...any)

	mu       sync.Mutex
	sessions map[target.SessionID]*sessionWorlds
	// unanswered holds the commands sent on each session that Chrome has not answered yet.
	unanswered map[target.SessionID]map[int64]bool

	// backlog is what Read hands chromedp before it reads the connection again; only Read uses it.
	backlog []*cdproto.Message

	// writeMu serializes chromedp's writes with a spec's sending of the commands a hook held back.
	writeMu sync.Mutex
}

// sessionWorlds is what the connection keeps for one session's frame worlds.
type sessionWorlds struct {
	// seen is the record the reader keeps, under the connection's mu, until the registry is claimed.
	seen map[cdp.FrameID]frameWorld
	// claim serializes worldsFor calls for the session; registry is set by the first.
	claim    sync.Mutex
	registry *frameWorlds
}

func newConnection(tr chromedp.Transport, slowf func(string, ...any)) *connection {
	return &connection{
		Transport: tr, slowf: slowf,
		sessions: map[target.SessionID]*sessionWorlds{}, unanswered: map[target.SessionID]map[int64]bool{},
	}
}

// writeHook, when set, sees every command a connection sends Chrome, and keeps it back by returning
// true.
var writeHook atomic.Pointer[func(*connection, *cdproto.Message) bool]

func (c *connection) Write(ctx context.Context, msg *cdproto.Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	// recorded before it is sent, since its answer can be read before Write returns, and before a hook
	// holds it back, since for chromedp it has been sent
	tracked := msg.SessionID != "" && msg.ID != 0
	if tracked {
		c.mu.Lock()
		if c.unanswered[msg.SessionID] == nil {
			c.unanswered[msg.SessionID] = map[int64]bool{}
		}
		c.unanswered[msg.SessionID][msg.ID] = true
		c.mu.Unlock()
	}
	if hook := writeHook.Load(); hook != nil && (*hook)(c, msg) {
		return nil
	}
	err := c.Transport.Write(ctx, msg)
	if err != nil && tracked {
		c.answered(msg.SessionID, msg.ID)
	}
	return err
}

func (c *connection) answered(session target.SessionID, id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pending := c.unanswered[session]; pending != nil {
		delete(pending, id)
		if len(pending) == 0 {
			delete(c.unanswered, session)
		}
	}
}

// detachedSessionGoneError is what Chrome answers a command whose target goes away under it.
var detachedSessionGoneError = cdproto.Error{Code: -32000, Message: "Inspected target navigated or closed"}

// barrierMessageID marks the message that follows the answers to a detached session's commands.
// chromedp's own ids count up from 1, so nothing waits for it.
const barrierMessageID = -1

// sendHeld sends the commands a writeHook held back, which take returns while no other command is
// being written.
func (c *connection) sendHeld(ctx context.Context, take func() []*cdproto.Message) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	for _, msg := range take() {
		_ = c.Transport.Write(ctx, msg)
	}
}

// Read is chromedp's: it records what the connection keeps before chromedp routes the message.
func (c *connection) Read(ctx context.Context, msg *cdproto.Message) error {
	if len(c.backlog) > 0 {
		*msg = *c.backlog[0]
		c.backlog[0] = nil
		c.backlog = c.backlog[1:]
		return nil
	}
	if err := c.Transport.Read(ctx, msg); err != nil {
		return err
	}
	switch method := string(msg.Method); {
	case msg.ID != 0 && msg.SessionID != "":
		c.answered(msg.SessionID, msg.ID)
	case method == target.DetachedFromTarget.Method:
		// on the browser's session, or on a session for one Chrome attached under it (auto-attach)
		session := detachedSession(msg)
		if session == "" {
			break
		}
		c.mu.Lock()
		delete(c.sessions, session)
		pending := c.unanswered[session]
		delete(c.unanswered, session)
		c.mu.Unlock()
		if msg.SessionID == "" && len(pending) > 0 {
			c.answerBeforeDetach(session, pending, msg)
		}
	case msg.SessionID != "" && frameWorldEvent(method):
		c.record(msg)
	}
	return nil
}

// answerBeforeDetach puts an answer for each of a detached session's pending commands, and then a
// barrier, ahead of the detach in msg.  chromedp's reader hands a session's message to the loop that
// routes it before it reads the next, but the loop takes a detach as soon as the one before it, so the
// barrier, which chromedp may drop, makes sure the last answer was routed before the detach.
func (c *connection) answerBeforeDetach(session target.SessionID, pending map[int64]bool, detach *cdproto.Message) {
	ids := slices.Sorted(maps.Keys(pending))
	for _, id := range ids {
		gone := detachedSessionGoneError
		c.backlog = append(c.backlog, &cdproto.Message{ID: id, SessionID: session, Error: &gone})
	}
	detached := *detach
	c.backlog = append(c.backlog, &cdproto.Message{ID: barrierMessageID, SessionID: session, Result: jsonv2.Value(`{}`)}, &detached)
	*detach = *c.backlog[0]
	c.backlog = c.backlog[1:]
}

func (c *connection) record(msg *cdproto.Message) {
	ev, err := cdproto.UnmarshalMessage(msg, chromedp.DefaultUnmarshalOptions)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.sessions[msg.SessionID]
	if entry == nil {
		// a context going away starts no record: it may be all that is left of a session Chrome detached
		if _, created := ev.(*runtime.EventExecutionContextCreated); !created {
			return
		}
		entry = &sessionWorlds{seen: map[cdp.FrameID]frameWorld{}}
		c.sessions[msg.SessionID] = entry
	}
	if entry.seen != nil {
		applyFrameWorldEvent(entry.seen, ev)
	}
}

/*
worldsFor returns the frame-world registry of the session ctx's target is attached on, kept in order
by a listener on that target.  The first call for a session registers the listener and then seeds the
registry with what the reader recorded, holding the registry until it has: an event the reader read
after that is applied by the listener, after the seed.  An event the reader had read but the target
had not yet handed its listeners is applied again by the listener, in order, which leaves the
registry as it would have been had the listener seen everything.
*/
func (c *connection) worldsFor(ctx context.Context, session target.SessionID) (*frameWorlds, error) {
	c.mu.Lock()
	entry := c.sessions[session]
	c.mu.Unlock()
	if entry == nil {
		// The session has announced no context, or Chrome has detached it already: there is nothing
		// to seed the registry with, and nothing to keep for the session.
		registry := newFrameWorlds()
		return registry, chromedp.Listen(context.WithoutCancel(ctx), registry.apply)
	}

	entry.claim.Lock()
	defer entry.claim.Unlock()
	if entry.registry != nil {
		return entry.registry, nil
	}
	registry := newFrameWorlds()
	registry.mu.Lock()
	defer registry.mu.Unlock()
	// The listener lives as long as the target: the registry outlives the context it was asked for.
	if err := chromedp.Listen(context.WithoutCancel(ctx), registry.apply); err != nil {
		return nil, err
	}
	c.mu.Lock()
	maps.Copy(registry.byFrame, entry.seen)
	entry.seen = nil // the listener keeps it from here
	entry.registry = registry
	c.mu.Unlock()
	return registry, nil
}

// detachedSession is the session a Target.detachedFromTarget names, or "".
func detachedSession(msg *cdproto.Message) target.SessionID {
	var params struct {
		SessionID target.SessionID `json:"sessionId"`
	}
	if jsonv2.Unmarshal(msg.Params, &params, chromedp.DefaultUnmarshalOptions) != nil {
		return ""
	}
	return params.SessionID
}

// SetDebugf passes chromedp's protocol logger through to the connection, which is what writes it.
func (c *connection) SetDebugf(f func(string, ...any)) {
	if s, ok := c.Transport.(interface{ SetDebugf(func(string, ...any)) }); ok {
		s.SetDebugf(f)
	}
}

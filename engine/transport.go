package engine

import (
	"context"
	"maps"
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

Write passes every command through writeHook, the seam specs use to see and hold commands.
*/
type connection struct {
	chromedp.Transport
	// slowf, when set, is told about a listener that held its target's messages too long.  It is only
	// set for a connection with debug logging on, so timing costs nothing otherwise.
	slowf func(string, ...any)

	mu       sync.Mutex
	sessions map[target.SessionID]*sessionWorlds

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
	return &connection{Transport: tr, slowf: slowf, sessions: map[target.SessionID]*sessionWorlds{}}
}

// writeHook, when set, sees every command a connection sends Chrome, and keeps it back by returning
// true.
var writeHook atomic.Pointer[func(*connection, *cdproto.Message) bool]

func (c *connection) Write(ctx context.Context, msg *cdproto.Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if hook := writeHook.Load(); hook != nil && (*hook)(c, msg) {
		return nil
	}
	return c.Transport.Write(ctx, msg)
}

// sendHeld sends the commands a writeHook held back, which take returns while no other command is
// being written.
func (c *connection) sendHeld(ctx context.Context, take func() []*cdproto.Message) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	for _, msg := range take() {
		_ = c.Transport.Write(ctx, msg)
	}
}

// Read is chromedp's: it records a session's frame worlds before chromedp routes the message.
func (c *connection) Read(ctx context.Context, msg *cdproto.Message) error {
	if err := c.Transport.Read(ctx, msg); err != nil {
		return err
	}
	switch method := string(msg.Method); {
	case method == target.DetachedFromTarget.Method:
		// on the browser's session, or on a session for one Chrome attached under it (auto-attach)
		if session := detachedSession(msg); session != "" {
			c.mu.Lock()
			delete(c.sessions, session)
			c.mu.Unlock()
		}
	case msg.SessionID != "" && frameWorldEvent(method):
		c.record(msg)
	}
	return nil
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

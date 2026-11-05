package engine

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/chromedp/cdproto"
	"github.com/chromedp/chromedp"
)

/*
connection wraps the websocket to Chrome.  chromedp runs Biloba's listeners itself, in order, from
the start of a target when asked (chromedp.Listen, chromedp.WithListener; see events.go), routes a
session's detach after every message of the session read before it, and tells a context that its
target was detached (chromedp.Detached), so the connection keeps nothing of the messages it passes on.
What is left:

  - Write passes every command through writeHook, the seam specs use to see and hold commands;
  - slowf, which ListenTarget and ListenBrowser use to report a listener that holds its target's
    messages too long (see timed in events.go).
*/
type connection struct {
	chromedp.Transport
	// slowf, when set, is told about a listener that held its target's messages too long.  It is only
	// set for a connection with debug logging on, so timing costs nothing otherwise.
	slowf func(string, ...any)

	// writeMu serializes chromedp's writes with a spec's sending of the commands a hook held back.
	writeMu sync.Mutex
}

func newConnection(tr chromedp.Transport, slowf func(string, ...any)) *connection {
	return &connection{Transport: tr, slowf: slowf}
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

// SetDebugf passes chromedp's protocol logger through to the connection, which is what writes it.
func (c *connection) SetDebugf(f func(string, ...any)) {
	if s, ok := c.Transport.(interface{ SetDebugf(func(string, ...any)) }); ok {
		s.SetDebugf(f)
	}
}

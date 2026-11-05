package engine

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

/*
Biloba needs every event a target raises in the order Chrome sent it, across event types: the
Prepare boundary, the in-flight tracker, and the frame-world registry all depend on it.  chromedp's
Events/Subscribe give one queue per method, each drained by a goroutine of its own, so neither the
order across methods nor the timing below survives them.  chromedp.Listen gives both, and
ListenTarget and ListenBrowser are thin layers over it:

  - listeners see their target's events in wire order, across methods, and listeners on one target
    run in the order they were registered;
  - a target hands its listeners every event Chrome sent before a command's reply, or before the
    load event Navigate waits for, before chromedp acts on that later message, so a Navigate that
    returns has its events recorded.

A target's listeners run on the goroutine chromedp reads that target's messages on, which also
delivers its replies: a listener must not run a command on its own target or wait on one, must not
register a listener, and holds up that target's messages for as long as it runs.  chromedp queues
1024 messages for a target, and once that many are waiting, the one loop that routes every target's
messages and sends every command waits too, so a listener that waits can hold up every tab on the
connection.  A browser listener runs on chromedp's reader, before the next message of any session is
read, and must not wait on anything.  A listener records under locks no CDP caller holds and hands
anything that waits on Chrome (answering a dialog or a paused request) to a goroutine, a Handoff when
the order matters.

A listener sees the events that arrive after it is registered, on a target chromedp has already
attached, so it misses what the target announced while chromedp attached it.  The one thing Biloba
needs from that time, the frame-world registry, the connection records itself (see connection in
transport.go).
*/

var connections sync.Map // *chromedp.Browser -> *connection

func registerConnection(browser *chromedp.Browser, conn *connection) {
	connections.Store(browser, conn)
	go func() {
		<-browser.LostConnection
		connections.Delete(browser)
	}()
}

func connectionFor(ctx context.Context) (*connection, *chromedp.Context, error) {
	c := chromedp.FromContext(ctx)
	if c == nil || c.Browser == nil {
		return nil, nil, errors.New("listening for events needs a context whose browser is allocated")
	}
	conn, ok := connections.Load(c.Browser)
	if !ok {
		return nil, nil, errors.New("listening for events needs a browser that Biloba connected to")
	}
	return conn.(*connection), c, nil
}

/*
ListenTarget calls fn for each event of the target that ctx is attached to, in the order Chrome sent
them, until ctx is done.  Listeners on one target run in the order they were registered.  fn gets
cdproto's typed events (*page.EventLoadEventFired) and no replies.

fn runs on the goroutine that reads the target's messages: it must not run a command on that target or
wait on one, and must not register a listener.
*/
func ListenTarget(ctx context.Context, fn func(any)) error {
	conn, c, err := connectionFor(ctx)
	if err != nil {
		return err
	}
	// chromedp.Listen would attach a target that has none, on ctx: the attach would end with it
	if c.Target == nil {
		return errors.New("listening for target events needs an attached target: run the context once first")
	}
	return chromedp.Listen(ctx, conn.timed(fn))
}

// ListenBrowser is ListenTarget for the browser's own events (target discovery, crashes).  fn runs on
// chromedp's reader and must not wait on anything.
func ListenBrowser(ctx context.Context, fn func(any)) error {
	conn, _, err := connectionFor(ctx)
	if err != nil {
		return err
	}
	return chromedp.ListenBrowser(ctx, conn.timed(fn))
}

/*
WatchTargetGone calls onGone, once, when Chrome detaches the session ctx's target is attached on (its
target closed, or the session was detached), or at once if Chrome already has, until ctx is done.
A listener sees only what arrives after it is registered, so it also asks the connection, which
records each detach before chromedp's listeners see it: a session that went while it was being set
up is not missed.  onGone may run on chromedp's reader, so it must not wait on anything.
*/
func WatchTargetGone(ctx context.Context, onGone func()) error {
	conn, c, err := connectionFor(ctx)
	if err != nil {
		return err
	}
	if c.Target == nil {
		return errors.New("watching a target needs an attached target: run the context once first")
	}
	session := c.Target.SessionID
	var once sync.Once
	gone := func() { once.Do(onGone) }
	if err := chromedp.ListenBrowser(ctx, func(ev any) {
		if detached, ok := ev.(*target.EventDetachedFromTarget); ok && detached.SessionID == session {
			gone()
		}
	}); err != nil {
		return err
	}
	if conn.sessionDetached(session) {
		gone()
	}
	return nil
}

// TargetGone reports whether Chrome has detached the session ctx's target is attached on, as far as
// the connection has read, or false for a context not attached yet.  The connection records a detach
// before chromedp sees it and ends the session's waiting commands, so a command failing because its
// target went away already sees it, before any listener has run.  It sends no
// command.
func TargetGone(ctx context.Context) bool {
	conn, c, err := connectionFor(ctx)
	return err == nil && c.Target != nil && conn.sessionDetached(c.Target.SessionID)
}

// slowListenerThreshold is how long a listener may hold its target's messages before a debug-enabled
// connection reports it.
const slowListenerThreshold = 100 * time.Millisecond

// timed reports a call of fn that holds its target's messages too long, when debugging is on.
func (c *connection) timed(fn func(any)) func(any) {
	if c.slowf == nil {
		return fn
	}
	return func(ev any) {
		started := time.Now()
		fn(ev)
		if elapsed := time.Since(started); elapsed > slowListenerThreshold {
			// (the event's type names its domain and method: *page.EventLoadEventFired)
			c.slowf("biloba: a %T listener held its target's CDP messages for %s", ev, elapsed.Round(time.Millisecond))
		}
	}
}

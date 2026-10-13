package engine

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

//go:generate cp ../clock.js clock.js
//go:embed clock.js
var clockJS string

// DefaultClockRunTimeout bounds how long RunFor may spend, in real time, running the callbacks that
// fall due in the fake time it advances.  A timer or animation loop that keeps firing makes a long
// RunFor slow rather than endless (every callback moves fake time forward), so this is a budget, not
// a liveness check.
const DefaultClockRunTimeout = 10 * time.Second

// ClockState is a page clock as the tab's own document last reported it.
type ClockState struct {
	// Now is the clock's time: what Date reports unless Fixed pins it.
	Now time.Time
	// Paused is true once PauseAt has stopped time from flowing, until Resume.
	Paused bool
	// Fixed is the time SetFixedTime pinned Date to, or nil.
	Fixed *time.Time
}

// clockSnapshot is the clock state on the wire between Go and clock.js.  Times are milliseconds since
// the epoch.  Wall is the browser's real Date.now() when the state was read: a document that installs
// from a flowing snapshot later catches up by the real time that has passed since.
type clockSnapshot struct {
	Now    float64  `json:"now"`
	Paused bool     `json:"paused"`
	Fixed  *float64 `json:"fixed"`
	Wall   *float64 `json:"wall"`
}

type clockReply struct {
	clockSnapshot
	Fired    int  `json:"fired"`
	Exceeded bool `json:"exceeded"`
}

// PageClock is one tab's page clock: fake timers (clock.js) installed in each of the tab's documents,
// and the new-document script that installs it in documents that load later.  Both clients drive it
// through these methods, the Go API from its tab and the engine Session from its own; neither may
// call it concurrently with itself.
//
// Every operation runs in every document the tab's renderer attachment knows about - its own
// document first, then its same-process frames - one after another.  A document that has no clock
// yet (a frame that loaded without running the new-document script) gets one, from the state the
// clock was in before the operation, and then the operation.  Out-of-process frames are not covered:
// they run on real time.
type PageClock struct {
	script   page.ScriptIdentifier
	snapshot clockSnapshot
}

func clockMillis(t time.Time) float64 { return float64(t.UnixMilli()) }

func clockTime(ms float64) time.Time { return time.UnixMilli(int64(ms)).UTC() }

// InstallClockContext installs the page clock at start, flowing, in every document of the tab and in
// every document it loads from now on.
func InstallClockContext(ctx context.Context, start time.Time) (*PageClock, error) {
	c := &PageClock{snapshot: clockSnapshot{Now: clockMillis(start)}}
	reply, err := c.inEveryDocument(ctx, "install the page clock", func(docCtx context.Context) (*clockReply, error) {
		return c.install(docCtx)
	})
	if err != nil {
		return nil, err
	}
	c.snapshot = reply.clockSnapshot
	if err := c.register(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// State reports the clock as the tab's own document last reported it.  It sends nothing to Chrome.
func (c *PageClock) State() ClockState {
	state := ClockState{Now: clockTime(c.snapshot.Now), Paused: c.snapshot.Paused}
	if c.snapshot.Fixed != nil {
		fixed := clockTime(*c.snapshot.Fixed)
		state.Fixed = &fixed
	}
	return state
}

// PauseAtContext stops time at t.  A t ahead of the clock gets there the way FastForward does, firing
// each timer that falls due on the way once; a t behind it (time flows after install, so a PauseAt at
// the install time is usually behind) moves the clock back without firing anything.
func (c *PageClock) PauseAtContext(ctx context.Context, t time.Time) error {
	return c.do(ctx, "pause the page clock", fmt.Sprintf("pauseAt(%d)", t.UnixMilli()))
}

// ResumeContext lets time flow again at the real rate from wherever the clock is.
func (c *PageClock) ResumeContext(ctx context.Context) error {
	return c.do(ctx, "resume the page clock", "resume()")
}

// FastForwardContext jumps the clock ahead by d, firing each timer that fell due along the way once,
// at the new time - like a laptop waking from sleep.
func (c *PageClock) FastForwardContext(ctx context.Context, d time.Duration) error {
	if d < 0 {
		return &Error{Code: CodeInvalidArgument, Operation: "fast-forward the page clock", Message: fmt.Sprintf("the duration must not be negative, got %s", d)}
	}
	return c.do(ctx, "fast-forward the page clock", fmt.Sprintf("fastForward(%d)", d.Milliseconds()))
}

// SetFixedTimeContext pins what Date reports to t.  Timers keep running on the clock's own time.
func (c *PageClock) SetFixedTimeContext(ctx context.Context, t time.Time) error {
	return c.do(ctx, "fix the page clock's time", fmt.Sprintf("setFixedTime(%d)", t.UnixMilli()))
}

// SetSystemTimeContext moves the clock's time to t without firing any timer: a pending timer keeps the
// delay it had left.  It unpins a time SetFixedTime pinned.
func (c *PageClock) SetSystemTimeContext(ctx context.Context, t time.Time) error {
	return c.do(ctx, "set the page clock's time", fmt.Sprintf("setSystemTime(%d)", t.UnixMilli()))
}

// RunForContext advances the clock by d, running every timer and animation frame that falls due in
// order, each in its own task so the page's promise callbacks run in between.  It gives up once it has
// spent budget of real time on it - a page whose callbacks keep rescheduling themselves makes a long
// RunFor slow - and reports how far it got.  ctx must outlast budget.
func (c *PageClock) RunForContext(ctx context.Context, d, budget time.Duration) error {
	const operation = "run the page clock"
	if d < 0 {
		return &Error{Code: CodeInvalidArgument, Operation: operation, Message: fmt.Sprintf("the duration must not be negative, got %s", d)}
	}
	if budget <= 0 {
		budget = DefaultClockRunTimeout
	}
	deadline := time.Now().Add(budget)
	fired, exceeded := 0, false
	err := c.apply(ctx, operation, func(docCtx context.Context) (*clockReply, error) {
		remaining := max(time.Until(deadline).Milliseconds(), 0)
		reply, err := c.evaluate(docCtx, fmt.Sprintf("runFor(%d, %d)", d.Milliseconds(), remaining), true)
		if reply != nil {
			fired += reply.Fired
			exceeded = exceeded || reply.Exceeded
		}
		return reply, err
	})
	if err != nil {
		return err
	}
	if exceeded {
		return &Error{Code: CodeTimeout, Operation: operation, Message: fmt.Sprintf(
			"RunFor(%s) did not finish within %s: after %d timer and animation-frame callbacks the page clock had reached %s.\n"+
				"The page keeps scheduling work, so running every callback in that much time is slow.  FastForward skips ahead firing each due timer once, or allow RunFor more time.",
			d, budget, fired, FormatClockTime(clockTime(c.snapshot.Now)))}
	}
	return nil
}

// UninstallContext restores the real timers in every document of the tab and stops installing the
// clock in new ones.  Timers the page scheduled on the clock are dropped.
func (c *PageClock) UninstallContext(ctx context.Context) error {
	err := c.RemoveContext(ctx)
	_, uninstallErr := c.inEveryDocument(ctx, "uninstall the page clock", func(docCtx context.Context) (*clockReply, error) {
		return nil, EvaluateContext(docCtx, "window._bilobaClock && window._bilobaClock.uninstall()", false, nil)
	})
	if err != nil {
		return err
	}
	return uninstallErr
}

// RemoveContext stops installing the clock in new documents, leaving the current ones alone.  This is
// Prepare's half: its about:blank navigation takes the current document, and the clock, away.
func (c *PageClock) RemoveContext(ctx context.Context) error {
	if c.script == "" {
		return nil
	}
	if err := RemoveInitScriptContext(ctx, c.script); err != nil {
		return err
	}
	c.script = ""
	return nil
}

// FormatClockTime renders a page-clock time the way failure messages print it: UTC, to the millisecond.
func FormatClockTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// do runs one synchronous clock operation in every document.
func (c *PageClock) do(ctx context.Context, operation, call string) error {
	return c.apply(ctx, operation, func(docCtx context.Context) (*clockReply, error) {
		return c.evaluate(docCtx, call, false)
	})
}

// apply runs an operation in every document, adopts the state the tab's own document reports, and
// re-registers the new-document script with it so a document that loads next starts there.
func (c *PageClock) apply(ctx context.Context, operation string, run func(context.Context) (*clockReply, error)) error {
	before := c.snapshot
	reply, err := c.inEveryDocument(ctx, operation, func(docCtx context.Context) (*clockReply, error) {
		reply, err := run(docCtx)
		if err != nil || reply != nil {
			return reply, err
		}
		// this document never got the clock: give it one where the others were, then the operation
		installer := &PageClock{snapshot: before}
		if _, err := installer.install(docCtx); err != nil {
			return nil, err
		}
		return run(docCtx)
	})
	if err != nil {
		return err
	}
	if reply != nil {
		c.snapshot = reply.clockSnapshot
	}
	return c.register(ctx)
}

// evaluate calls window._bilobaClock.<call> in one document; a nil reply means the document has no clock.
func (c *PageClock) evaluate(ctx context.Context, call string, await bool) (*clockReply, error) {
	var reply *clockReply
	err := EvaluateContext(ctx, "(() => { const clock = window._bilobaClock; return clock ? clock."+call+" : null })()", await, &reply)
	return reply, err
}

// install evaluates biloba.js and clock.js in one document and installs the clock from the snapshot.
// biloba.js goes first so that, in a document that does not have it yet, it captures the real timers
// before clock.js replaces them; where it is already installed it is a no-op.
func (c *PageClock) install(ctx context.Context) (*clockReply, error) {
	var reply *clockReply
	err := EvaluateContext(ctx, c.installScript(), false, &reply)
	if err == nil && reply == nil {
		err = &Error{Code: CodeJavaScript, Operation: "install the page clock", Message: "clock.js did not install"}
	}
	return reply, err
}

func (c *PageClock) installScript() string {
	encoded, _ := json.Marshal(c.snapshot)
	return bilobaJS + "\n;\n" + clockJS + "\n;\nwindow._bilobaInstallClock(" + string(encoded) + ")"
}

// register replaces the new-document script with one that installs the clock in its current state.
func (c *PageClock) register(ctx context.Context) error {
	if err := c.RemoveContext(ctx); err != nil {
		return err
	}
	script, err := AddInitScriptContext(ctx, c.installScript())
	if err != nil {
		return err
	}
	c.script = script
	return nil
}

// inEveryDocument runs one step in each document of the tab - its own first - and returns the reply
// of the tab's own document.  A document that went away in the meantime is skipped.
func (c *PageClock) inEveryDocument(ctx context.Context, operation string, step func(context.Context) (*clockReply, error)) (*clockReply, error) {
	var main *clockReply
	for i, docCtx := range clockDocuments(ctx) {
		reply, err := step(docCtx)
		if err != nil {
			if i > 0 && frameContextGone(err) {
				continue
			}
			if engineErr, ok := err.(*Error); ok {
				return nil, engineErr
			}
			return nil, contextError(operation, err)
		}
		if i == 0 {
			main = reply
		}
	}
	return main, nil
}

// clockDocuments lists the tab's documents as contexts to evaluate in: the tab's own document (ctx
// itself), then each same-process frame's page world from the attachment's execution-context
// registry, in a stable order.
func clockDocuments(ctx context.Context) []context.Context {
	docs := []context.Context{ctx}
	worlds, _ := ctx.Value(frameWorldsKey{}).(*frameWorlds)
	c := chromedp.FromContext(ctx)
	if worlds == nil || c == nil || c.Target == nil {
		return docs
	}
	// a page target's main frame shares the target's ID
	mainFrame := cdp.FrameID(c.Target.TargetID)
	worlds.mu.RLock()
	ids := make([]cdp.FrameID, 0, len(worlds.byFrame))
	found := map[cdp.FrameID]frameWorld{}
	for id, world := range worlds.byFrame {
		if id != mainFrame && world.uniqueID != "" {
			ids = append(ids, id)
			found[id] = world
		}
	}
	worlds.mu.RUnlock()
	slices.Sort(ids)
	for _, id := range ids {
		docs = append(docs, withExecutionContext(ctx, found[id]))
	}
	return docs
}

// The Session's page clock: one per tab session, installed by InstallClock and removed by Prepare.
// Every method is tab-only and serialized with the session's other operations, and maps onto one
// PageClock operation.

// InstallClock installs the page clock on this tab at start, with time flowing from there at the real
// rate.  A second InstallClock before Prepare fails.
func (s *Session) InstallClock(ctx context.Context, start time.Time) error {
	const operation = "install clock"
	if err := s.tabOnly(operation); err != nil {
		return err
	}
	return s.serial(ctx, operation, func(opCtx context.Context) error {
		if s.clock != nil {
			return &Error{Code: CodeInvalidArgument, Operation: operation, Message: "this tab's page clock is already installed"}
		}
		clock, err := InstallClockContext(opCtx, start)
		if err != nil {
			return err
		}
		s.clock = clock
		return nil
	})
}

// PauseClockAt stops the page clock at t - see PageClock.PauseAtContext.
func (s *Session) PauseClockAt(ctx context.Context, t time.Time) error {
	return s.withClock(ctx, "pause clock", func(c *PageClock, opCtx context.Context) error { return c.PauseAtContext(opCtx, t) })
}

// ResumeClock lets the page clock's time flow again at the real rate.
func (s *Session) ResumeClock(ctx context.Context) error {
	return s.withClock(ctx, "resume clock", func(c *PageClock, opCtx context.Context) error { return c.ResumeContext(opCtx) })
}

// RunClockFor advances the page clock by d, running every callback that falls due, for at most timeout
// of real time (DefaultClockRunTimeout when zero) - see PageClock.RunForContext.  ctx must outlast
// timeout.
func (s *Session) RunClockFor(ctx context.Context, d, timeout time.Duration) error {
	return s.withClock(ctx, "run clock", func(c *PageClock, opCtx context.Context) error { return c.RunForContext(opCtx, d, timeout) })
}

// FastForwardClock jumps the page clock ahead by d, firing each timer that falls due once.
func (s *Session) FastForwardClock(ctx context.Context, d time.Duration) error {
	return s.withClock(ctx, "fast-forward clock", func(c *PageClock, opCtx context.Context) error { return c.FastForwardContext(opCtx, d) })
}

// SetClockFixedTime pins what the page's Date reports to t; timers keep running.
func (s *Session) SetClockFixedTime(ctx context.Context, t time.Time) error {
	return s.withClock(ctx, "set clock fixed time", func(c *PageClock, opCtx context.Context) error { return c.SetFixedTimeContext(opCtx, t) })
}

// SetClockSystemTime moves the page clock to t without firing any timer.
func (s *Session) SetClockSystemTime(ctx context.Context, t time.Time) error {
	return s.withClock(ctx, "set clock system time", func(c *PageClock, opCtx context.Context) error { return c.SetSystemTimeContext(opCtx, t) })
}

// ClockState reports this tab's page clock as of its last operation, and false when none is
// installed.  It sends nothing to Chrome, so a failure report can say the clock was paused without
// another round trip.
func (s *Session) ClockState() (ClockState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.clock == nil {
		return ClockState{}, false
	}
	return s.clock.State(), true
}

func (s *Session) withClock(ctx context.Context, operation string, run func(*PageClock, context.Context) error) error {
	if err := s.tabOnly(operation); err != nil {
		return err
	}
	return s.serial(ctx, operation, func(opCtx context.Context) error {
		if s.clock == nil {
			return &Error{Code: CodeInvalidArgument, Operation: operation, Message: "this tab has no page clock: install one first"}
		}
		return run(s.clock, opCtx)
	})
}

// removeClock is Prepare's half of the clock's lifecycle: stop installing it in new documents.  The
// about:blank navigation that follows takes the current ones away.  No round trip without a clock.
func (s *Session) removeClock(ctx context.Context) error {
	if s.clock == nil {
		return nil
	}
	if err := s.clock.RemoveContext(ctx); err != nil {
		return err
	}
	s.clock = nil
	return nil
}

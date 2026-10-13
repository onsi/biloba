package biloba

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/biloba/engine"
)

/*
Clock is a tab's page clock: fake timers for the page's own code, so a spec can test time-dependent UI
- a countdown, a "5 minutes ago" label, a debounce, a session that expires - without waiting for it.
[Biloba.InstallClock] installs it and returns one; [Biloba.Clock] returns one for a tab that has it.

A Clock acts through the view of the tab it came from, so b.WithTimeout(d).Clock().RunFor(...) gives
RunFor d of real time.

Read https://onsi.github.io/biloba/#controlling-time to learn more about the page clock
*/
type Clock struct {
	b *Biloba
}

// tabClock is a tab's installed page clock, kept in tabState so every view of the tab shares it.
type tabClock struct {
	page *engine.PageClock
	// active is true from InstallClock until the clock is removed - by the spec-end cleanup, or by
	// Prepare when that cleanup did not land.
	active bool
	// noted records that a poll's failure message already carries the paused-clock note, so the failure
	// report does not repeat it.
	noted bool
}

/*
InstallClock() installs the page clock on this tab and its frames, starting at t0 with time flowing
at the real rate, and returns the [Clock] that controls it:

	clock := b.InstallClock(time.Date(2024, 2, 2, 8, 0, 0, 0, time.UTC))
	b.Navigate(fixtureServer + "/countdown.html")
	clock.PauseAt(time.Date(2024, 2, 2, 10, 0, 0, 0, time.UTC))
	clock.RunFor(5 * time.Second)

It fakes the page's Date, performance.now, setTimeout/clearTimeout, setInterval/clearInterval,
requestAnimationFrame/cancelAnimationFrame, and requestIdleCallback/cancelIdleCallback.  CSS animations
and transitions are not affected.  Biloba itself keeps running on real time.

Install before Navigate to cover everything the page does; installed after, the clock replaces those
functions in the current document, but timers the page already scheduled keep running on real time.
Every document the tab loads afterwards gets the clock where the others are.  The clock is removed
when the spec ends.  A second InstallClock on the same tab fails, and InstallClock applies to a tab,
not a frame handle.

Read https://onsi.github.io/biloba/#controlling-time to learn more about the page clock
*/
func (b *Biloba) InstallClock(t0 time.Time) *Clock {
	b.gt.Helper()
	if b.refusedOnFrame("InstallClock") {
		return nil
	}
	b.guardConfig("InstallClock")
	b.lock.Lock()
	existing := b.state.clock
	b.lock.Unlock()
	if existing != nil && existing.active {
		b.gt.Fatalf("InstallClock: this tab's page clock is already installed - use the Clock InstallClock returned, or b.Clock()")
		return nil
	}
	var page *engine.PageClock
	err := b.runEngine("install the page clock", func(ctx context.Context) error {
		var err error
		page, err = engine.InstallClockContext(ctx, t0)
		return err
	})
	if err != nil {
		b.gt.Fatalf("failed to install the page clock:\n%s", err.Error())
		return nil
	}
	installed := &tabClock{page: page, active: true}
	b.lock.Lock()
	b.state.clock = installed
	b.lock.Unlock()
	b.gt.DeferCleanup(func() {
		if err := b.uninstallClock(installed); err != nil {
			b.gt.Fatalf("failed to remove the page clock:\n%s", err.Error())
		}
	})
	return &Clock{b: b}
}

/*
Clock() returns this tab's page clock, installed earlier in the spec with [Biloba.InstallClock], acting
through this view of the tab - so b.WithTimeout(time.Minute).Clock().RunFor(time.Hour) gives RunFor a
minute.  It fails the spec when the tab has no clock, and applies to a tab, not a frame handle.

Read https://onsi.github.io/biloba/#controlling-time to learn more about the page clock
*/
func (b *Biloba) Clock() *Clock {
	b.gt.Helper()
	if b.refusedOnFrame("Clock") {
		return nil
	}
	if b.activeClock("Clock") == nil {
		return nil
	}
	return &Clock{b: b}
}

/*
PauseAt() stops the page's time at t: from then on no timer or animation frame runs until [Clock.RunFor],
[Clock.FastForward], or [Clock.Resume].  When t is ahead of the clock it gets there the way FastForward
does, firing each timer that falls due on the way once.  When t is behind it (time flows after
InstallClock, so pausing at the install time is usually going back a little) the clock moves back
without firing anything.

While the clock is paused, Biloba's own polling keeps running on real time but the page does not: a
poll that waits for the page's timers to change something times out, and its failure says the clock
is paused.

Read https://onsi.github.io/biloba/#controlling-time to learn more about the page clock
*/
func (c *Clock) PauseAt(t time.Time) {
	c.b.gt.Helper()
	c.operate("PauseAt", func(ctx context.Context, page *engine.PageClock) error { return page.PauseAtContext(ctx, t) })
}

/*
Resume() lets the page's time flow again at the real rate, from wherever the clock is.

Read https://onsi.github.io/biloba/#controlling-time to learn more about the page clock
*/
func (c *Clock) Resume() {
	c.b.gt.Helper()
	c.operate("Resume", func(ctx context.Context, page *engine.PageClock) error { return page.ResumeContext(ctx) })
}

/*
RunFor() advances the page clock by d, running every timer and animation frame that falls due, in
order, each in its own task so the page's promise callbacks run in between.  It returns once they have
all run.  Time keeps flowing afterwards if it was flowing before; a paused clock stays paused.

RunFor gives up after 10 seconds of real time by default and fails the spec with how far it got - a
page that keeps scheduling work makes a long RunFor slow.  b.WithTimeout(d).Clock().RunFor(...) changes
that, and WithContext aborts it; [Clock.FastForward] skips ahead without running every callback.

Read https://onsi.github.io/biloba/#controlling-time to learn more about the page clock
*/
func (c *Clock) RunFor(d time.Duration) {
	b := c.b
	b.gt.Helper()
	b.guardConfig("RunFor", knobTimeout, knobContext)
	installed := b.activeClock("RunFor")
	if installed == nil {
		return
	}
	if d < 0 {
		b.gt.Fatalf("RunFor: the duration must not be negative, got %s", d)
		return
	}
	budget := b.waitingTimeout(engine.DefaultClockRunTimeout)
	// The page enforces the budget itself and reports how far it got; the context only has to outlast it.
	ctx, cancel := b.cdpContext(budget + cdpTimeout)
	defer cancel()
	err := b.runEngineIn(ctx, budget+cdpTimeout, "run the page clock", func(ctx context.Context) error {
		return installed.page.RunForContext(ctx, d, budget)
	})
	if err != nil {
		b.gt.Fatalf("RunFor:\n%s", err.Error())
	}
}

/*
FastForward() jumps the page clock ahead by d and fires each timer that fell due along the way once, at
the new time - the way a laptop's timers catch up when it wakes from sleep.  An interval that should
have fired ten times fires once.

Read https://onsi.github.io/biloba/#controlling-time to learn more about the page clock
*/
func (c *Clock) FastForward(d time.Duration) {
	c.b.gt.Helper()
	if d < 0 {
		c.b.gt.Fatalf("FastForward: the duration must not be negative, got %s", d)
		return
	}
	c.operate("FastForward", func(ctx context.Context, page *engine.PageClock) error { return page.FastForwardContext(ctx, d) })
}

/*
SetFixedTime() pins the time the page's Date reports (Date.now() and new Date()) to t.  Timers and
animation frames keep running on the clock's own time.  [Clock.SetSystemTime] unpins it.

Read https://onsi.github.io/biloba/#controlling-time to learn more about the page clock
*/
func (c *Clock) SetFixedTime(t time.Time) {
	c.b.gt.Helper()
	c.operate("SetFixedTime", func(ctx context.Context, page *engine.PageClock) error { return page.SetFixedTimeContext(ctx, t) })
}

/*
SetSystemTime() moves the page clock's time to t, forwards or backwards, without firing any timer: a
pending timer keeps the delay it had left.  It is the wall clock changing under the page (a time zone
trip, an NTP correction).  It also unpins a time [Clock.SetFixedTime] pinned.

Read https://onsi.github.io/biloba/#controlling-time to learn more about the page clock
*/
func (c *Clock) SetSystemTime(t time.Time) {
	c.b.gt.Helper()
	c.operate("SetSystemTime", func(ctx context.Context, page *engine.PageClock) error { return page.SetSystemTimeContext(ctx, t) })
}

// operate runs one of the clock's one-shot operations: no poll-config knobs, one engine call.
func (c *Clock) operate(method string, run func(context.Context, *engine.PageClock) error) {
	b := c.b
	b.gt.Helper()
	b.guardConfig(method)
	installed := b.activeClock(method)
	if installed == nil {
		return
	}
	err := b.runEngine("control the page clock", func(ctx context.Context) error {
		return run(ctx, installed.page)
	})
	if err != nil {
		b.gt.Fatalf("%s:\n%s", method, err.Error())
	}
}

// activeClock returns this tab's installed clock, failing the spec on behalf of method when there is
// none.
func (b *Biloba) activeClock(method string) *tabClock {
	b.gt.Helper()
	b.lock.Lock()
	installed := b.state.clock
	b.lock.Unlock()
	if installed == nil || !installed.active {
		b.gt.Fatalf("%s: this tab has no page clock - call InstallClock first (the clock is removed when the spec ends)", method)
		return nil
	}
	return installed
}

// uninstallClock is the spec-end cleanup: restore the real timers in the tab's documents and stop
// installing the clock in new ones.  A tab the spec closed has nothing left to clean up.
//
// It is also where a failed spec's report says the clock was paused, when no poll's failure already
// did - the spec's own Eventually, say.  After this the tab has no clock to report on.
func (b *Biloba) uninstallClock(installed *tabClock) error {
	if !installed.active {
		return nil
	}
	if b.gt.Failed() {
		if note := b.pausedClockReport(); note != "" {
			suffix := ""
			if tab := b.tab(); tab != nil && !tab.isRootTab() {
				suffix = fmt.Sprintf(" for tab: %s", tab.targetID)
			}
			b.gt.AddReportEntryVisibilityFailureOrVerbose("Page clock paused"+suffix, note)
		}
	}
	installed.active = false
	b.lock.Lock()
	if b.state.clock == installed {
		b.state.clock = nil
	}
	b.lock.Unlock()
	if b.Context.Err() != nil {
		return nil
	}
	return b.runEngine("uninstall the page clock", func(ctx context.Context) error {
		return installed.page.UninstallContext(ctx)
	})
}

// removeLeakedClock is Prepare's half of the clock's lifecycle, for a clock whose spec-end cleanup
// did not land: stop installing it in new documents.  Prepare's about:blank navigation takes the
// current document, and the clock, away.  No round trip unless a clock is still installed.
func (b *Biloba) removeLeakedClock(installed *tabClock) {
	if installed == nil || !installed.active {
		return
	}
	installed.active = false
	if err := b.runEngine("remove the page clock", func(ctx context.Context) error {
		return installed.page.RemoveContext(ctx)
	}); err != nil {
		b.gt.Printf("Failed to remove a leaked page clock during Prepare():\n  %s\n", err.Error())
	}
}

// pausedClockNote is the line a failure gets when this tab's page clock is paused, "" otherwise.  It
// reads only what the last clock operation reported, so it costs nothing without a clock.
func (b *Biloba) pausedClockNote() string {
	tab := b.tab()
	if tab == nil {
		return ""
	}
	tab.lock.Lock()
	installed := tab.state.clock
	tab.lock.Unlock()
	if installed == nil || !installed.active {
		return ""
	}
	state := installed.page.State()
	if !state.Paused {
		return ""
	}
	return fmt.Sprintf("This tab's page clock is paused at %s; timers and animation frames won't run until you call RunFor, FastForward, or Resume.", engine.FormatClockTime(state.Now))
}

// notePausedClock returns pausedClockNote and remembers that a failure carried it.
func (b *Biloba) notePausedClock() string {
	note := b.pausedClockNote()
	if note != "" {
		tab := b.tab()
		tab.lock.Lock()
		tab.state.clock.noted = true
		tab.lock.Unlock()
	}
	return note
}

// pausedClockReport is the failure report's paused-clock entry: the note, unless a poll's failure
// message already carried it.
func (b *Biloba) pausedClockReport() string {
	note := b.pausedClockNote()
	if note == "" {
		return ""
	}
	tab := b.tab()
	tab.lock.Lock()
	defer tab.lock.Unlock()
	if tab.state.clock.noted {
		return ""
	}
	return note
}

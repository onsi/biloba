package biloba

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

/*
The CDP backstop.

Every command Biloba sends to Chrome used to run on b.Context, which carries no deadline.  A Chrome
that stops answering therefore did not fail a spec - it hung the goroutine that issued the command,
forever.  The poll-config knobs do not save you: WithTimeout bounds Gomega's Eventually loop, but a
Runtime.evaluate that never returns blocks *inside* the matcher call, and Gomega cannot preempt a
synchronous blocking call.  So the deadline never gets a chance to fire and the whole suite dies on
its own timeout with no failing spec to point at.

runCDP fixes that by putting a deadline on every low-level command.  The deadline is a BACKSTOP, not
a poll bound: Gomega's Eventually is what bounds a poll, and this exists so that a wedged CDP call
fails *at all*.  It is therefore deliberately generous - far longer than any healthy operation, so it
never trips on a slow-but-alive Chrome (principle 1: a false timeout on a loaded CI box would be
worse than the bug it guards).

Because it is a backstop and not a wait, it deliberately does NOT honor WithTimeout.  That keeps it
coherent with the four-bucket model rather than bolted beside it:

  - Polling methods allow WithTimeout, but there it means "how long to keep RETRYING" - Eventually's
    business.  Letting it also shorten each individual CDP call would regress a real case: a healthy
    call that legitimately takes longer than a tight WithTimeout currently still completes (Gomega
    accepts a match that arrives after the deadline), and cancelling it mid-flight would turn a
    passing spec into a failing one on a loaded machine.
  - Snapshots and one-shot mutations reject WithTimeout outright (guardConfig), so there is nothing
    to honor - but they still get the backstop, which is the point: a bucket that takes no knobs
    still must not hang.
  - Waiting commands (Navigate, the screenshot captures, ResponseHold.Await) are the one bucket whose
    bound IS the user's wait, and they already have waitingContext - which is cdpContext plus the
    WithTimeout override the bucket allows.

WithContext is different and IS honored everywhere, because it means "abort" rather than "wait this
long": tying it in lets a cancelled context kill a command already in flight.
*/

// cdpTimeout is the backstop deadline for a single low-level CDP command.  Every command Biloba
// issues on its own behalf - evaluating JavaScript, dispatching input, emulating a viewport, reading
// cookies, answering a paused request - is expected back from a healthy Chrome in milliseconds, so
// 30s is roughly three orders of magnitude of headroom.  It matches navigationTimeout, which was
// picked for this same failure class (see navigation.go) after real Chrome occasionally failed to
// acknowledge a navigation under parallel/CI load.  A var (not a const) only so cdp_test.go can
// shrink it via SetCDPTimeoutsForTest.
var cdpTimeout = 30 * time.Second

// cdpAwaitTimeout is the backstop for a Runtime.evaluate that awaits a promise - RunAsync/RunErrAsync.
// It is a genuinely different class from every other command: the duration is set by the page's own
// JavaScript, not by Chrome's responsiveness, so `return await fetch(...)` against a slow endpoint is
// a healthy call that can legitimately outlast cdpTimeout.  Two minutes is far beyond any await a
// browser test should contain while still being finite, which is the whole point - today a promise
// that never settles hangs the suite forever.  A var only so cdp_test.go can shrink it.
var cdpAwaitTimeout = 2 * time.Minute

// simulateWedgedCDP, when non-nil and returning true, makes runCDP wait out its own deadline instead
// of dispatching to Chrome.  It is the seam cdp_test.go uses to exercise the wedge path without
// actually wedging a renderer - a real infinite-loop renderer would keep a CPU pegged for the rest of
// the run, which under `make stress-test`'s 41 repeats is an unacceptable price for one spec.  nil
// (the default) means "dispatch for real".  Overridden from tests via SetWedgedCDPForTest.
var simulateWedgedCDP func() bool

// cdpContext bounds a single low-level CDP command with timeout.  The returned context is always
// parented on b.Context so chromedp's executor stays in the chain (a user's WithContext is typically
// a plain context.Background-derived context with no executor); a WithContext is honored for
// cancellation by tying it to the returned context via context.AfterFunc.
func (b *Biloba) cdpContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	b.ensureChromedpAllocated()
	ctx, cancel := context.WithTimeout(b.Context, timeout)
	if b.pollingCtx != nil {
		stop := context.AfterFunc(b.pollingCtx, cancel)
		return ctx, func() { stop(); cancel() }
	}
	return ctx, cancel
}

// ensureChromedpAllocated makes sure this tab's browser and target exist BEFORE a bounded context is
// built on top of them.  chromedp allocates both lazily, inside the first chromedp.Run on a context -
// and that Run's context then owns them: initContextBrowser ties the browser to it, and newTarget
// runs the target's event loop under it.  So a deadline on the first Run does not bound a hang, it
// hands chromedp a context it will cancel and takes the tab (or the whole browser) down with it.
// That is not theoretical: b.RunErr("1") is exactly the first-contact probe bootstrapIsolatedTab and
// NewTab use, and bounding it detached every tab the moment its first command returned.
//
// One extra context-value lookup per command, short-circuiting as soon as the target exists.
func (b *Biloba) ensureChromedpAllocated() {
	if c := chromedp.FromContext(b.Context); c == nil || c.Target != nil {
		return
	}
	chromedp.Run(b.Context)
}

// runCDP runs actions against Chrome under the backstop deadline, turning a wedged, crashed, or
// vanished browser into an actionable error instead of a hang.  what names the operation in the
// failure message and reads as the tail of "Chrome did not ___": "evaluate JavaScript in the page",
// "dispatch keyboard input", and so on.
func (b *Biloba) runCDP(what string, actions ...chromedp.Action) error {
	return b.runCDPWithin(cdpTimeout, what, actions...)
}

// runCDPWithin is runCDP with an explicit deadline, for the one class (an awaited promise) whose
// duration the page rather than Chrome controls.
func (b *Biloba) runCDPWithin(timeout time.Duration, what string, actions ...chromedp.Action) error {
	ctx, cancel := b.cdpContext(timeout)
	defer cancel()
	return b.runCDPIn(ctx, timeout, what, actions...)
}

// runCDPIn runs actions in an already-bounded context - the waiting commands build theirs with
// waitingContext - so every path into Chrome funnels through one place that can diagnose a failure.
func (b *Biloba) runCDPIn(ctx context.Context, timeout time.Duration, what string, actions ...chromedp.Action) error {
	return b.runEngineIn(ctx, timeout, what, func(runCtx context.Context) error {
		return chromedp.Run(runCtx, actions...)
	})
}

// runEngine applies Biloba's CDP backstop and diagnosis around a runner-neutral engine operation.
func (b *Biloba) runEngine(what string, run func(context.Context) error) error {
	ctx, cancel := b.cdpContext(cdpTimeout)
	defer cancel()
	return b.runEngineIn(ctx, cdpTimeout, what, run)
}

// runEngineIn is the engine-operation counterpart to runCDPIn for callers that already own the
// waiting context and timeout.
func (b *Biloba) runEngineIn(ctx context.Context, timeout time.Duration, what string, run func(context.Context) error) error {
	var err error
	if simulateWedgedCDP != nil && simulateWedgedCDP() {
		<-ctx.Done()
		err = ctx.Err()
	} else {
		err = run(ctx)
	}
	return b.diagnoseCDPError(what, timeout, err)
}

// diagnoseCDPError explains WHY a command failed when Biloba can tell, and otherwise hands the error
// back untouched.  A deadline alone stops the hang; naming the cause is what makes the failure
// actionable, so the three cases the issue asks for get their own vocabulary - page_crashed,
// browser_gone, deadline_exceeded.  Every other error (a thrown JS exception, a missing binding, a
// transient "target navigated or closed") passes through verbatim: callers pattern-match on some of
// them, and wrapping an ordinary bug in wedge language would be worse than saying nothing.
func (b *Biloba) diagnoseCDPError(what string, timeout time.Duration, err error) error {
	if err == nil {
		return nil
	}
	// Each diagnosis is built as a plain string and handed to wrapCDPError, which does the one
	// error-wrapping fmt.Errorf in this file.
	if b.pageCrashed() {
		return wrapCDPError(fmt.Sprintf("page_crashed: this tab's renderer crashed, so Chrome could not %s.\n"+
			"Chrome reported Inspector.targetCrashed for this target - everything the page held is gone.\n"+
			"Navigate the tab again to get a fresh renderer, or run the rest of the spec on a new tab.", what), err)
	}
	if b.browserGone() {
		return wrapCDPError(fmt.Sprintf("browser_gone: the connection to Chrome is closed, so Chrome could not %s.\n"+
			"The browser process is no longer there - it crashed, ran out of memory, or was killed.", what), err)
	}
	if b.pollingCtx != nil && b.pollingCtx.Err() != nil {
		// The caller's own WithContext expired or was cancelled; that is their deadline, not ours.
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return wrapCDPError(fmt.Sprintf("deadline_exceeded: Chrome did not %s within %s.\n"+
			"Biloba bounds every browser command so an unresponsive Chrome fails the spec instead of hanging the suite.\n"+
			"Chrome is wedged, badly overloaded, or the page is stuck in a long-running synchronous script.", what, timeout), err)
	}
	return err
}

// wrapCDPError keeps the underlying error wrapped rather than replaced, so a caller that
// pattern-matches on it still can - navigateWithStatus tests errors.Is(err, context.DeadlineExceeded)
// to print its own bespoke timeout message.
func wrapCDPError(diagnosis string, err error) error {
	return fmt.Errorf("%s\nUnderlying error: %w", diagnosis, err)
}

// pageCrashed reports whether Chrome has told us this tab's renderer died (Inspector.targetCrashed).
// The flag is cleared by a subsequent navigation - which gives the target a fresh renderer - and by
// Prepare, so it never outlives the crash it describes.
func (b *Biloba) pageCrashed() bool {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.state.targetCrashed
}

// browserGone reports whether the connection to the Chrome process itself has gone away.  chromedp
// cancels every context under a browser when its websocket closes, so a done root-tab context means
// the browser - not just this tab - is finished.
func (b *Biloba) browserGone() bool {
	return b.root != nil && b.root.Context != nil && b.root.Context.Err() != nil
}

/*
What is deliberately NOT bounded, and why.

Chrome's own bring-up - the first-contact probes in SpinUpChrome and bootstrapIsolatedTab - stays
unbounded.  chromedp allocates the browser (and its target) lazily, inside the FIRST chromedp.Run on
a context, which makes that Run's context the owner of the Chrome process: cancel it and chromedp
kills the browser.  A deadline there does not bound a hang, it tears Chrome down mid-suite - that is
not a guess, it was tried, and every spec then failed to connect.  Browser start-up has its own bound
anyway: chromedp.WSURLReadTimeout, which SpinUpChrome sets to 60s.

The rule that falls out is worth carrying to any new call site: a context may bound a command only
once the browser and target on it are already allocated - which, for every method on a *Biloba, they
are.
*/

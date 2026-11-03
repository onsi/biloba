package engine

import (
	"context"
	"sync"
)

// CrashSignal records that Chrome announced a tab's renderer crashed (Inspector.targetCrashed or
// Target.targetCrashed).  Both clients keep one per tab: the Go client and the daemon's Session use it
// to refuse new commands on the dead renderer, to interrupt the ones already waiting on it, and to
// retry the navigation that recovers it.
//
// It is a channel rather than a bool because CDP does not fail calls to a dead renderer, it stops
// answering them: a command already waiting when the crash arrives would otherwise sit there until its
// deadline and report a timeout.  The zero value is ready to use, and it is safe to use from Chrome's
// event goroutine and the command goroutines at once.
type CrashSignal struct {
	mu      sync.Mutex
	crashed chan struct{}
}

func (c *CrashSignal) signal() chan struct{} {
	if c.crashed == nil {
		c.crashed = make(chan struct{})
	}
	return c.crashed
}

// Done is closed once the crash is announced, and stays closed until Clear.
func (c *CrashSignal) Done() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.signal()
}

// Mark records the crash.  Marking twice is harmless: Chrome can announce one crash on both the
// page session and the browser connection.
func (c *CrashSignal) Mark() {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.signal():
	default:
		close(c.crashed)
	}
}

// Clear records that the target has a live renderer again.  It does nothing when no crash is
// recorded, so a command already watching Done keeps watching the channel a later crash closes.
func (c *CrashSignal) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.signal():
		c.crashed = make(chan struct{})
	default:
	}
}

// Crashed reports whether a crash is recorded.
func (c *CrashSignal) Crashed() bool {
	select {
	case <-c.Done():
		return true
	default:
		return false
	}
}

// Interrupt returns a context that is cancelled when the crash is announced, so a command waiting on
// the renderer when it dies returns then instead of at its deadline.  Callers must call the returned
// cancel.
func (c *CrashSignal) Interrupt(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	crashed := c.Done()
	go func() {
		select {
		case <-crashed:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// NavigateRecoveringContext is NavigateContext for a tab whose renderer may have crashed.  A
// navigation gives the target a fresh renderer, which is how a crashed page recovers, but Chrome needs
// a moment to spawn one, and a navigation sent before it exists comes back ERR_ABORTED.  So a
// navigation that fails while a crash is recorded is retried once.  This is what makes "navigate again
// to recover it" true advice rather than something that works only if you happen to wait first.
func NavigateRecoveringContext(ctx context.Context, destination string, crash *CrashSignal) (NavigationResult, error) {
	result, err := NavigateContext(ctx, destination)
	if err != nil && crash.Crashed() {
		result, err = NavigateContext(ctx, destination)
	}
	return result, err
}

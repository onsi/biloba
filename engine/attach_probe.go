package engine

import (
	"context"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

/*
AttachProbe runs the attempts that get chromedp to attach a tab (the first Run on a tab's context
attaches it) and records whether one did, so that discarding the tab closes it only if it was
attached, as chromedp's own cancellation did before v0.21.

chromedp sets the context's Target on the goroutine of the Run that attaches, and then runs that Run's
setup commands.  So an attempt can have attached the tab while it is still running, and only the
attempt's goroutine can read that without a race.  A cleanup asks through Settle, which waits for the
attempt to return.
*/
type AttachProbe struct {
	mu       sync.Mutex
	running  int
	attached bool
	settling bool
	idle     chan struct{} // made by Settle while an attempt runs, closed when the last one returns
}

// Try runs attempt for the tab context tabCtx on a goroutine of its own, and delivers its error.  Once
// Settle has been called it runs nothing and delivers context.Canceled.
func (p *AttachProbe) Try(tabCtx context.Context, attempt func() error) <-chan error {
	done := make(chan error, 1)
	p.mu.Lock()
	if p.settling {
		p.mu.Unlock()
		done <- context.Canceled
		return done
	}
	p.running++
	p.mu.Unlock()
	go func() {
		err := attempt()
		c := chromedp.FromContext(tabCtx)
		attached := c != nil && c.Target != nil // set on this goroutine, by the attempt's Run
		p.mu.Lock()
		p.attached = p.attached || attached
		p.running--
		if p.running == 0 && p.idle != nil {
			close(p.idle)
			p.idle = nil
		}
		p.mu.Unlock()
		done <- err
	}()
	return done
}

// attachSettleWait bounds how long Settle waits for an attempt that is still running.  Cancelling the
// tab's context ends an attempt at once, because every chromedp call watches it.
const attachSettleWait = time.Second

// Settle stops new attempts and calls then with whether an attempt attached the tab, once none is
// running.  Call it after cancelling the tab's context, which ends a running attempt.  It waits up to
// attachSettleWait for that attempt; if the attempt is still running after that, then is called
// when it returns.
func (p *AttachProbe) Settle(then func(attached bool)) {
	p.mu.Lock()
	p.settling = true
	if p.running == 0 {
		attached := p.attached
		p.mu.Unlock()
		then(attached)
		return
	}
	if p.idle == nil {
		p.idle = make(chan struct{})
	}
	idle := p.idle
	p.mu.Unlock()
	settled := func() {
		p.mu.Lock()
		attached := p.attached
		p.mu.Unlock()
		then(attached)
	}
	select {
	case <-idle:
		settled()
	case <-time.After(attachSettleWait):
		go func() {
			<-idle
			settled()
		}()
	}
}

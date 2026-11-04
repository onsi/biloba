package engine

import (
	"sync"
	"sync/atomic"
)

/*
Handoff hands values from a listener, which must not wait (see ListenTarget), to a goroutine that
handles them in the order they were handed over.  The queue has no bound, so Push never waits.  The
goroutine runs only while values are waiting, so a Handoff nobody closes holds no goroutine.
*/
type Handoff[T any] struct {
	mu      sync.Mutex
	handle  func(T)
	pending []T
	running bool
	closed  bool
	idle    chan struct{} // made by a Drain or Close while values are handled, closed when they are
}

func NewHandoff[T any](handle func(T)) *Handoff[T] {
	return &Handoff[T]{handle: handle}
}

// Push queues v, unless the Handoff is closed.
func (h *Handoff[T]) Push(v T) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.pending = append(h.pending, v)
	if !h.running {
		h.running = true
		go h.run()
	}
}

// Drain returns once every value pushed before it has been handled.
func (h *Handoff[T]) Drain() {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}
	if h.idle == nil {
		h.idle = make(chan struct{})
	}
	idle := h.idle
	h.mu.Unlock()
	<-idle
}

// Close stops accepting values, returns once every value pushed before it has been handled, and then
// lets go of the handler: a listener chromedp has not dropped yet holds nothing through it.
func (h *Handoff[T]) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.Drain()
	h.mu.Lock()
	h.handle = nil
	h.mu.Unlock()
}

// liveHandoffWorkers counts the Handoff goroutines running, for specs.
var liveHandoffWorkers atomic.Int64

func (h *Handoff[T]) run() {
	liveHandoffWorkers.Add(1)
	defer liveHandoffWorkers.Add(-1)
	for {
		h.mu.Lock()
		batch, handle := h.pending, h.handle
		h.pending = nil
		if len(batch) == 0 {
			h.running = false
			if h.idle != nil {
				close(h.idle)
				h.idle = nil
			}
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()
		for _, v := range batch {
			handle(v)
		}
	}
}

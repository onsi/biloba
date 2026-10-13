package engine

import (
	"sync"
	"sync/atomic"

	"github.com/chromedp/cdproto/cdp"
	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
)

// EventGeneration counts a tab's resets.  A reset advances it before it navigates the page it is
// clearing away, so everything that page creates from then on can be told apart from what the next
// page creates.
type EventGeneration struct{ n atomic.Uint64 }

// Advance starts a new generation.  Call it before the navigation that unloads the old page.
func (g *EventGeneration) Advance() { g.n.Add(1) }

// Current reports the generation events are attributed to right now.
func (g *EventGeneration) Current() uint64 { return g.n.Load() }

// EventOrigins remembers the generation each execution context, document loader, and request on one
// CDP target was created in, so a listener can drop the events a page left behind by a reset raises
// afterwards: an in-flight fetch the navigation aborts ("Uncaught (in promise) TypeError: Failed to
// fetch"), its console output, the request's failure.  Attribution is by origin, not by when the
// event arrives, so a late event from the old page is dropped and an early one from the new page is
// kept.  Contexts, loaders, and requests it never saw created are treated as current.
//
// Feed it every event from the target, in order, through Stale.  It sends no CDP commands.
type EventOrigins struct {
	generation *EventGeneration

	mu       sync.Mutex
	contexts map[runtime.ExecutionContextID]uint64
	loaders  map[cdp.LoaderID]uint64
	requests map[network.RequestID]uint64
	pruned   uint64
}

// NewEventOrigins tracks one target's events against generation, which a tab's targets share.
func NewEventOrigins(generation *EventGeneration) *EventOrigins {
	return &EventOrigins{
		generation: generation,
		contexts:   map[runtime.ExecutionContextID]uint64{},
		loaders:    map[cdp.LoaderID]uint64{},
		requests:   map[network.RequestID]uint64{},
	}
}

// Stale records what event creates and reports whether it comes from a page an earlier generation
// left behind.  Events it does not attribute (dialogs, paused requests, downloads, ...) are never stale.
func (o *EventOrigins) Stale(event any) bool {
	current := o.generation.Current()
	o.mu.Lock()
	defer o.mu.Unlock()
	o.prune(current)
	switch event := event.(type) {
	case *runtime.EventExecutionContextCreated:
		if event.Context != nil {
			o.contexts[event.Context.ID] = current
		}
	case *runtime.EventConsoleAPICalled:
		return o.staleContext(event.ExecutionContextID, current)
	case *runtime.EventExceptionThrown:
		return event.ExceptionDetails != nil && o.staleContext(event.ExceptionDetails.ExecutionContextID, current)
	case *network.EventRequestWillBeSent:
		generation := current
		if event.LoaderID != "" {
			if loader, ok := o.loaders[event.LoaderID]; ok {
				generation = loader
			} else {
				o.loaders[event.LoaderID] = current
			}
		}
		// a redirect hop keeps the generation its request started in
		if started, ok := o.requests[event.RequestID]; ok {
			generation = started
		}
		o.requests[event.RequestID] = generation
		return generation < current
	case *network.EventResponseReceived:
		return o.staleRequest(event.RequestID, current)
	case *network.EventLoadingFinished:
		return o.staleRequest(event.RequestID, current)
	case *network.EventLoadingFailed:
		return o.staleRequest(event.RequestID, current)
	case *cdplog.EventEntryAdded:
		return event.Entry != nil && event.Entry.NetworkRequestID != "" && o.staleRequest(event.Entry.NetworkRequestID, current)
	}
	return false
}

func (o *EventOrigins) staleContext(id runtime.ExecutionContextID, current uint64) bool {
	generation, ok := o.contexts[id]
	return ok && generation < current
}

func (o *EventOrigins) staleRequest(id network.RequestID, current uint64) bool {
	generation, ok := o.requests[id]
	return ok && generation < current
}

// prune forgets what two or more generations back created: a reset's stragglers arrive while the
// next page loads, not a whole spec later.
func (o *EventOrigins) prune(current uint64) {
	if current == o.pruned {
		return
	}
	o.pruned = current
	for id, generation := range o.contexts {
		if generation+1 < current {
			delete(o.contexts, id)
		}
	}
	for id, generation := range o.loaders {
		if generation+1 < current {
			delete(o.loaders, id)
		}
	}
	for id, generation := range o.requests {
		if generation+1 < current {
			delete(o.requests, id)
		}
	}
}

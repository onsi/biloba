package engine

import (
	"sync"

	"github.com/chromedp/cdproto/cdp"
	cdplog "github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
)

// documentBoundary tells the events of the document a Prepare left behind apart from the next spec's.
// Prepare navigates the tab away, and the old document keeps reporting while it goes: a fetch it had in
// flight rejects ("Uncaught (in promise) TypeError: Failed to fetch"), a request it had started fails,
// Chrome logs the failed load.  Chrome emits those before it tears the document down, but the listener
// can process them after Prepare has returned, so neither timing nor the event generation can catch
// them.  The document they come from can: an execution context, a loader, or a request that already
// existed when Prepare reset the session belongs to the previous spec.
//
// The listener feeds observe every event in stream order, and Prepare calls reset.  An execution
// context stays stale until the listener sees it destroyed - nothing can come from it afterwards, and
// Chrome may reuse its ID.  Loader and request IDs are never reused, so they stay stale until the next
// reset.  A Log.entryAdded carries no execution context: it is stale when it names a stale request, or
// while any stale context is still alive.
type documentBoundary struct {
	mu                      sync.Mutex
	live, staleContexts     map[runtime.ExecutionContextID]struct{}
	loaders, staleLoaders   map[cdp.LoaderID]struct{}
	requests, staleRequests map[network.RequestID]struct{}
}

func (d *documentBoundary) observe(event any) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch event := event.(type) {
	case *runtime.EventExecutionContextCreated:
		if d.live == nil {
			d.live = map[runtime.ExecutionContextID]struct{}{}
		}
		d.live[event.Context.ID] = struct{}{}
	case *runtime.EventExecutionContextDestroyed:
		delete(d.live, event.ExecutionContextID)
		delete(d.staleContexts, event.ExecutionContextID)
	case *runtime.EventExecutionContextsCleared:
		clear(d.live)
		clear(d.staleContexts)
	case *network.EventRequestWillBeSent:
		if _, stale := d.staleLoaders[event.LoaderID]; stale {
			// a request the old document starts after the reset: its response, finish, failure, and
			// log entry are the old document's too
			if d.staleRequests == nil {
				d.staleRequests = map[network.RequestID]struct{}{}
			}
			d.staleRequests[event.RequestID] = struct{}{}
			return
		}
		if d.loaders == nil {
			d.loaders, d.requests = map[cdp.LoaderID]struct{}{}, map[network.RequestID]struct{}{}
		}
		// a request with no loader (a worker's, say) names no document, so it can't make one stale
		if event.LoaderID != "" {
			d.loaders[event.LoaderID] = struct{}{}
		}
		d.requests[event.RequestID] = struct{}{}
	}
}

// reset marks everything observed so far as the previous spec's.
func (d *documentBoundary) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.staleContexts = make(map[runtime.ExecutionContextID]struct{}, len(d.live))
	for id := range d.live {
		d.staleContexts[id] = struct{}{}
	}
	d.staleLoaders, d.loaders = d.loaders, nil
	d.staleRequests, d.requests = d.requests, nil
}

func (d *documentBoundary) staleContext(id runtime.ExecutionContextID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, stale := d.staleContexts[id]
	return stale
}

func (d *documentBoundary) staleRequest(loader cdp.LoaderID, id network.RequestID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, staleLoader := d.staleLoaders[loader]
	_, staleRequest := d.staleRequests[id]
	return staleLoader || staleRequest
}

func (d *documentBoundary) staleLogEntry(entry *cdplog.Entry) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if entry.NetworkRequestID != "" {
		if _, stale := d.staleRequests[entry.NetworkRequestID]; stale {
			return true
		}
	}
	return len(d.staleContexts) > 0
}

// DocumentBoundary is documentBoundary for a listener outside the engine - the Go client's tab
// listeners.  Feed it every event from the target, in stream order, through Stale, and call Reset
// when a Prepare starts tearing the old document down.  It sends no CDP commands.
type DocumentBoundary struct{ boundary documentBoundary }

// Reset marks everything observed so far as the previous spec's.
func (d *DocumentBoundary) Reset() { d.boundary.reset() }

// Stale observes event and reports whether it comes from a document the last Reset left behind: a
// console call or exception from one of its execution contexts, a request it starts, and the
// response, finish, failure, and Chrome log entry of a request it started.  Events it does not
// attribute (dialogs, paused requests, downloads, ...) are never stale.
func (d *DocumentBoundary) Stale(event any) bool {
	d.boundary.observe(event)
	switch event := event.(type) {
	case *runtime.EventConsoleAPICalled:
		return d.boundary.staleContext(event.ExecutionContextID)
	case *runtime.EventExceptionThrown:
		return event.ExceptionDetails != nil && d.boundary.staleContext(event.ExceptionDetails.ExecutionContextID)
	case *network.EventRequestWillBeSent:
		return d.boundary.staleRequest(event.LoaderID, event.RequestID)
	case *network.EventResponseReceived:
		return d.boundary.staleRequest(event.LoaderID, event.RequestID)
	case *network.EventLoadingFinished:
		return d.boundary.staleRequestID(event.RequestID)
	case *network.EventLoadingFailed:
		return d.boundary.staleRequestID(event.RequestID)
	case *cdplog.EventEntryAdded:
		return event.Entry != nil && d.boundary.staleLogEntry(event.Entry)
	}
	return false
}

func (d *documentBoundary) staleRequestID(id network.RequestID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, stale := d.staleRequests[id]
	return stale
}

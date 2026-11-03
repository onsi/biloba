package engine

import (
	"sort"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
)

// InflightRequest is a request Chrome announced and has not yet finished or failed: what
// WaitForNetworkIdle names when it times out.  A redirect replaces it with the next hop and keeps the
// original Started.
type InflightRequest struct {
	Method, URL, ResourceType string
	Started                   time.Time
}

type inflightEntry struct {
	InflightRequest
	frame  cdp.FrameID
	loader cdp.LoaderID
}

// InflightTracker is the set of requests a target has announced and not yet ended: what network idle
// means for both clients.  The zero value is ready to use and it is safe for concurrent use.
//
// A request leaves the set when Chrome reports it finished or failed.  Chrome reports neither for the
// requests a navigation cancels: when a frame commits a new document, the old document's pending
// requests just stop.  So the set also forgets a frame's requests that belong to an earlier document
// when the frame navigates (FrameNavigated), along with those of the frames inside that document (Chrome
// sends no frameDetached for them), and all of a frame's requests when it goes away (FrameDetached).
// The request that started the navigation carries the new document's loader ID, so it stays.  A
// same-document navigation commits no new document and drops nothing.  Frame ancestry comes from
// FrameAttached and FrameNavigated.
//
// Trade-off: a keepalive fetch or sendBeacon can outlive its document, and it stops counting the
// moment the page navigates.
type InflightTracker struct {
	mu       sync.Mutex
	requests map[network.RequestID]inflightEntry
	parents  map[cdp.FrameID]cdp.FrameID
}

// Track records a request Chrome announced.  A redirect announces its next hop under the same request
// ID: that replaces the entry and keeps the original start time.
func (t *InflightTracker) Track(event *network.EventRequestWillBeSent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.requests == nil {
		t.requests = map[network.RequestID]inflightEntry{}
	}
	started := time.Now()
	if hop, ok := t.requests[event.RequestID]; ok {
		started = hop.Started
	}
	t.requests[event.RequestID] = inflightEntry{
		InflightRequest: InflightRequest{Method: event.Request.Method, URL: event.Request.URL, ResourceType: event.Type.String(), Started: started},
		frame:           event.FrameID,
		loader:          event.LoaderID,
	}
}

// Finish forgets a request Chrome reported finished or failed.
func (t *InflightTracker) Finish(id network.RequestID) {
	t.mu.Lock()
	delete(t.requests, id)
	t.mu.Unlock()
}

// FrameAttached records that frame is a child of parent.  Call it for every Page.frameAttached.
func (t *InflightTracker) FrameAttached(frame, parent cdp.FrameID) {
	t.mu.Lock()
	t.setParent(frame, parent)
	t.mu.Unlock()
}

func (t *InflightTracker) setParent(frame, parent cdp.FrameID) {
	if parent == "" {
		return
	}
	if t.parents == nil {
		t.parents = map[cdp.FrameID]cdp.FrameID{}
	}
	t.parents[frame] = parent
}

// FrameNavigated forgets the requests of frame that belong to a document other than the one it just
// committed, and those of every frame inside the old document.  Call it for every Page.frameNavigated,
// which Chrome sends for cross-document navigations only.
func (t *InflightTracker) FrameNavigated(frame *cdp.Frame) {
	if frame == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.setParent(frame.ID, frame.ParentID)
	inside := t.descendants(frame.ID)
	for id := range inside {
		delete(t.parents, id)
	}
	for id, entry := range t.requests {
		if _, gone := inside[entry.frame]; gone || (entry.frame == frame.ID && entry.loader != frame.LoaderID) {
			delete(t.requests, id)
		}
	}
}

// FrameDetached forgets every request of a frame that is gone, and those of the frames inside it.
func (t *InflightTracker) FrameDetached(id cdp.FrameID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	gone := t.descendants(id)
	gone[id] = struct{}{}
	for frame := range gone {
		delete(t.parents, frame)
	}
	for requestID, entry := range t.requests {
		if _, isGone := gone[entry.frame]; isGone {
			delete(t.requests, requestID)
		}
	}
}

// descendants returns the frames known to be inside root, at any depth.
func (t *InflightTracker) descendants(root cdp.FrameID) map[cdp.FrameID]struct{} {
	out := map[cdp.FrameID]struct{}{}
	for frame := range t.parents {
		// the walk is bounded by the number of known frames, which also guards against a cycle
		for at, steps := t.parents[frame], 0; at != "" && steps <= len(t.parents); at, steps = t.parents[at], steps+1 {
			if at == root {
				out[frame] = struct{}{}
				break
			}
		}
	}
	return out
}

// Count returns the number of requests in flight.
func (t *InflightTracker) Count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.requests)
}

// Snapshot returns the requests in flight, oldest first.
func (t *InflightTracker) Snapshot() []InflightRequest {
	t.mu.Lock()
	out := make([]InflightRequest, 0, len(t.requests))
	for _, entry := range t.requests {
		out = append(out, entry.InflightRequest)
	}
	t.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

// Reset forgets everything.
func (t *InflightTracker) Reset() {
	t.mu.Lock()
	t.requests, t.parents = nil, nil
	t.mu.Unlock()
}

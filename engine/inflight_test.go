package engine_test

import (
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/onsi/biloba/engine"
)

// The tracker is fed Chrome's events in the order Chrome sends them; these specs feed it the same
// events by hand, so each rule has a case that does not depend on what Chrome happens to report.
var _ = Describe("InflightTracker", func() {
	var tracker *engine.InflightTracker

	request := func(id, url string, frame cdp.FrameID, loader cdp.LoaderID) *network.EventRequestWillBeSent {
		return &network.EventRequestWillBeSent{
			RequestID: network.RequestID(id),
			FrameID:   frame,
			LoaderID:  loader,
			Request:   &network.Request{Method: "GET", URL: url},
			Type:      network.ResourceTypeFetch,
		}
	}
	urls := func() []string {
		out := []string{}
		for _, r := range tracker.Snapshot() {
			out = append(out, r.URL)
		}
		return out
	}

	BeforeEach(func() {
		tracker = &engine.InflightTracker{}
	})

	It("tracks a request until it finishes or fails", func() {
		tracker.Track(request("1", "/a", "main", "doc1"))
		tracker.Track(request("2", "/b", "main", "doc1"))
		Expect(tracker.Count()).To(Equal(2))
		tracker.Finish("1")
		Expect(urls()).To(Equal([]string{"/b"}))
		tracker.Finish("2")
		Expect(tracker.Count()).To(BeZero())
	})

	It("replaces a redirected request with its next hop and keeps the original start", func() {
		tracker.Track(request("1", "/a", "main", "doc1"))
		started := tracker.Snapshot()[0].Started
		tracker.Track(request("1", "/b", "main", "doc1"))
		Expect(tracker.Snapshot()).To(HaveLen(1))
		Expect(tracker.Snapshot()[0].URL).To(Equal("/b"))
		Expect(tracker.Snapshot()[0].Started).To(Equal(started))
	})

	It("forgets the requests of the documents a frame leaves, and keeps the one that started the navigation", func() {
		tracker.Track(request("1", "/old", "main", "doc1"))
		tracker.Track(request("2", "/navigation", "main", "doc2"))
		tracker.Track(request("3", "/elsewhere", "other", "doc9"))
		tracker.FrameNavigated(&cdp.Frame{ID: "main", LoaderID: "doc2"})
		Expect(urls()).To(ConsistOf("/navigation", "/elsewhere"))
	})

	It("keeps a request made by the document the frame just committed", func() {
		tracker.FrameNavigated(&cdp.Frame{ID: "main", LoaderID: "doc2"})
		tracker.Track(request("1", "/new", "main", "doc2"))
		Expect(urls()).To(Equal([]string{"/new"}))
	})

	It("forgets the requests of the frames inside a document a frame leaves", func() {
		tracker.FrameAttached("child", "main")
		tracker.FrameAttached("grandchild", "child")
		tracker.FrameAttached("sibling", "other")
		tracker.Track(request("1", "/child", "child", "c1"))
		tracker.Track(request("2", "/grandchild", "grandchild", "g1"))
		tracker.Track(request("3", "/sibling", "sibling", "s1"))
		tracker.FrameNavigated(&cdp.Frame{ID: "main", LoaderID: "doc2"})
		Expect(urls()).To(Equal([]string{"/sibling"}))
	})

	It("learns a frame's parent from its own navigation", func() {
		tracker.FrameNavigated(&cdp.Frame{ID: "child", ParentID: "main", LoaderID: "c1"})
		tracker.Track(request("1", "/child", "child", "c1"))
		tracker.FrameNavigated(&cdp.Frame{ID: "main", LoaderID: "doc2"})
		Expect(tracker.Count()).To(BeZero())
	})

	It("forgets every request of a frame that is detached, and of the frames inside it", func() {
		tracker.FrameAttached("child", "main")
		tracker.FrameAttached("grandchild", "child")
		tracker.Track(request("1", "/main", "main", "doc1"))
		tracker.Track(request("2", "/child", "child", "c1"))
		tracker.Track(request("3", "/grandchild", "grandchild", "g1"))
		tracker.FrameDetached("child")
		Expect(urls()).To(Equal([]string{"/main"}))
	})

	It("forgets nothing for a frame it has not seen", func() {
		tracker.Track(request("1", "/a", "main", "doc1"))
		tracker.FrameDetached("unknown")
		tracker.FrameNavigated(nil)
		Expect(tracker.Count()).To(Equal(1))
	})

	It("forgets everything on Reset", func() {
		tracker.FrameAttached("child", "main")
		tracker.Track(request("1", "/a", "child", "c1"))
		tracker.Reset()
		Expect(tracker.Count()).To(BeZero())
		tracker.Track(request("2", "/b", "child", "c2"))
		tracker.FrameNavigated(&cdp.Frame{ID: "main", LoaderID: "doc2"})
		Expect(urls()).To(Equal([]string{"/b"}), "the frame ancestry is part of what Reset forgets")
	})
})

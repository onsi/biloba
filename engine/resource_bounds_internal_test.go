package engine

import (
	"bytes"
	"context"
	"testing"

	"github.com/chromedp/cdproto/fetch"
)

type recordingReader struct {
	remaining int
	maxRead   int
}

func (r *recordingReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		r.maxRead = len(p)
	}
	if r.remaining == 0 {
		return 0, nil
	}
	n := min(len(p), r.remaining)
	for i := range n {
		p[i] = 'x'
	}
	r.remaining -= n
	return n, nil
}

func TestReadBoundedStopsWithoutAllocatingTheWholeInput(t *testing.T) {
	reader := &recordingReader{remaining: 32 << 20}
	_, err := readBounded(context.Background(), reader, 1024)
	if err == nil || err.Error() != "content size exceeds limit 1024" {
		t.Fatalf("expected a deterministic limit error, got %v", err)
	}
	if reader.maxRead > boundedReadChunkSize {
		t.Fatalf("read requested %d bytes at once, want at most %d", reader.maxRead, boundedReadChunkSize)
	}
	if reader.remaining < (32<<20)-1025 {
		t.Fatalf("reader consumed beyond the one-byte limit probe: %d bytes remain", reader.remaining)
	}
}

func TestReadBoundedHonorsCancellationBetweenChunks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelingReader{cancel: cancel}
	_, err := readBounded(ctx, reader, 1<<20)
	if err != context.Canceled {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

type cancelingReader struct {
	cancel func()
	read   bool
}

func (r *cancelingReader) Read(p []byte) (int, error) {
	if !r.read {
		r.read = true
		r.cancel()
		return copy(p, bytes.Repeat([]byte{'x'}, len(p))), nil
	}
	return 0, nil
}

func TestResponseHeadersPreserveDuplicatesAndCompatibleLookup(t *testing.T) {
	entries := []*fetch.HeaderEntry{
		{Name: "Set-Cookie", Value: "a=1"},
		{Name: "Set-Cookie", Value: "b=2"},
		{Name: "X-Trace", Value: "first"},
	}
	headers, ordered := responseHeaders(entries)
	if got := headers["Set-Cookie"]; got != "b=2" {
		t.Fatalf("compatibility map should contain the last value, got %q", got)
	}
	want := []HeaderEntry{{Name: "Set-Cookie", Value: "a=1"}, {Name: "Set-Cookie", Value: "b=2"}, {Name: "X-Trace", Value: "first"}}
	if !headerEntriesEqual(ordered, want) {
		t.Fatalf("ordered entries = %#v, want %#v", ordered, want)
	}

	ordered[0].Value = "mutated"
	cloned := cloneHeaderEntries(want)
	cloned[0].Value = "clone"
	if want[0].Value != "a=1" {
		t.Fatalf("header entry clones alias their input")
	}
}

func TestMergeResponseHeadersRetainsExplicitDuplicateOverrides(t *testing.T) {
	original := []HeaderEntry{
		{Name: "Set-Cookie", Value: "old=1"},
		{Name: "Content-Type", Value: "text/plain"},
	}
	override := ResponseOverride{
		Headers: map[string]string{"X-Trace": "mapped"},
		HeaderEntries: []HeaderEntry{
			{Name: "Set-Cookie", Value: "a=1"},
			{Name: "Set-Cookie", Value: "b=2"},
		},
	}
	got := mergeResponseHeaders(original, override)
	want := []HeaderEntry{
		{Name: "Content-Type", Value: "text/plain"},
		{Name: "X-Trace", Value: "mapped"},
		{Name: "Set-Cookie", Value: "a=1"},
		{Name: "Set-Cookie", Value: "b=2"},
	}
	if !headerEntriesEqual(got, want) {
		t.Fatalf("merged entries = %#v, want %#v", got, want)
	}
}

func TestDownloadQueryMatchesExactBinaryContentWithoutStringCoercion(t *testing.T) {
	content := []byte{0xff, 0x00, 0x80, 'x'}
	session := &Session{downloads: map[string]*Download{
		"binary": {ID: "binary", State: DownloadComplete, content: content},
	}, downloadOrder: []string{"binary"}}

	matches := session.Downloads(DownloadQuery{ContentBytes: []byte{0xff, 0x00, 0x80, 'x'}})
	if len(matches) != 1 || matches[0].ID != "binary" {
		t.Fatalf("exact binary query did not match: %#v", matches)
	}
	if matches := session.Downloads(DownloadQuery{ContentBytes: []byte{0xff, 0x00, 0x80, 'y'}}); len(matches) != 0 {
		t.Fatalf("different binary query unexpectedly matched: %#v", matches)
	}
}

func headerEntriesEqual(a, b []HeaderEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNetworkShadowDiagnosticsAreBoundedAndTruncated(t *testing.T) {
	// One record is appended per intercepted request, each carrying client callsites, so an unbounded
	// slice grows with traffic rather than with the number of handlers - and once the retained set
	// outgrows a protocol frame the diagnostic stops being answerable at all.
	session := &Session{}
	oversized := string(bytes.Repeat([]byte("u"), DefaultWarningPreviewBytes*2))
	for range DefaultEventHistoryLimit + 250 {
		session.appendNetworkShadowLocked(NetworkShadowDiagnostic{
			URL:      oversized,
			Winner:   NetworkOwnerProvenance{Callsite: oversized},
			Shadowed: []NetworkOwnerProvenance{{Callsite: oversized}},
		})
	}
	if len(session.networkShadows) != DefaultEventHistoryLimit {
		t.Fatalf("retained %d shadow records, want the %d bound", len(session.networkShadows), DefaultEventHistoryLimit)
	}
	if session.networkShadowsDropped != 250 {
		t.Fatalf("dropped counter is %d, want 250 - a silent eviction reads as 'nothing was shadowed'", session.networkShadowsDropped)
	}
	// truncateUTF8 appends a "… [truncated]" marker, so the bound is the preview plus that marker.
	if got := len(session.networkShadows[0].URL); got > DefaultWarningPreviewBytes+32 {
		t.Fatalf("retained a %d-byte URL, want it truncated to about %d", got, DefaultWarningPreviewBytes)
	}
}

func TestRetainedProvenanceCallsitesAreTruncated(t *testing.T) {
	oversized := string(bytes.Repeat([]byte("s"), DefaultWarningPreviewBytes*2))
	handler := networkHandlerOwnerProvenance(&networkHandlerEntry{options: NetworkHandlerOptions{Callsite: oversized}})
	if len(handler.Callsite) > DefaultWarningPreviewBytes+32 {
		t.Fatalf("handler callsite is %d bytes, want at most %d", len(handler.Callsite), DefaultWarningPreviewBytes)
	}
	hold := responseHoldOwnerProvenance(&responseHold{callsite: oversized})
	if len(hold.Callsite) > DefaultWarningPreviewBytes+32 {
		t.Fatalf("hold callsite is %d bytes, want at most %d", len(hold.Callsite), DefaultWarningPreviewBytes)
	}
}

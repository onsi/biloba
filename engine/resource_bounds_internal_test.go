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

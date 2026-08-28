package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

const DefaultDownloadContentLimit int64 = 16 << 20

type DownloadState string

const (
	DownloadActive    DownloadState = "active"
	DownloadComplete  DownloadState = "complete"
	DownloadCancelled DownloadState = "cancelled"
)

type Download struct {
	ID, URL, Filename         string
	State                     DownloadState
	ReceivedBytes, TotalBytes int64
	StartedAt, CompletedAt    time.Time
	content                   []byte
}
type DownloadQuery struct {
	State                  DownloadState
	Filename, URL, Content *Expectation
	ContentBytes           []byte
}

func (s *Session) setupDownloads(ctx context.Context) error {
	if s.root == s {
		dir, err := os.MkdirTemp("", "biloba-engine-downloads-")
		if err != nil {
			return err
		}
		s.downloadDir = dir
	} else {
		s.downloadDir = s.contextRoot().downloadDir
	}
	return ConfigureDownloadsContext(ctx, s.browserContextID, s.downloadDir)
}
func (s *Session) handleDownloadBegin(event *cdpbrowser.EventDownloadWillBegin) {
	if !s.eventsEnabled.Load() {
		return
	}
	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()
	if s.downloads == nil {
		s.downloads = map[string]*Download{}
	}
	d := &Download{ID: event.GUID, URL: event.URL, Filename: event.SuggestedFilename, State: DownloadActive, StartedAt: time.Now()}
	s.downloads[event.GUID] = d
	s.downloadOrder = append(s.downloadOrder, event.GUID)
}
func (s *Session) handleDownloadProgress(event *cdpbrowser.EventDownloadProgress) {
	if !s.eventsEnabled.Load() {
		return
	}
	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()
	d := s.downloads[event.GUID]
	if d == nil || d.State != DownloadActive {
		return
	}
	d.ReceivedBytes = int64(event.ReceivedBytes)
	d.TotalBytes = int64(event.TotalBytes)
	switch event.State {
	case cdpbrowser.DownloadProgressStateCompleted:
		d.State = DownloadComplete
		d.CompletedAt = time.Now()
	case cdpbrowser.DownloadProgressStateCanceled:
		d.State = DownloadCancelled
		d.CompletedAt = time.Now()
	}
}
func (s *Session) Downloads(query DownloadQuery) []Download {
	return s.downloadsMatching(context.Background(), query)
}

func (s *Session) downloadsMatching(ctx context.Context, query DownloadQuery) []Download {
	s.downloadMu.Lock()
	items := make([]Download, 0, len(s.downloadOrder))
	for _, id := range s.downloadOrder {
		if d := s.downloads[id]; d != nil {
			items = append(items, *d)
		}
	}
	s.downloadMu.Unlock()
	out := []Download{}
	for _, d := range items {
		matched, _ := s.matchesDownload(ctx, d, query)
		if matched {
			out = append(out, d)
		}
	}
	return out
}
func (s *Session) matchesDownload(ctx context.Context, d Download, q DownloadQuery) (bool, error) {
	if q.State != "" && d.State != q.State {
		return false, nil
	}
	for _, pair := range []struct {
		v any
		e *Expectation
	}{{d.Filename, q.Filename}, {d.URL, q.URL}} {
		if pair.e != nil {
			ok, err := MatchExpectation(pair.v, *pair.e)
			if err != nil || !ok {
				return false, err
			}
		}
	}
	if q.Content != nil || q.ContentBytes != nil {
		if d.State != DownloadComplete {
			return false, nil
		}
		body, err := s.DownloadContent(ctx, d.ID, DefaultDownloadContentLimit)
		if err != nil {
			return false, err
		}
		if q.Content != nil {
			matched, matchErr := MatchExpectation(string(body), *q.Content)
			if matchErr != nil || !matched {
				return false, matchErr
			}
		}
		if q.ContentBytes != nil && !bytes.Equal(body, q.ContentBytes) {
			return false, nil
		}
		return true, nil
	}
	return true, nil
}
func (s *Session) WaitForDownload(ctx context.Context, q DownloadQuery, p PollPolicy) (Download, error) {
	var found Download
	_, err := Poll(ctx, p, func(probeCtx context.Context) (Observation, bool, error) {
		matches := s.downloadsMatching(probeCtx, q)
		if len(matches) == 0 {
			return Observation{}, false, nil
		}
		found = matches[0]
		return Observation{Value: found}, true, nil
	})
	return found, err
}
func (s *Session) DownloadContent(ctx context.Context, id string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultDownloadContentLimit
	}
	if id == "" || filepath.Base(id) != id || filepath.Clean(id) != id {
		return nil, &Error{Code: CodeInvalidArgument, Operation: "read download", Message: "download ID must be a basename", Observed: id}
	}
	s.downloadMu.Lock()
	d := s.downloads[id]
	if d == nil {
		s.downloadMu.Unlock()
		return nil, &Error{Code: CodeInvalidArgument, Operation: "read download", Message: "download not found"}
	}
	snapshot := *d
	cached := append([]byte(nil), d.content...)
	dir := s.downloadDir
	s.downloadMu.Unlock()
	if snapshot.State != DownloadComplete {
		return nil, &Error{Code: CodeActionFailed, Operation: "read download", Message: "download is not complete", Observed: snapshot.State}
	}
	if cached != nil {
		if int64(len(cached)) > maxBytes {
			return nil, &Error{Code: CodeInvalidArgument, Operation: "read download", Message: fmt.Sprintf("download content size %d exceeds limit %d", len(cached), maxBytes), Observed: len(cached)}
		}
		return cached, nil
	}
	path := filepath.Join(dir, id)
	file, err := os.Open(path)
	if err != nil {
		return nil, typedError(CodeIO, "read download", err)
	}
	defer file.Close()
	content, err := readBounded(ctx, file, maxBytes)
	if err != nil {
		if err == context.Canceled || err == context.DeadlineExceeded {
			return nil, contextError("read download", err)
		}
		var limitErr *contentLimitError
		if errors.As(err, &limitErr) {
			return nil, &Error{Code: CodeInvalidArgument, Operation: "read download", Message: fmt.Sprintf("download content exceeds limit %d", maxBytes), Observed: maxBytes + 1}
		}
		return nil, typedError(CodeIO, "read download", err)
	}
	select {
	case <-ctx.Done():
		return nil, contextError("read download", ctx.Err())
	default:
	}
	s.downloadMu.Lock()
	if current := s.downloads[id]; current != nil && current.State == DownloadComplete {
		current.content = append([]byte(nil), content...)
	}
	s.downloadMu.Unlock()
	return content, nil
}
func (s *Session) CancelDownload(ctx context.Context, id string) error {
	s.downloadMu.Lock()
	download := s.downloads[id]
	s.downloadMu.Unlock()
	if download == nil {
		return &Error{Code: CodeInvalidArgument, Operation: "cancel download", Message: "download not found", Observed: id}
	}
	return s.serial(ctx, "cancel download", func(op context.Context) error {
		return s.withBrowserExecutor(op, func(browserCtx context.Context) error {
			return cdpbrowser.CancelDownload(id).WithBrowserContextID(s.browserContextID).Do(browserCtx)
		})
	})
}

func (s *Session) activeOrRecentDownloadCount(now time.Time) int {
	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()
	count := 0
	for _, download := range s.downloads {
		if download.State == DownloadActive || (!download.CompletedAt.IsZero() && now.Sub(download.CompletedAt) < time.Second) {
			count++
		}
	}
	return count
}

func (s *Session) hasActiveDownloads() bool {
	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()
	for _, download := range s.downloads {
		if download.State == DownloadActive {
			return true
		}
	}
	return false
}

func (s *Session) cancelActiveDownloads() {
	s.downloadMu.Lock()
	defer s.downloadMu.Unlock()
	for _, download := range s.downloads {
		if download.State == DownloadActive {
			download.State = DownloadCancelled
			download.CompletedAt = time.Now()
		}
	}
}

func (s *Session) removeOwnDownloadArtifacts() {
	s.downloadMu.Lock()
	ids := append([]string(nil), s.downloadOrder...)
	dir := s.downloadDir
	s.downloadMu.Unlock()
	for _, id := range ids {
		_ = os.Remove(filepath.Join(dir, id))
	}
}

func (s *Session) resetDownloads(ctx context.Context) error {
	if err := chromedp.Run(ctx, cdpbrowser.SetDownloadBehavior(cdpbrowser.SetDownloadBehaviorBehaviorDeny).
		WithBrowserContextID(s.browserContextID)); err != nil {
		return err
	}
	s.downloadMu.Lock()
	active := make([]string, 0)
	ids := make([]string, 0, len(s.downloads))
	for id, download := range s.downloads {
		ids = append(ids, id)
		if download.State == DownloadActive {
			active = append(active, id)
		}
	}
	s.downloads = nil
	s.downloadOrder = nil
	dir := s.downloadDir
	ownsContext := s.ownsContext
	s.downloadMu.Unlock()
	for _, id := range active {
		_ = s.withBrowserExecutor(ctx, func(browserCtx context.Context) error {
			return cdpbrowser.CancelDownload(id).WithBrowserContextID(s.browserContextID).Do(browserCtx)
		})
	}
	if ownsContext {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	} else {
		for _, id := range ids {
			_ = os.Remove(filepath.Join(dir, id))
		}
	}
	return ConfigureDownloadsContext(ctx, s.browserContextID, dir)
}

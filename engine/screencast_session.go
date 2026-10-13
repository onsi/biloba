package engine

import (
	"context"
	"fmt"
	"sync"
)

// Validate reports the first invalid setting, in the words the daemon's clients see.
func (p ScreencastParams) Validate() error {
	if p.Format != "" && p.Format != "jpeg" && p.Format != "png" {
		return invalidArgument("start screencast", fmt.Sprintf(`format must be "jpeg" or "png", got %q`, p.Format))
	}
	if p.Quality < 0 || p.Quality > 100 {
		return invalidArgument("start screencast", fmt.Sprintf("quality must be between 0 and 100, got %d", p.Quality))
	}
	for _, field := range []struct {
		name  string
		value int
	}{{"maxWidth", p.MaxWidth}, {"maxHeight", p.MaxHeight}, {"everyNthFrame", p.EveryNthFrame}} {
		if field.value < 0 {
			return invalidArgument("start screencast", fmt.Sprintf("%s must not be negative, got %d", field.name, field.value))
		}
	}
	return nil
}

// sessionScreencast is one recording made through Session.StartScreencast.
type sessionScreencast struct {
	id   string
	stop func(context.Context) error

	// stopMu serializes stops, so a second StopScreencast waits for the first and returns its frames.
	stopMu  sync.Mutex
	mu      sync.Mutex
	frames  []ScreencastFrame // in arrival order until stopped, then in Timestamp order
	stopped bool
}

func (rec *sessionScreencast) record(frame ScreencastFrame) {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	rec.frames = append(rec.frames, frame)
}

// finish ends the recording once: run calls stop (or arranges not to), and the frames are sorted.
func (rec *sessionScreencast) finish(run func(stop func(context.Context) error) error) error {
	rec.mu.Lock()
	stopped := rec.stopped
	rec.mu.Unlock()
	if stopped {
		return nil
	}
	err := run(rec.stop)
	rec.mu.Lock()
	rec.stopped = true
	SortScreencastFrames(rec.frames)
	rec.mu.Unlock()
	return err
}

// drop ends the recording without asking Chrome, for a tab that is going away or can no longer be
// reached.
func (rec *sessionScreencast) drop() {
	_ = rec.finish(func(stop func(context.Context) error) error {
		dropScreencastListener(stop)
		return nil
	})
}

// dropScreencastListener calls a StartScreencastContext stop with a context that holds no browser: the
// stop command fails at once, and the listener is dropped all the same.
func dropScreencastListener(stop func(context.Context) error) {
	if stop == nil {
		return // the screencast never started
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = stop(ctx)
}

func (rec *sessionScreencast) framesSoFar() int {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return len(rec.frames)
}

/*
StartScreencast starts recording this tab's frames and returns the recording's id; StopScreencast ends
it and returns the frames.  It is the session form of StartScreencastContext, with the rules the Go
client's StartScreencast follows: a tab runs one recording at a time, and a frame session refuses.
Prepare and Close stop a recording that is still running and discard every recording the session
holds.
*/
func (s *Session) StartScreencast(ctx context.Context, params ScreencastParams) (string, error) {
	if err := s.tabOnly("start screencast"); err != nil {
		return "", err
	}
	if err := params.Validate(); err != nil {
		return "", err
	}
	s.screencastMu.Lock()
	if s.recording != nil {
		s.screencastMu.Unlock()
		return "", invalidArgument("start screencast", "this tab is already recording a screencast - stop it before starting another")
	}
	s.screencastSequence++
	rec := &sessionScreencast{id: fmt.Sprintf("%s-screencast-%d", s.targetID, s.screencastSequence)}
	s.recording = rec
	s.screencastMu.Unlock()

	err := s.serial(ctx, "start screencast", func(opCtx context.Context) error {
		stop, err := StartScreencastContext(opCtx, params, rec.record)
		rec.stop = stop
		return err
	})
	s.screencastMu.Lock()
	defer s.screencastMu.Unlock()
	if s.recording != rec {
		// Close ran while the screencast started, and has already given up on the session.
		rec.drop()
		return "", &Error{Code: CodeSessionClosed, Operation: "start screencast", Message: "session is closed"}
	}
	if err != nil {
		s.recording = nil
		return "", err
	}
	if s.screencasts == nil {
		s.screencasts = map[string]*sessionScreencast{}
	}
	s.screencasts[rec.id] = rec
	return rec.id, nil
}

/*
StopScreencast stops the recording, waits for Chrome to confirm, and returns its frames in Timestamp
order (see SortScreencastFrames): no frame arrives after it returns.  Stopping a stopped recording
returns the same frames without a round trip.  The session keeps the frames until ReleaseScreencast,
Prepare, or Close.  The returned slice is shared: do not modify it.
*/
func (s *Session) StopScreencast(ctx context.Context, id string) ([]ScreencastFrame, error) {
	s.screencastMu.Lock()
	rec := s.screencasts[id]
	s.screencastMu.Unlock()
	if rec == nil {
		return nil, &Error{Code: CodeNotFound, Operation: "stop screencast", Message: "no such screencast: a session discards its recordings when it is prepared or closed"}
	}
	rec.stopMu.Lock()
	defer rec.stopMu.Unlock()
	err := rec.finish(func(stop func(context.Context) error) error {
		ran := false
		serialErr := s.serial(ctx, "stop screencast", func(opCtx context.Context) error {
			ran = true
			return stop(opCtx)
		})
		if !ran {
			dropScreencastListener(stop) // a closed or crashed session cannot answer a stop
		}
		return serialErr
	})
	s.screencastMu.Lock()
	if s.recording == rec {
		s.recording = nil
	}
	s.screencastMu.Unlock()
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.frames, err
}

// ReleaseScreencast forgets a stopped recording and its frames.
func (s *Session) ReleaseScreencast(id string) {
	s.screencastMu.Lock()
	defer s.screencastMu.Unlock()
	if rec := s.screencasts[id]; rec != nil && rec != s.recording {
		delete(s.screencasts, id)
	}
}

// stopScreencastForReset asks Chrome to stop a recording that is still running, while the tab is still
// there to answer.  Prepare calls it, under serial, before discardScreencasts.
func (s *Session) stopScreencastForReset(ctx context.Context) {
	s.screencastMu.Lock()
	rec := s.recording
	s.screencastMu.Unlock()
	if rec != nil {
		_ = rec.finish(func(stop func(context.Context) error) error { return stop(ctx) })
	}
}

// discardScreencasts drops every recording the session holds, ending one that is still running
// without asking Chrome.  It costs nothing when the session never recorded.
func (s *Session) discardScreencasts() {
	s.screencastMu.Lock()
	defer s.screencastMu.Unlock()
	if s.recording != nil {
		s.recording.drop()
	}
	for _, rec := range s.screencasts {
		rec.drop()
	}
	s.recording = nil
	s.screencasts = nil
}

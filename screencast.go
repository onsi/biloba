package biloba

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/onsi/biloba/engine"
)

/*
ScreencastOptions configures [Biloba.StartScreencast].  The zero value records every frame as a JPEG
at Chrome's default quality and the tab's full size:

  - Format is "jpeg" (the default when empty) or "png".
  - Quality is the JPEG quality, 0-100; 0 leaves it to Chrome.  PNG ignores it.
  - MaxWidth and MaxHeight cap the frame size in pixels; 0 means no cap.  Chrome scales the frame
    down, keeping its aspect ratio, to fit both.  It never scales a frame up.
  - EveryNthFrame keeps one frame in every n; 0 or 1 keeps them all.

Read https://onsi.github.io/biloba/#recording-a-screencast to learn more about screencasts
*/
type ScreencastOptions struct {
	Format        string
	Quality       int
	MaxWidth      int
	MaxHeight     int
	EveryNthFrame int
}

func (o ScreencastOptions) validate() error {
	if o.Format != "" && o.Format != "jpeg" && o.Format != "png" {
		return fmt.Errorf(`ScreencastOptions.Format must be "jpeg" or "png", got %q`, o.Format)
	}
	if o.Quality < 0 || o.Quality > 100 {
		return fmt.Errorf("ScreencastOptions.Quality must be between 0 and 100, got %d", o.Quality)
	}
	for _, field := range []struct {
		name  string
		value int
	}{{"MaxWidth", o.MaxWidth}, {"MaxHeight", o.MaxHeight}, {"EveryNthFrame", o.EveryNthFrame}} {
		if field.value < 0 {
			return fmt.Errorf("ScreencastOptions.%s must not be negative, got %d", field.name, field.value)
		}
	}
	return nil
}

/*
ScreencastFrame is one frame of a screencast - see [Biloba.StartScreencast]:

  - Data is the encoded image, in the format the screencast was started with.
  - Timestamp is when Chrome expects to display the frame: from Chrome 156, about one frame (16ms at
    60 frames a second) after the page drew it, so the last frame's Timestamp can be a little later
    than Stop returning.  Earlier versions stamp the frame when they send it.  It is wall-clock time
    on the machine running Chrome, so on the same machine it compares directly with time.Now().  It
    is the zero time in the unlikely event that Chrome sent a frame without one.  Full Chrome can
    stamp a frame hundreds of milliseconds late when it falls behind.
  - DeviceWidth and DeviceHeight are the viewport size, in CSS pixels, that the image shows.
  - PageScaleFactor is the page's pinch-zoom scale.
  - ScrollOffsetX and ScrollOffsetY are the page's scroll position, in CSS pixels, when the frame was
    drawn.
  - OffsetTop is how far down the image the page starts, in CSS pixels (non-zero only when browser
    chrome such as an emulated mobile URL bar sits above it).

Read https://onsi.github.io/biloba/#recording-a-screencast to learn more about screencasts
*/
type ScreencastFrame struct {
	Data            []byte
	Timestamp       time.Time
	DeviceWidth     float64
	DeviceHeight    float64
	PageScaleFactor float64
	ScrollOffsetX   float64
	ScrollOffsetY   float64
	OffsetTop       float64
}

/*
ScreencastRecording is a screencast in progress, returned by [Biloba.StartScreencast].  Call Stop to
end it and collect its frames.

Read https://onsi.github.io/biloba/#recording-a-screencast to learn more about screencasts
*/
type ScreencastRecording struct {
	b    *Biloba
	stop func(context.Context) error

	lock     sync.Mutex
	recorded []engine.ScreencastFrame // in arrival order
	stopped  bool
}

/*
StartScreencast() starts recording this tab's frames, as Chrome produces them, until you call Stop
on the returned recording:

	rec := b.StartScreencast(biloba.ScreencastOptions{Format: "jpeg", Quality: 92})
	// ... drive the page ...
	frames := rec.Stop()

Chrome sends the frame already on screen and then a frame each time the page draws a new one, so a
page that is not changing yields one frame and then nothing.  Frames are the size of the tab's window
in CSS pixels - an emulated device scale factor does not enlarge them.  They are held in memory until
Stop returns them: a 1024x768 JPEG frame is roughly 40-60KB, a PNG frame 500KB or more.

A tab runs one screencast at a time: starting a second before stopping the first fails the spec.  A
recording the spec does not stop is stopped by a DeferCleanup when the spec ends.  StartScreencast
applies to a tab, not a frame handle, and accepts no poll-config knobs.

Read https://onsi.github.io/biloba/#recording-a-screencast to learn more about screencasts
*/
func (b *Biloba) StartScreencast(options ScreencastOptions) *ScreencastRecording {
	b.gt.Helper()
	if b.refusedOnFrame("StartScreencast") {
		return nil
	}
	b.guardConfig("StartScreencast")
	if err := options.validate(); err != nil {
		b.gt.Fatalf("StartScreencast: %s", err.Error())
		return nil
	}

	b.lock.Lock()
	if b.state.screencast != nil {
		b.lock.Unlock()
		b.gt.Fatalf("StartScreencast: this tab is already recording a screencast - Stop it before starting another")
		return nil
	}
	rec := &ScreencastRecording{b: b}
	b.state.screencast = rec
	b.lock.Unlock()

	params := engine.ScreencastParams{Format: options.Format, Quality: options.Quality, MaxWidth: options.MaxWidth, MaxHeight: options.MaxHeight, EveryNthFrame: options.EveryNthFrame}
	err := b.runEngine("start a screencast", func(ctx context.Context) error {
		stop, err := engine.StartScreencastContext(ctx, params, rec.record)
		rec.stop = stop
		return err
	})
	if err != nil {
		b.lock.Lock()
		b.state.screencast = nil
		b.lock.Unlock()
		b.gt.Fatalf("failed to start the screencast: %s", err.Error())
		return nil
	}
	b.gt.DeferCleanup(func() { rec.Stop() })
	return rec
}

func (rec *ScreencastRecording) record(frame engine.ScreencastFrame) {
	rec.lock.Lock()
	defer rec.lock.Unlock()
	rec.recorded = append(rec.recorded, frame)
}

/*
Stop() stops the screencast and returns every frame it recorded, in Timestamp order (full Chrome can
deliver a frame after one drawn later).  It waits for Chrome to confirm the stop, so no frame arrives
after it returns.  Calling Stop again returns the same frames.

Read https://onsi.github.io/biloba/#recording-a-screencast to learn more about screencasts
*/
func (rec *ScreencastRecording) Stop() []ScreencastFrame {
	if rec == nil {
		return nil
	}
	b := rec.b
	b.gt.Helper()
	rec.lock.Lock()
	alreadyStopped := rec.stopped
	rec.stopped = true
	rec.lock.Unlock()
	if !alreadyStopped {
		if b.Context.Err() == nil {
			if err := b.runEngine("stop the screencast", rec.stop); err != nil {
				b.gt.Fatalf("failed to stop the screencast: %s", err.Error())
			}
		} else {
			// A closed tab sends no more frames and cannot answer a stop; this only drops the listener.
			_ = rec.stop(b.Context)
		}
		b.lock.Lock()
		if b.state.screencast == rec {
			b.state.screencast = nil
		}
		b.lock.Unlock()
	}
	rec.lock.Lock()
	recorded := slices.Clone(rec.recorded)
	rec.lock.Unlock()
	engine.SortScreencastFrames(recorded)
	frames := make([]ScreencastFrame, len(recorded))
	for i, frame := range recorded {
		frames[i] = ScreencastFrame(frame)
	}
	return frames
}

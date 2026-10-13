package engine

import (
	"context"
	"encoding/base64"
	"slices"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// ScreencastParams configures Page.startScreencast.  Format is "jpeg" or "png" ("" means jpeg); a zero
// Quality, MaxWidth, MaxHeight, or EveryNthFrame leaves that setting to Chrome.
type ScreencastParams struct {
	Format        string
	Quality       int
	MaxWidth      int
	MaxHeight     int
	EveryNthFrame int
}

// ScreencastFrame is one Page.screencastFrame: the decoded image and its metadata.  Timestamp is
// Chrome's frame-swap time, or the zero time if Chrome sent none.
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

// SortScreencastFrames puts frames in Timestamp order.  Full Chrome encodes screencast frames on
// several threads and can deliver one before a frame drawn earlier; chrome-headless-shell delivers
// them in order.  When any frame lacks a timestamp the frames are left in arrival order.
func SortScreencastFrames(frames []ScreencastFrame) {
	for _, frame := range frames {
		if frame.Timestamp.IsZero() {
			return
		}
	}
	slices.SortStableFunc(frames, func(a, b ScreencastFrame) int { return a.Timestamp.Compare(b.Timestamp) })
}

// screencastAckTimeout bounds one frame acknowledgement.  An ack runs on its own goroutine, after
// the call that started the screencast has returned, so it cannot borrow that call's deadline.
const screencastAckTimeout = 10 * time.Second

/*
StartScreencastContext starts a screencast on the target in ctx and calls onFrame, in arrival order,
for every frame Chrome sends until the returned stop is called (see SortScreencastFrames).  It acknowledges each frame as it
arrives - Chrome sends no further frames while one is unacknowledged.

The listener and the acknowledgements outlive ctx, which only bounds the start command: they run on
ctx's values without its deadline until stop.  stop sends Page.stopScreencast and, once Chrome has
answered, guarantees onFrame is not called again.  A target's events and command responses arrive on
one ordered stream, so every frame Chrome sent before answering has been delivered by then.
*/
func StartScreencastContext(ctx context.Context, params ScreencastParams, onFrame func(ScreencastFrame)) (stop func(context.Context) error, err error) {
	listenCtx, cancelListen := context.WithCancel(context.WithoutCancel(ctx))
	var lock sync.Mutex
	stopped := false
	chromedp.ListenTarget(listenCtx, func(ev any) {
		event, ok := ev.(*page.EventScreencastFrame)
		if !ok {
			return
		}
		// The listener runs on the target's event goroutine, which is also the goroutine that delivers
		// command responses: acknowledging synchronously here would wait forever on itself.
		go func() {
			ackCtx, cancel := context.WithTimeout(listenCtx, screencastAckTimeout)
			defer cancel()
			_ = chromedp.Run(ackCtx, page.ScreencastFrameAck(event.SessionID))
		}()
		frame := ScreencastFrame{}
		if data, err := base64.StdEncoding.DecodeString(event.Data); err == nil {
			frame.Data = data
		}
		if m := event.Metadata; m != nil {
			frame.DeviceWidth, frame.DeviceHeight = m.DeviceWidth, m.DeviceHeight
			frame.PageScaleFactor = m.PageScaleFactor
			frame.ScrollOffsetX, frame.ScrollOffsetY = m.ScrollOffsetX, m.ScrollOffsetY
			frame.OffsetTop = m.OffsetTop
			if m.Timestamp != nil {
				frame.Timestamp = m.Timestamp.Time()
			}
		}
		lock.Lock()
		defer lock.Unlock()
		if !stopped {
			onFrame(frame)
		}
	})

	// Chrome's own default is PNG; spell the format out so "" means JPEG.
	format := page.ScreencastFormatJpeg
	if params.Format != "" {
		format = page.ScreencastFormat(params.Format)
	}
	start := page.StartScreencast().WithFormat(format)
	if params.Quality > 0 {
		start = start.WithQuality(int64(params.Quality))
	}
	if params.MaxWidth > 0 {
		start = start.WithMaxWidth(int64(params.MaxWidth))
	}
	if params.MaxHeight > 0 {
		start = start.WithMaxHeight(int64(params.MaxHeight))
	}
	if params.EveryNthFrame > 0 {
		start = start.WithEveryNthFrame(int64(params.EveryNthFrame))
	}
	if err := chromedp.Run(ctx, start); err != nil {
		cancelListen()
		return nil, err
	}

	return func(stopCtx context.Context) error {
		err := chromedp.Run(stopCtx, page.StopScreencast())
		lock.Lock()
		stopped = true
		lock.Unlock()
		cancelListen()
		return err
	}, nil
}

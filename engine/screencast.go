package engine

import (
	"context"
	"slices"
	"time"

	"github.com/chromedp/cdproto/cdp"
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
answered, guarantees onFrame is not called again.  Every frame Chrome sent before answering has been
handed to onFrame by the time stop returns: the tab's listeners queue each frame before chromedp
hands over the answer (see ListenTarget), and stop waits for the queue to drain.

The listener only queues a frame (chromedp has decoded it already); a goroutine acknowledges it and
calls onFrame, in arrival order.  That goroutine runs only while frames are queued, so a recording
whose tab goes away without a stop (a popup closing itself) leaves none behind.
*/
func StartScreencastContext(ctx context.Context, params ScreencastParams, onFrame func(ScreencastFrame)) (stop func(context.Context) error, err error) {
	listenCtx, cancelListen := context.WithCancel(context.WithoutCancel(ctx))
	frames := NewHandoff(func(event *page.EventScreencastFrame) {
		// The acknowledgement waits on a reply, so it can't hold up the frames behind this one.
		go func() {
			ackCtx, cancel := context.WithTimeout(listenCtx, screencastAckTimeout)
			defer cancel()
			_, _ = chromedp.Call(ackCtx, page.ScreencastFrameAck, page.ScreencastFrameAckParams{SessionID: event.SessionID})
		}()
		frame := ScreencastFrame{Data: event.Data} // cdproto decodes the base64
		if m := event.Metadata; m != nil {
			frame.DeviceWidth, frame.DeviceHeight = m.DeviceWidth, m.DeviceHeight
			frame.PageScaleFactor = m.PageScaleFactor
			frame.ScrollOffsetX, frame.ScrollOffsetY = m.ScrollOffsetX, m.ScrollOffsetY
			frame.OffsetTop = m.OffsetTop
			frame.Timestamp = EpochTime(m.Timestamp)
		}
		onFrame(frame)
	})
	if err := ListenTarget(listenCtx, func(ev any) {
		if frame, ok := ev.(*page.EventScreencastFrame); ok {
			frames.Push(frame)
		}
	}); err != nil {
		cancelListen()
		frames.Close()
		return nil, err
	}

	// Chrome's own default is PNG; spell the format out so "" means JPEG.
	start := page.StartScreencastParams{Format: page.StartScreencastFormatJpeg}
	if params.Format != "" {
		start.Format = page.StartScreencastFormat(params.Format)
	}
	if params.Quality > 0 {
		start.Quality = ptr(int64(params.Quality))
	}
	start.MaxWidth = int64(max(params.MaxWidth, 0))
	start.MaxHeight = int64(max(params.MaxHeight, 0))
	start.EveryNthFrame = int64(max(params.EveryNthFrame, 0))
	if _, err := chromedp.Call(ctx, page.StartScreencast, start); err != nil {
		cancelListen()
		frames.Close()
		return nil, err
	}

	return func(stopCtx context.Context) error {
		_, err := chromedp.Call(stopCtx, page.StopScreencast, cdp.Empty{})
		// drops anything Chrome sends after answering and delivers what came before; and since chromedp
		// drops the cancelled listener only when the tab next sends something, Close also lets go of
		// onFrame, so an idle tab does not keep the recording alive through it
		frames.Close()
		cancelListen()
		return err
	}, nil
}

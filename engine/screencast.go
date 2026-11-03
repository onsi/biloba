package engine

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/cdp"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
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
handed to onFrame by the time stop returns: the connection's reader queues each frame before it reads
the answer (see ListenTarget), and stop waits for the queue to drain.

A frame is large, so the reader only queues its raw params; one goroutine per screencast decodes
them, acknowledges the frame, and calls onFrame, in arrival order.
*/
func StartScreencastContext(ctx context.Context, params ScreencastParams, onFrame func(ScreencastFrame)) (stop func(context.Context) error, err error) {
	listenCtx, cancelListen := context.WithCancel(context.WithoutCancel(ctx))
	frames := newScreencastQueue()
	go frames.deliver(func(raw jsonv2.Value) {
		var event page.EventScreencastFrame
		if jsonv2.Unmarshal(raw, &event, chromedp.DefaultUnmarshalOptions) != nil {
			return
		}
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
	// A tab that goes away without a stop (a popup closing itself) ends the session's events: the
	// queue ends then too, so its goroutine does not wait for a stop that never comes.
	if err := listenRaw(listenCtx, func(_ string, raw jsonv2.Value) { frames.push(raw) }, frames.end, page.ScreencastFrame.Method); err != nil {
		cancelListen()
		frames.close()
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
		frames.close()
		return nil, err
	}

	return func(stopCtx context.Context) error {
		_, err := chromedp.Call(stopCtx, page.StopScreencast, cdp.Empty{})
		frames.close() // drops anything Chrome sends after answering, and delivers what came before
		cancelListen()
		return err
	}, nil
}

// screencastQueue hands a screencast's frames from the connection's reader to the goroutine that
// delivers them, without a bound: the reader must never wait on it.
type screencastQueue struct {
	mu        sync.Mutex
	pending   []jsonv2.Value
	closed    bool
	wake      chan struct{}
	delivered chan struct{}
}

func newScreencastQueue() *screencastQueue {
	return &screencastQueue{wake: make(chan struct{}, 1), delivered: make(chan struct{})}
}

func (q *screencastQueue) push(raw jsonv2.Value) {
	q.mu.Lock()
	if !q.closed {
		q.pending = append(q.pending, raw)
	}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// close stops accepting frames and returns once every frame accepted before it has been delivered.
func (q *screencastQueue) close() {
	q.end()
	<-q.delivered
}

// end stops accepting frames without waiting for the ones already accepted to be delivered.
func (q *screencastQueue) end() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// liveScreencastWorkers counts the screencast delivery goroutines running, for specs.
var liveScreencastWorkers atomic.Int64

func (q *screencastQueue) deliver(handle func(jsonv2.Value)) {
	liveScreencastWorkers.Add(1)
	defer liveScreencastWorkers.Add(-1)
	defer close(q.delivered)
	for {
		q.mu.Lock()
		batch, closed := q.pending, q.closed
		q.pending = nil
		q.mu.Unlock()
		for _, raw := range batch {
			handle(raw)
		}
		if closed && len(batch) == 0 {
			return
		}
		if len(batch) == 0 {
			<-q.wake
		}
	}
}

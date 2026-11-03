package engine

import (
	"context"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Step is one CDP command as a chromedp action, so a sequence of commands can run with chromedp.Do
// against the context's target (chromedp.Run took the commands themselves before v0.17.0).
func Step[P, R any](command cdp.Command[P, R], params P) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, command, params)
		return err
	})
}

// callRaw sends one command to the target of ctx and decodes the result into result, for a command
// whose generated result type decodes a field in a way Biloba must not have (IO.read's data is
// base64 only when Chrome says so).
func callRaw(ctx context.Context, method string, params, result any) error {
	return chromedp.Do(ctx, chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		return t.Call(ctx, method, params, result)
	}))
}

// ptr is the address of v, for cdproto's optional fields.
func ptr[T any](v T) *T { return &v }

// opt is ptr for a value that is left out of the request when it is zero, which is what cdproto's
// old With... setters did for an optional field Biloba only sometimes sets.
func opt[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

// TimestampTime is the time of a runtime.Timestamp (milliseconds since the epoch).  The zero
// timestamp - Chrome sent none - is the zero time.
func TimestampTime(t runtime.Timestamp) time.Time {
	if t == 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(float64(t)*float64(time.Millisecond)))
}

// EpochTime is the time of a cdp.TimeSinceEpoch (seconds since the epoch).  The zero value - Chrome
// sent none - is the zero time.
func EpochTime(t cdp.TimeSinceEpoch) time.Time {
	if t == 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(float64(t)*float64(time.Second)))
}

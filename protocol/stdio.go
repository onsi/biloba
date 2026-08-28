package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// ServeStdio serves concurrent requests over framed stdin/stdout. A request
// with method "cancel" and id 0 cancels an in-flight request without producing
// its own response.
func ServeStdio(ctx context.Context, server *Server, input io.Reader, output io.Writer) error {
	reader := NewFramedReader(input)
	writer := NewFramedWriter(output)
	callbacks := newStdioCallbackBroker(writer)
	server.SetCallbackInvoker(callbacks)
	ctx, cancelAll := context.WithCancel(ctx)
	defer cancelAll()
	type readResult struct {
		request Request
		err     error
	}
	reads := make(chan readResult, 1)
	go func() {
		for {
			var request Request
			err := reader.Read(&request)
			select {
			case reads <- readResult{request: request, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				var malformed *MalformedFrameError
				if errors.As(err, &malformed) {
					continue
				}
				return
			}
		}
	}()
	writeErrors := make(chan error, 1)

	var activeMu sync.Mutex
	active := map[uint64]context.CancelFunc{}
	var requests sync.WaitGroup
	defer func() {
		callbacks.close()
		activeMu.Lock()
		for _, cancel := range active {
			cancel()
		}
		activeMu.Unlock()
		requests.Wait()
	}()

	for {
		var request Request
		select {
		case <-ctx.Done():
			return nil
		case err := <-writeErrors:
			return fmt.Errorf("write protocol response: %w", err)
		case read := <-reads:
			request = read.request
			if read.err == nil {
				break
			}
			err := read.err
			if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return nil
			}
			var malformed *MalformedFrameError
			if errors.As(err, &malformed) {
				// The header and payload were consumed in full, so the stream is still aligned on a
				// frame boundary: this is one bad request, not a broken pipe, and the other sessions
				// on this worker have done nothing to deserve losing their daemon.  A body that did
				// not parse has no id to correlate against, so answer with id 0 exactly as the
				// zero-id case below does - it cannot be routed to a caller, but it puts the bug on
				// the wire instead of leaving the client to time out against a silent daemon.
				if writeErr := writeResponse(writer, Response{ID: 0, Error: NewError(CodeInvalidArgument, "malformed request frame: "+malformed.Err.Error())}); writeErr != nil {
					return fmt.Errorf("write protocol response: %w", writeErr)
				}
				continue
			}
			return fmt.Errorf("read protocol request: %w", err)
		}
		if request.Method == "cancel" {
			var cancelRequest CancelRequest
			if decodeErr := decodeParams(request.Params, &cancelRequest); decodeErr == nil {
				activeMu.Lock()
				cancel := active[cancelRequest.RequestID]
				activeMu.Unlock()
				if cancel != nil {
					cancel()
				}
			}
			continue
		}
		if request.Method == "callbackResult" && request.ID == 0 {
			var result CallbackResultRequest
			if decodeErr := decodeParams(request.Params, &result); decodeErr == nil {
				callbacks.resolve(result)
			}
			continue
		}
		if request.ID == 0 {
			// A zero id is a client bug, not a desynced stream: the frame decoded fine, so keep
			// serving.  Tearing the daemon down here would take every other session on this worker
			// with it.  The response is uncorrelatable by definition; it exists so the bug shows up
			// on the wire instead of as a bare "bilobad closed stdout".
			if err := writeResponse(writer, Response{ID: 0, Error: NewError(CodeInvalidArgument, "request id must be non-zero")}); err != nil {
				return fmt.Errorf("write protocol response: %w", err)
			}
			continue
		}

		// Exactly one derived context per request: assigning over a WithCancel context to add a
		// deadline drops its cancel func on the floor, and the discarded child stays attached to the
		// daemon-lifetime parent for as long as the daemon lives.  Clients send timeoutMs on nearly
		// every request, so that leak accumulates for the life of the process.  Every path out of
		// here calls the one cancel: the duplicate-id branch below, and the handler's defer.
		var requestCtx context.Context
		var cancel context.CancelFunc
		if request.TimeoutMS > 0 {
			requestCtx, cancel = context.WithTimeout(ctx, time.Duration(request.TimeoutMS)*time.Millisecond)
		} else {
			requestCtx, cancel = context.WithCancel(ctx)
		}
		activeMu.Lock()
		if _, duplicate := active[request.ID]; duplicate {
			activeMu.Unlock()
			cancel()
			if err := writeResponse(writer, Response{ID: request.ID, Error: NewError(CodeInvalidArgument, "duplicate request id")}); err != nil {
				return fmt.Errorf("write protocol response: %w", err)
			}
			continue
		}
		active[request.ID] = cancel
		activeMu.Unlock()

		requests.Add(1)
		go func(request Request) {
			defer requests.Done()
			defer cancel()
			result, protocolErr := server.Dispatch(requestCtx, request.Method, request.Params)
			activeMu.Lock()
			delete(active, request.ID)
			activeMu.Unlock()
			if err := writeResponse(writer, Response{ID: request.ID, Result: result, Error: protocolErr}); err != nil {
				select {
				case writeErrors <- err:
				default:
				}
			}
		}(request)
	}
}

type callbackAnswer struct {
	result WireNetworkOverride
	err    error
}

type stdioCallbackBroker struct {
	writer  *FramedWriter
	next    atomic.Uint64
	mu      sync.Mutex
	pending map[string]chan callbackAnswer
	closed  bool
}

func newStdioCallbackBroker(writer *FramedWriter) *stdioCallbackBroker {
	return &stdioCallbackBroker{writer: writer, pending: map[string]chan callbackAnswer{}}
}

func (b *stdioCallbackBroker) Invoke(ctx context.Context, invocation CallbackInvocation) (WireNetworkOverride, error) {
	id := fmt.Sprintf("callback-%d", b.next.Add(1))
	answer := make(chan callbackAnswer, 1)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return WireNetworkOverride{}, errors.New("callback transport disconnected")
	}
	b.pending[id] = answer
	b.mu.Unlock()
	if err := b.writer.Write(EventFrame{Event: "responseIntercepted", InvocationID: id, CallbackID: invocation.CallbackID, Payload: invocation.Payload}); err != nil {
		b.remove(id)
		return WireNetworkOverride{}, fmt.Errorf("write callback invocation: %w", err)
	}
	select {
	case value := <-answer:
		return value.result, value.err
	case <-ctx.Done():
		if b.remove(id) {
			return WireNetworkOverride{}, errors.New("callback transport disconnected")
		}
		return WireNetworkOverride{}, ctx.Err()
	}
}

func (b *stdioCallbackBroker) resolve(result CallbackResultRequest) {
	b.mu.Lock()
	answer := b.pending[result.InvocationID]
	delete(b.pending, result.InvocationID)
	b.mu.Unlock()
	if answer == nil {
		return
	}
	if result.Error != "" {
		answer <- callbackAnswer{err: errors.New(result.Error)}
		return
	}
	var value WireNetworkOverride
	if len(result.Result) > 0 {
		if err := json.Unmarshal(result.Result, &value); err != nil {
			answer <- callbackAnswer{err: fmt.Errorf("decode callback result: %w", err)}
			return
		}
	}
	answer <- callbackAnswer{result: value}
}

func (b *stdioCallbackBroker) remove(id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, id)
	return b.closed
}

func (b *stdioCallbackBroker) close() {
	b.mu.Lock()
	b.closed = true
	pending := b.pending
	b.pending = map[string]chan callbackAnswer{}
	b.mu.Unlock()
	for _, answer := range pending {
		answer <- callbackAnswer{err: errors.New("callback transport disconnected")}
	}
}

// writeResponse puts response on the wire, degrading a response the writer will not encode into an
// error response rather than into a dead daemon.  An UnwritableFrameError is the writer saying it
// rejected the value before writing a single byte, so the stream is still aligned on a frame
// boundary: the session is intact and only that one request is lost.  The runner's poll trajectory
// is uncapped, which makes an over-large response genuinely reachable - and one daemon carries every
// session on the worker, so dying here would take all of them.
//
// A non-nil error from here is the other kind of failure: bytes did reach the stream, the frame is
// half-written, and nothing downstream can resynchronize.  That one stays fatal and must surface to
// the caller rather than being swallowed.
func writeResponse(writer *FramedWriter, response Response) error {
	err := writer.Write(response)
	if err == nil {
		return nil
	}
	var unwritable *UnwritableFrameError
	if !errors.As(err, &unwritable) {
		return err
	}
	return writer.Write(substituteForUnwritableResponse(response.ID, unwritable))
}

// substituteForUnwritableResponse builds the answer to send in place of a response that will not fit
// in one frame.  DRIVER_ERROR is the code for the daemon failing to deliver on a request whose
// arguments were fine - the same code normalizeError gives an unclassifiable daemon failure - rather
// than INVALID_ARGUMENT, which would blame the caller for the daemon's own output.  The substitute
// carries two integers and a bounded message, so it cannot fail the way the response it replaces
// did.
func substituteForUnwritableResponse(id uint64, unwritable *UnwritableFrameError) Response {
	if unwritable.Size == 0 {
		return Response{ID: id, Error: NewError(CodeDriver, "response could not be encoded: "+boundedDetail(unwritable.Err.Error()))}
	}
	return Response{ID: id, Error: NewError(CodeDriver, fmt.Sprintf("response is %d bytes; the protocol caps a single response at %d bytes", unwritable.Size, MaxFrameSize))}
}

// boundedDetail keeps detail borrowed from another error from reintroducing the size problem the
// substitute exists to report.  A cut that lands mid-rune is fine: encoding/json replaces the
// fragment rather than failing.
func boundedDetail(message string) string {
	const limit = 1024
	if len(message) <= limit {
		return message
	}
	return message[:limit] + "…(truncated)"
}

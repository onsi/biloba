package protocol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// ServeStdio serves concurrent requests over framed stdin/stdout. A request
// with method "cancel" and id 0 cancels an in-flight request without producing
// its own response.
func ServeStdio(ctx context.Context, server *Server, input io.Reader, output io.Writer) error {
	reader := NewFramedReader(input)
	writer := NewFramedWriter(output)
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
				if writeErr := writer.Write(Response{ID: 0, Error: NewError(CodeInvalidArgument, "malformed request frame: "+malformed.Err.Error())}); writeErr != nil {
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
		if request.ID == 0 {
			// A zero id is a client bug, not a desynced stream: the frame decoded fine, so keep
			// serving.  Tearing the daemon down here would take every other session on this worker
			// with it.  The response is uncorrelatable by definition; it exists so the bug shows up
			// on the wire instead of as a bare "bilobad closed stdout".
			if err := writer.Write(Response{ID: 0, Error: NewError(CodeInvalidArgument, "request id must be non-zero")}); err != nil {
				return fmt.Errorf("write protocol response: %w", err)
			}
			continue
		}

		requestCtx, cancel := context.WithCancel(ctx)
		if request.TimeoutMS > 0 {
			requestCtx, cancel = context.WithTimeout(ctx, time.Duration(request.TimeoutMS)*time.Millisecond)
		}
		activeMu.Lock()
		if _, duplicate := active[request.ID]; duplicate {
			activeMu.Unlock()
			cancel()
			if err := writer.Write(Response{ID: request.ID, Error: NewError(CodeInvalidArgument, "duplicate request id")}); err != nil {
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
			if err := writer.Write(Response{ID: request.ID, Result: result, Error: protocolErr}); err != nil {
				select {
				case writeErrors <- err:
				default:
				}
			}
		}(request)
	}
}

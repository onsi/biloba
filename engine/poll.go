package engine

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type ErrorCode string

const (
	CodeInvalidSelector ErrorCode = "invalid_selector"
	// CodeInvalidArgument is the caller handing an operation something it cannot act on - a cookie
	// with no domain, say.  Distinct from CodeActionFailed, which is the target refusing an
	// operation whose arguments were fine: that one can come good on a retry, this one never can.
	CodeInvalidArgument ErrorCode = "invalid_argument"
	CodeBrowserStart    ErrorCode = "browser_start"
	CodeSessionClosed   ErrorCode = "session_closed"
	CodeNavigation      ErrorCode = "navigation"
	CodeJavaScript      ErrorCode = "javascript"
	CodeNotFound        ErrorCode = "not_found"
	CodeActionFailed    ErrorCode = "action_failed"
	CodeBrowserGone     ErrorCode = "browser_gone"
	CodeInvalidScript   ErrorCode = "invalid_script"
	CodePageCrashed     ErrorCode = "page_crashed"
	CodeTimeout         ErrorCode = "timeout"
	CodeCanceled        ErrorCode = "canceled"
	CodeDeadline        ErrorCode = "deadline_exceeded"
	CodeIO              ErrorCode = "io"
)

// Error is a stable structured failure returned by engine operations.
type Error struct {
	Code         ErrorCode
	Operation    string
	Message      string
	Cause        error
	Observed     any
	AttemptCount int
	Attempts     []Attempt
	Diagnostics  Diagnostics
}

func (e *Error) Error() string {
	if e.Operation == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Operation, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// Fatal reports whether polling could ever change this outcome.  A missing element is worth
// retrying; a browser that exited is not, and neither is a selector that will never parse.  This is
// the engine's answer to gomega.StopTrying - the Go API leans on it (see capture.go, visual.go,
// cookie_matchers.go) and the engine needs its own, runner-neutral form for the same reason: a poll
// that retries an unfixable failure spends its whole budget and then reports a timeout, which
// blames the page for something that was never about the page.
func (e *Error) Fatal() bool {
	switch e.Code {
	case CodeBrowserGone, CodeSessionClosed, CodeInvalidSelector, CodeInvalidScript, CodePageCrashed:
		return true
	default:
		return false
	}
}

// FatalError marks an attempt failure that Poll must not retry.  Use it for the cases an Error code
// cannot express - the equivalent of handing gomega.StopTrying to Eventually.
type FatalError struct{ Err error }

func (e *FatalError) Error() string { return e.Err.Error() }
func (e *FatalError) Unwrap() error { return e.Err }

// Fatal wraps err so Poll stops on it instead of retrying until the deadline.
func Fatal(err error) error { return &FatalError{Err: err} }

// IsFatal reports whether err ends the poll immediately.
func IsFatal(err error) bool {
	var fatal *FatalError
	if errors.As(err, &fatal) {
		return true
	}
	var engineErr *Error
	if errors.As(err, &engineErr) {
		return engineErr.Fatal()
	}
	return false
}

type Observation struct {
	Value any
	Found *bool
}

type Attempt struct {
	Number      int
	StartedAt   time.Time
	Duration    time.Duration
	Observation Observation
	Error       string
}

type PollPolicy struct {
	Timeout  time.Duration
	Interval time.Duration
}

type PollResult struct {
	Final        Observation
	AttemptCount int
	Attempts     []Attempt
	StartedAt    time.Time
	Duration     time.Duration
}

type Assertion func(context.Context) (Observation, bool, error)

// Poll retries an entire one-attempt assertion in Go until it succeeds or its context expires.
func Poll(ctx context.Context, policy PollPolicy, assertion Assertion) (PollResult, error) {
	started := time.Now()
	if policy.Interval <= 0 {
		policy.Interval = 10 * time.Millisecond
	}
	pollCtx := ctx
	cancel := func() {}
	if policy.Timeout > 0 {
		pollCtx, cancel = context.WithTimeout(ctx, policy.Timeout)
	}
	defer cancel()

	result := PollResult{StartedAt: started}
	for {
		attemptStarted := time.Now()
		observation, matched, attemptErr := assertion(pollCtx)
		attempt := Attempt{
			Number:      len(result.Attempts) + 1,
			StartedAt:   attemptStarted,
			Duration:    time.Since(attemptStarted),
			Observation: observation,
		}
		if attemptErr != nil {
			attempt.Error = attemptErr.Error()
		}
		result.Final = observation
		result.Attempts = append(result.Attempts, attempt)
		result.AttemptCount = len(result.Attempts)
		result.Duration = time.Since(started)
		if attemptErr != nil && IsFatal(attemptErr) {
			// Report it as what it is, now, with the attempts made so far.  Burning the rest of the
			// budget would turn "the browser exited" into "your assertion timed out".
			return result, fatalPollError(result, attemptErr)
		}
		if matched && attemptErr == nil {
			return result, nil
		}

		timer := time.NewTimer(policy.Interval)
		select {
		case <-pollCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			code := CodeCanceled
			if ctx.Err() != nil {
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					code = CodeDeadline
				}
			} else if errors.Is(pollCtx.Err(), context.DeadlineExceeded) {
				code = CodeTimeout
			}
			return result, &Error{
				Code:         code,
				Operation:    "poll",
				Message:      pollCtx.Err().Error(),
				Cause:        pollCtx.Err(),
				Observed:     result.Final.Value,
				AttemptCount: result.AttemptCount,
				Attempts:     result.Attempts,
			}
		case <-timer.C:
		}
	}
}

// fatalPollError preserves the underlying failure's own code and message so the caller sees why the
// poll stopped, not merely that it did, while still carrying the trajectory it collected.
func fatalPollError(result PollResult, cause error) *Error {
	failure := &Error{
		Code:         CodeActionFailed,
		Operation:    "poll",
		Message:      cause.Error(),
		Cause:        cause,
		Observed:     result.Final.Value,
		AttemptCount: result.AttemptCount,
		Attempts:     result.Attempts,
	}
	var engineErr *Error
	if errors.As(cause, &engineErr) {
		failure.Code, failure.Operation = engineErr.Code, engineErr.Operation
	}
	return failure
}

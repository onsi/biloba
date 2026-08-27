package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/onsi/biloba/engine"
	"github.com/onsi/biloba/protocol"
)

type engineBackend struct{ browser *engine.Browser }

func (b *engineBackend) OpenSession(ctx context.Context) (protocol.Session, error) {
	session, err := b.browser.OpenSession(ctx)
	if err != nil {
		return nil, engineRPCError(err)
	}
	return &engineSession{session: session}, nil
}

func (b *engineBackend) Close() error { return b.browser.Close() }

type engineSession struct{ session *engine.Session }

func (s *engineSession) Prepare(ctx context.Context) error { return s.session.Prepare(ctx) }
func (s *engineSession) Close() error                      { return s.session.Close() }

func (s *engineSession) Execute(ctx context.Context, operation protocol.Operation) (protocol.Result, error) {
	started := time.Now()
	switch operation.Kind {
	case protocol.OperationNavigate:
		return oneAttempt(started, s.session.NavigateWithStatus(ctx, operation.URL, operation.ExpectedStatus))
	case protocol.OperationSetCookies:
		cookies := make([]engine.Cookie, len(operation.Cookies))
		for i, cookie := range operation.Cookies {
			cookies[i] = engine.Cookie{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path, Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly, SameSite: cookie.SameSite}
			if cookie.ExpiresUnix != 0 {
				cookies[i].Expires = time.UnixMilli(int64(cookie.ExpiresUnix * 1000))
			}
		}
		return oneAttempt(started, s.session.SetCookies(ctx, cookies))
	case protocol.OperationClick:
		selector, err := selectorFromProtocol(operation.Locator)
		if err != nil {
			return protocol.Result{}, err
		}
		return s.poll(ctx, operation, func(ctx context.Context) (engine.Observation, bool, error) {
			err := s.session.Click(ctx, selector)
			return engine.Observation{}, err == nil, err
		})
	case protocol.OperationSetValue:
		selector, err := selectorFromProtocol(operation.Locator)
		if err != nil {
			return protocol.Result{}, err
		}
		var value any
		if err := json.Unmarshal([]byte(operation.ValueJSON), &value); err != nil {
			return protocol.Result{}, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("value_json: %v", err))
		}
		return s.poll(ctx, operation, func(ctx context.Context) (engine.Observation, bool, error) {
			err := s.session.SetValue(ctx, selector, value)
			return engine.Observation{}, err == nil, err
		})
	case protocol.OperationEvaluate:
		script, err := evaluationScript(operation.Expression, operation.ArgumentsJSON, operation.Invoke)
		if err != nil {
			return protocol.Result{}, err
		}
		value, err := s.session.Evaluate(ctx, script)
		if err != nil {
			return protocol.Result{}, engineRPCError(err)
		}
		return observedResult(started, value), nil
	case protocol.OperationAssert:
		assertion, err := s.assertion(operation.Assertion)
		if err != nil {
			return protocol.Result{}, err
		}
		return s.poll(ctx, operation, assertion)
	default:
		return protocol.Result{}, protocol.NewError(protocol.CodeInvalidArgument, "unsupported operation")
	}
}

// evaluationScript turns an evaluate request into one snippet.  `invoke` is what says whether
// `expression` is a value to evaluate or a function to call: without it the meaning of the
// expression hangs off how many arguments happen to accompany it, so `(a) => a + 1` with no
// arguments evaluates to the function source rather than calling it.  A nil `invoke` is the
// pre-`invoke` client, which is entitled to that inference.
func evaluationScript(expression, argumentsJSON string, invoke *bool) (string, error) {
	arguments, err := evaluationArguments(argumentsJSON)
	if err != nil {
		return "", err
	}
	if invoke != nil && !*invoke {
		if len(arguments) > 0 {
			return "", protocol.NewError(protocol.CodeInvalidArgument, "arguments_json requires invoke: true")
		}
		return expression, nil
	}
	if invoke == nil && len(arguments) == 0 {
		return expression, nil
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return "", protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("arguments_json: %v", err))
	}
	return fmt.Sprintf("(%s)(...%s)", expression, encoded), nil
}

func evaluationArguments(argumentsJSON string) ([]any, error) {
	if strings.TrimSpace(argumentsJSON) == "" {
		return nil, nil
	}
	var arguments []any
	if err := json.Unmarshal([]byte(argumentsJSON), &arguments); err != nil {
		return nil, protocol.NewError(protocol.CodeInvalidArgument, fmt.Sprintf("arguments_json must be a JSON array: %v", err))
	}
	return arguments, nil
}

func (s *engineSession) poll(ctx context.Context, operation protocol.Operation, assertion engine.Assertion) (protocol.Result, error) {
	policy := engine.PollPolicy{Timeout: operation.Poll.Timeout, Interval: operation.Poll.Interval}
	if policy.Timeout <= 0 {
		policy.Timeout = time.Second
	}
	result, pollErr := engine.Poll(ctx, policy, assertion)
	converted := pollResult(result, pollErr == nil)
	if pollErr == nil {
		return converted, nil
	}
	// A poll that stopped because retrying was pointless is a failure of a different kind from a
	// poll that ran out of time, and has to reach the client as one - reporting "the browser is
	// gone" as matched:false would render as an assertion timeout and send the reader to the page.
	if engine.IsFatal(pollErr) {
		return protocol.Result{}, engineRPCError(pollErr)
	}
	if errors.Is(pollErr, context.Canceled) && errors.Is(ctx.Err(), context.Canceled) {
		return protocol.Result{}, protocol.NewError(protocol.CodeCancelled, ctx.Err().Error())
	}
	if errors.Is(pollErr, context.DeadlineExceeded) && ctx.Err() != nil {
		return protocol.Result{}, protocol.NewError(protocol.CodeTimeout, ctx.Err().Error())
	}
	diagnosticCtx, cancel := context.WithTimeout(context.Background(), diagnosticsBudget(ctx, time.Now()))
	defer cancel()
	diagnostics, diagnosticErr := s.session.CaptureDiagnostics(diagnosticCtx, "biloba-failure")
	converted.Diagnostics = protocol.Diagnostics{
		Locator: locatorDescription(operation), Expected: expectedDescription(operation),
		DOMOutline: diagnostics.DOMOutline, ScreenshotPath: diagnostics.ScreenshotPath, DaemonDetail: pollErr.Error(),
	}
	if diagnosticErr != nil {
		converted.Diagnostics.DaemonDetail += "; capture diagnostics: " + diagnosticErr.Error()
	}
	return converted, nil
}

// The client arms its own timer at the very deadline it puts on the request, so capturing
// diagnostics has to fit inside what is left of that deadline: a screenshot that lands late is a
// response nobody is waiting for any more, and the failure the daemon exists to describe arrives
// as a bare "request timed out" with no trajectory, outline, or screenshot at all.  Budget from
// the time actually remaining, reserving enough of it to encode, frame, and write the answer.
const (
	diagnosticsCap     = 2 * time.Second
	diagnosticsReserve = 300 * time.Millisecond
	diagnosticsFloor   = 100 * time.Millisecond
)

func diagnosticsBudget(ctx context.Context, now time.Time) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return diagnosticsCap // nothing to race: an open-ended request gets the fixed budget
	}
	switch budget := deadline.Sub(now) - diagnosticsReserve; {
	case budget > diagnosticsCap:
		return diagnosticsCap
	case budget < diagnosticsFloor:
		// Already out of room.  Try anyway, briefly: a capture that fails fast still leaves the
		// trajectory on the response, and a long shot at an outline beats a certain miss.
		return diagnosticsFloor
	default:
		return budget
	}
}

func (s *engineSession) assertion(assertion protocol.Assertion) (engine.Assertion, error) {
	var selector engine.Selector
	var err error
	if assertion.Kind != protocol.AssertionURL && assertion.Kind != protocol.AssertionEvaluate {
		selector, err = selectorFromProtocol(assertion.Locator)
		if err != nil {
			return nil, err
		}
	}
	return func(ctx context.Context) (engine.Observation, bool, error) {
		var observation engine.Observation
		var readErr error
		switch assertion.Kind {
		case protocol.AssertionVisible:
			observation, readErr = s.session.Visible(ctx, selector)
			visible, _ := observation.Value.(bool)
			return observation, visible, readErr
		case protocol.AssertionText:
			observation, readErr = s.session.Text(ctx, selector)
		case protocol.AssertionCount:
			observation, readErr = s.session.Count(ctx, selector)
			return observation, numericEqual(observation.Value, assertion.ExpectedCount), readErr
		case protocol.AssertionAttribute:
			observation, readErr = s.session.Attribute(ctx, selector, assertion.Attribute)
		case protocol.AssertionValue:
			observation, readErr = s.session.Value(ctx, selector)
			matched, compareErr := jsonEqual(observation.Value, assertion.ExpectedJSON)
			if compareErr != nil {
				return observation, false, compareErr
			}
			return observation, matched, readErr
		case protocol.AssertionURL:
			observation, readErr = s.session.URL(ctx)
		case protocol.AssertionEvaluate:
			var value any
			value, readErr = s.session.Evaluate(ctx, assertion.Expression)
			observation = engine.Observation{Value: value}
			matched, compareErr := jsonEqual(value, assertion.ExpectedJSON)
			if compareErr != nil {
				return observation, false, compareErr
			}
			return observation, matched, readErr
		default:
			// Unreachable from the wire (assertionFromWire rejects unknown kinds first), but if it
			// ever were reached, retrying would turn a rejected request into an assertion timeout.
			return observation, false, engine.Fatal(protocol.NewError(protocol.CodeInvalidArgument, "unsupported assertion"))
		}
		return observation, stringMatches(observation.Value, assertion.ExpectedString, assertion.Match), readErr
	}, nil
}

func selectorFromProtocol(locator protocol.Locator) (engine.Selector, error) {
	mode := engine.Exact
	if locator.Match == protocol.MatchContains {
		mode = engine.Contains
	}
	var selector engine.Selector
	switch locator.Kind {
	case protocol.LocatorCSS:
		selector = engine.CSS(locator.Value)
	case protocol.LocatorTestID:
		selector = engine.TestID(locator.Value)
	case protocol.LocatorText:
		selector = engine.Text(locator.Value, mode)
	case protocol.LocatorRole:
		selector = engine.Role(locator.Role, locator.Name, mode)
	default:
		return engine.Selector{}, protocol.NewError(protocol.CodeInvalidArgument, "unsupported locator")
	}
	if locator.First {
		selector = selector.First()
	}
	return selector, nil
}

func oneAttempt(started time.Time, err error) (protocol.Result, error) {
	if err != nil {
		return protocol.Result{}, engineRPCError(err)
	}
	return protocol.Result{Matched: true, Attempts: 1, StartedAt: started, Elapsed: time.Since(started)}, nil
}

func observedResult(started time.Time, value any) protocol.Result {
	return protocol.Result{Matched: true, ObservedJSON: marshalJSON(value), Attempts: 1, StartedAt: started, Elapsed: time.Since(started)}
}

func pollResult(result engine.PollResult, matched bool) protocol.Result {
	trajectory := make([]protocol.Observation, len(result.Attempts))
	for i, attempt := range result.Attempts {
		trajectory[i] = protocol.Observation{Attempt: uint32(attempt.Number), Elapsed: attempt.StartedAt.Sub(result.StartedAt), ObservedJSON: marshalJSON(attempt.Observation.Value), RetryReason: attempt.Error}
	}
	return protocol.Result{Matched: matched, ObservedJSON: marshalJSON(result.Final.Value), Attempts: uint32(result.AttemptCount), Trajectory: trajectory, StartedAt: result.StartedAt, Elapsed: result.Duration}
}

func stringMatches(value any, expected string, mode protocol.MatchMode) bool {
	observed, ok := value.(string)
	if !ok {
		return false
	}
	if mode == protocol.MatchContains {
		return strings.Contains(observed, expected)
	}
	return observed == expected
}

func numericEqual(value any, expected int64) bool {
	switch value := value.(type) {
	case int:
		return int64(value) == expected
	case int64:
		return value == expected
	case float64:
		return value == float64(expected)
	default:
		return false
	}
}

// jsonEqual compares an observation against the caller's expected value.  A malformed expectation
// is fatal rather than simply unequal: no amount of polling turns invalid JSON into a value, and
// reporting it as a mismatch would spend the whole budget and then blame the page - quoting what
// was observed while saying nothing about the expectation that never parsed.  This is the same
// judgement capture.go makes with gomega.StopTrying for a decode into the wrong pointer type.
func jsonEqual(observed any, expectedJSON string) (bool, error) {
	var expected any
	if err := json.Unmarshal([]byte(expectedJSON), &expected); err != nil {
		return false, engine.Fatal(protocol.NewError(protocol.CodeInvalidArgument,
			fmt.Sprintf("expectedJson is not valid JSON: %v", err)))
	}
	return reflect.DeepEqual(observed, expected), nil
}

func marshalJSON(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%q", fmt.Sprint(value))
	}
	return string(encoded)
}

func locatorDescription(operation protocol.Operation) string {
	locator := operation.Locator
	if operation.Kind == protocol.OperationAssert {
		locator = operation.Assertion.Locator
	}
	selector, err := selectorFromProtocol(locator)
	if err != nil {
		return ""
	}
	return selector.Description()
}

func expectedDescription(operation protocol.Operation) string {
	if operation.Kind != protocol.OperationAssert {
		return "operation to succeed"
	}
	assertion := operation.Assertion
	switch assertion.Kind {
	case protocol.AssertionVisible:
		return "visible"
	case protocol.AssertionCount:
		return fmt.Sprint(assertion.ExpectedCount)
	case protocol.AssertionValue, protocol.AssertionEvaluate:
		return assertion.ExpectedJSON
	default:
		return assertion.ExpectedString
	}
}

// engineProtocolCodes is the whole engine-to-protocol error vocabulary, in one place.  A code that
// is missing here reaches the client as a generic DRIVER_ERROR - which reads as "the daemon broke"
// for a failure the page caused, and buries the one thing the client could act on (a page-level
// JavaScript error is JAVASCRIPT_ERROR; a navigation that never landed leaves the target not
// ready).  The two codes with no honest counterpart are listed with the reason rather than left
// out, so a code added to the engine shows up as a missing key - see the exhaustiveness spec in
// main_test.go - instead of silently inheriting a plausible-looking default.
var engineProtocolCodes = map[engine.ErrorCode]protocol.ErrorCode{
	engine.CodeInvalidSelector: protocol.CodeInvalidArgument,
	engine.CodeInvalidArgument: protocol.CodeInvalidArgument,
	engine.CodeSessionClosed:   protocol.CodeTargetNotFound,
	engine.CodeNotFound:        protocol.CodeTargetNotFound,
	// The target was found and refused the operation - a click on a hidden element.  That is a page
	// state, not a driver fault, and it is the one bucket where a retry might succeed.
	engine.CodeActionFailed: protocol.CodeTargetNotReady,
	// A navigation that landed on a status the caller did not ask for.  Deliberately not
	// TARGET_NOT_READY: the page loaded fine, so waiting will never change the answer.
	engine.CodeNavigation:    protocol.CodeNavigation,
	engine.CodeJavaScript:    protocol.CodeJavaScript,
	engine.CodeInvalidScript: protocol.CodeJavaScript,
	engine.CodeBrowserGone:   protocol.CodeBrowserGone,
	engine.CodePageCrashed:   protocol.CodePageCrashed,
	engine.CodeCanceled:      protocol.CodeCancelled,
	engine.CodeTimeout:       protocol.CodeTimeout,
	engine.CodeDeadline:      protocol.CodeTimeout,
	// No counterpart, deliberately.  Both are the daemon failing to bring its own Chrome up, before
	// there is a session to report against - BROWSER_GONE means a live Chrome died underneath a
	// worker, which is a different thing to tell a client.
	engine.CodeBrowserStart: protocol.CodeDriver,
	engine.CodeIO:           protocol.CodeDriver,
}

func engineRPCError(err error) error {
	// A ProtocolError that travelled out through the engine (an engine.Fatal wrapping one, say)
	// already knows its own code - do not flatten it to DRIVER_ERROR on the way past.
	var protocolErr *protocol.ProtocolError
	if errors.As(err, &protocolErr) {
		return protocolErr
	}
	var engineErr *engine.Error
	if !errors.As(err, &engineErr) {
		return protocol.NewError(protocol.CodeDriver, err.Error())
	}
	code, ok := engineProtocolCodes[engineErr.Code]
	if !ok {
		code = protocol.CodeDriver
	}
	return protocol.NewError(code, engineErr.Error())
}

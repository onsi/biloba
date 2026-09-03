package protocol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const Version = "1"

var Capabilities = []string{
	"locator.css", "locator.test_id", "locator.text", "locator.role", "locator.first",
	"session.prepare", "navigation", "cookies", "action.click", "action.set_value",
	"evaluate", "assert.visible", "assert.text", "assert.count", "assert.attribute",
	"assert.value", "assert.url", "assert.evaluate", "poll.server_side", "diagnostics.structured",
}

type ErrorCode string

const (
	CodeInvalidArgument ErrorCode = "INVALID_ARGUMENT"
	CodeTimeout         ErrorCode = "TIMEOUT"
	CodeTargetNotFound  ErrorCode = "TARGET_NOT_FOUND"
	// CodeTargetNotReady is a target that was found and refused the operation - a click on a hidden
	// element, say.  It means "not yet", so it is the one failure code that implies a retry might
	// succeed; do not reach for it as a general-purpose bucket for page-caused errors that will
	// never come good (an argument the caller got wrong is INVALID_ARGUMENT, a navigation that
	// landed on the wrong status is NAVIGATION).
	CodeTargetNotReady ErrorCode = "TARGET_NOT_READY"
	// CodeNavigation is a navigation whose main document answered with a status the caller did not
	// ask for.  Distinct from TARGET_NOT_READY because waiting cannot change it: the page loaded,
	// it simply is not the page the caller said it expected.  Navigate to the same URL with a
	// matching expectedStatus if the error page is what you meant to test.
	CodeNavigation       ErrorCode = "NAVIGATION"
	CodeJavaScript       ErrorCode = "JAVASCRIPT_ERROR"
	CodeProtocolMismatch ErrorCode = "PROTOCOL_MISMATCH"
	CodeDriverClosed     ErrorCode = "DRIVER_CLOSED"
	// CodeBrowserGone is the shared Chrome exiting or crashing underneath a worker - distinct from
	// DRIVER_CLOSED (this worker's daemon died) and from CANCELLED (the caller asked to stop).
	CodeBrowserGone ErrorCode = "BROWSER_GONE"
	// CodePageCrashed is this session's renderer dying while the browser itself lives on.  Chrome
	// stops answering for that target rather than erroring, so without this the operation would
	// simply hang until its deadline.  Navigating the session again recovers it.
	CodePageCrashed ErrorCode = "PAGE_CRASHED"
	CodeDriver      ErrorCode = "DRIVER_ERROR"
	CodeCancelled   ErrorCode = "CANCELLED"
)

type ProtocolError struct {
	Code        ErrorCode    `json:"code"`
	Message     string       `json:"message"`
	Diagnostics *Diagnostics `json:"diagnostics,omitempty"`
}

func (e *ProtocolError) Error() string { return e.Message }

func NewError(code ErrorCode, message string) *ProtocolError {
	return &ProtocolError{Code: code, Message: message}
}

type Backend interface {
	OpenSession(context.Context) (Session, error)
	Close() error
}

type Session interface {
	Prepare(context.Context) error
	Execute(context.Context, Operation) (Result, error)
	Close() error
}

type OperationKind uint8

const (
	OperationNavigate OperationKind = iota + 1
	OperationSetCookies
	OperationClick
	OperationSetValue
	OperationEvaluate
	OperationAssert
)

type Operation struct {
	Kind OperationKind
	URL  string
	// ExpectedStatus is always concrete by the time an Operation is built - Dispatch resolves the
	// request's omitted 0 to 200 - so nothing downstream has to know the default.
	ExpectedStatus int
	Cookies        []Cookie
	Locator        Locator
	Poll           PollPolicy
	ValueJSON      string
	Expression     string
	ArgumentsJSON  string
	Invoke         *bool
	Assertion      Assertion
}

type LocatorKind uint8

const (
	LocatorCSS LocatorKind = iota + 1
	LocatorTestID
	LocatorText
	LocatorRole
)

type MatchMode uint8

const (
	MatchExact MatchMode = iota + 1
	MatchContains
)

type Locator struct {
	Kind  LocatorKind
	Value string
	Role  string
	Name  string
	Match MatchMode
	First bool
}

type PollPolicy struct {
	Timeout  time.Duration
	Interval time.Duration
}

type Cookie struct {
	Name, Value, Domain, Path, SameSite string
	Secure, HTTPOnly                    bool
	ExpiresUnix                         float64
}

type AssertionKind uint8

const (
	AssertionVisible AssertionKind = iota + 1
	AssertionText
	AssertionCount
	AssertionAttribute
	AssertionValue
	AssertionURL
	AssertionEvaluate
)

type Assertion struct {
	Kind           AssertionKind
	Locator        Locator
	Attribute      string
	Expression     string
	ExpectedString string
	ExpectedCount  int64
	ExpectedJSON   string
	Match          MatchMode
}

type Result struct {
	Matched      bool
	ObservedJSON string
	Attempts     uint32
	Trajectory   []Observation
	StartedAt    time.Time
	Elapsed      time.Duration
	Diagnostics  Diagnostics
}

type Observation struct {
	Attempt      uint32
	Elapsed      time.Duration
	ObservedJSON string
	RetryReason  string
}

type Diagnostics struct {
	Locator        string `json:"locator,omitempty"`
	Expected       string `json:"expected,omitempty"`
	DOMOutline     string `json:"domOutline,omitempty"`
	ScreenshotPath string `json:"screenshotPath,omitempty"`
	DaemonDetail   string `json:"daemonDetail,omitempty"`
}

// The wire structs below are the authoritative JSON protocol definition.  The
// TypeScript declarations and golden frames are generated from these shapes.
type Request struct {
	ID        uint64          `json:"id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params,omitempty"`
	TimeoutMS int64           `json:"timeoutMs,omitempty"`
}

type Response struct {
	ID     uint64         `json:"id"`
	Result any            `json:"result,omitempty"`
	Error  *ProtocolError `json:"error,omitempty"`
}

type HandshakeRequest struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type HandshakeResponse struct {
	ProtocolVersion string   `json:"protocolVersion"`
	Capabilities    []string `json:"capabilities"`
}

type OpenSessionResponse struct {
	SessionID string `json:"sessionId"`
}

type SessionRequest struct {
	SessionID string `json:"sessionId"`
}

type NavigateRequest struct {
	SessionID string `json:"sessionId"`
	URL       string `json:"url"`
	// ExpectedStatus is the HTTP status the main document must answer with.  Omitted (0) means 200,
	// so a client that does not care keeps sending exactly what it sent before.
	ExpectedStatus int `json:"expectedStatus,omitempty"`
}

type PollOptions struct {
	TimeoutMS  int64 `json:"timeoutMs,omitempty"`
	IntervalMS int64 `json:"intervalMs,omitempty"`
}

type WireLocator struct {
	Kind  string `json:"kind"`
	Value string `json:"value,omitempty"`
	Role  string `json:"role,omitempty"`
	Name  string `json:"name,omitempty"`
	Match string `json:"match,omitempty"`
	First bool   `json:"first"`
}

type WireCookie struct {
	Name        string  `json:"name"`
	Value       string  `json:"value"`
	Domain      string  `json:"domain,omitempty"`
	Path        string  `json:"path,omitempty"`
	ExpiresUnix float64 `json:"expiresUnix,omitempty"`
	Secure      bool    `json:"secure,omitempty"`
	HTTPOnly    bool    `json:"httpOnly,omitempty"`
	SameSite    string  `json:"sameSite,omitempty"`
}

type SetCookiesRequest struct {
	SessionID string       `json:"sessionId"`
	Cookies   []WireCookie `json:"cookies"`
}

type LocatorRequest struct {
	SessionID string       `json:"sessionId"`
	Locator   *WireLocator `json:"locator"`
	Poll      PollOptions  `json:"poll,omitempty"`
}

type SetValueRequest struct {
	SessionID string       `json:"sessionId"`
	Locator   *WireLocator `json:"locator"`
	ValueJSON string       `json:"valueJson"`
	Poll      PollOptions  `json:"poll,omitempty"`
}

type EvaluateRequest struct {
	SessionID     string `json:"sessionId"`
	Expression    string `json:"expression"`
	ArgumentsJSON string `json:"argumentsJson,omitempty"`
	// Invoke says what Expression means, so that meaning does not hang off how many arguments came
	// with it: true calls Expression as a function with ArgumentsJson's elements spread into it -
	// including when that array is empty - and false evaluates Expression verbatim.  It is a
	// pointer because absent is a third answer: clients written before Invoke existed get the old
	// inference (arguments present means call it), and new clients should always send it.
	Invoke *bool `json:"invoke,omitempty"`
}

type WireAssertion struct {
	Kind           string       `json:"kind"`
	Locator        *WireLocator `json:"locator,omitempty"`
	Attribute      string       `json:"attribute,omitempty"`
	Expression     string       `json:"expression,omitempty"`
	ExpectedString string       `json:"expectedString,omitempty"`
	ExpectedCount  int64        `json:"expectedCount,omitempty"`
	ExpectedJSON   string       `json:"expectedJson,omitempty"`
	Match          string       `json:"match,omitempty"`
}

type AssertRequest struct {
	SessionID string         `json:"sessionId"`
	Assertion *WireAssertion `json:"assertion"`
	Poll      PollOptions    `json:"poll,omitempty"`
}

type CancelRequest struct {
	RequestID uint64 `json:"requestId"`
}

type OperationResult struct {
	Matched      bool              `json:"matched"`
	ObservedJSON string            `json:"observedJson,omitempty"`
	AttemptCount uint32            `json:"attemptCount"`
	Trajectory   []PollObservation `json:"trajectory,omitempty"`
	Timings      Timings           `json:"timings"`
	// A pointer, because `omitempty` does nothing for a struct: a value here put an empty
	// "diagnostics":{} on every successful response while the generated TypeScript declared the
	// field optional.  See TestProtocol's encoding specs.
	Diagnostics      *Diagnostics `json:"diagnostics,omitempty"`
	RPCRequestCount  uint32       `json:"rpcRequestCount"`
	RPCResponseCount uint32       `json:"rpcResponseCount"`
}

type PollObservation struct {
	Attempt      uint32 `json:"attempt"`
	ElapsedMS    int64  `json:"elapsedMs"`
	ObservedJSON string `json:"observedJson,omitempty"`
	RetryReason  string `json:"retryReason,omitempty"`
}

type Timings struct {
	StartedUnixMS int64 `json:"startedUnixMs"`
	ElapsedMS     int64 `json:"elapsedMs"`
}

type Server struct {
	backend  Backend
	mu       sync.Mutex
	sessions map[string]*sessionEntry
}

type sessionEntry struct {
	mu      sync.Mutex
	session Session
}

func NewServer(backend Backend) *Server {
	return &Server{backend: backend, sessions: map[string]*sessionEntry{}}
}

func (s *Server) Dispatch(ctx context.Context, method string, params json.RawMessage) (any, *ProtocolError) {
	switch method {
	case "handshake":
		var request HandshakeRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if request.ProtocolVersion != Version {
			return nil, NewError(CodeProtocolMismatch, fmt.Sprintf("protocol version mismatch: client=%q daemon=%q", request.ProtocolVersion, Version))
		}
		return HandshakeResponse{ProtocolVersion: Version, Capabilities: append([]string(nil), Capabilities...)}, nil
	case "openSession":
		session, err := s.backend.OpenSession(ctx)
		if err != nil {
			return nil, normalizeError(err)
		}
		id, idErr := randomID()
		if idErr != nil {
			_ = session.Close()
			return nil, NewError(CodeDriver, "generate session id")
		}
		s.mu.Lock()
		s.sessions[id] = &sessionEntry{session: session}
		s.mu.Unlock()
		return OpenSessionResponse{SessionID: id}, nil
	case "prepareSession":
		var request SessionRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		entry, err := s.session(request.SessionID)
		if err != nil {
			return nil, err
		}
		entry.mu.Lock()
		defer entry.mu.Unlock()
		if prepareErr := entry.session.Prepare(ctx); prepareErr != nil {
			return nil, normalizeError(prepareErr)
		}
		return struct{}{}, nil
	case "closeSession":
		var request SessionRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		return s.closeSession(request.SessionID)
	case "navigate":
		var request NavigateRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if request.URL == "" {
			return nil, NewError(CodeInvalidArgument, "url is required")
		}
		expectedStatus := request.ExpectedStatus
		if expectedStatus == 0 {
			expectedStatus = http.StatusOK
		}
		if expectedStatus < 100 || expectedStatus > 599 {
			return nil, NewError(CodeInvalidArgument, fmt.Sprintf("expectedStatus must be a valid HTTP status code, got %d", expectedStatus))
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationNavigate, URL: request.URL, ExpectedStatus: expectedStatus})
	case "setCookies":
		var request SetCookiesRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		cookies := make([]Cookie, len(request.Cookies))
		for i, cookie := range request.Cookies {
			if cookie.Name == "" {
				return nil, NewError(CodeInvalidArgument, fmt.Sprintf("cookies[%d].name is required", i))
			}
			cookies[i] = Cookie{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path, Secure: cookie.Secure, HTTPOnly: cookie.HTTPOnly, ExpiresUnix: cookie.ExpiresUnix, SameSite: cookie.SameSite}
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationSetCookies, Cookies: cookies})
	case "click":
		var request LocatorRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		locator, err := locatorFromWire(request.Locator)
		if err != nil {
			return nil, err
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationClick, Locator: locator, Poll: pollFromWire(request.Poll)})
	case "setValue":
		var request SetValueRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		locator, err := locatorFromWire(request.Locator)
		if err != nil {
			return nil, err
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationSetValue, Locator: locator, Poll: pollFromWire(request.Poll), ValueJSON: request.ValueJSON})
	case "evaluate":
		var request EvaluateRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if request.Expression == "" {
			return nil, NewError(CodeInvalidArgument, "expression is required")
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationEvaluate, Expression: request.Expression, ArgumentsJSON: request.ArgumentsJSON, Invoke: request.Invoke})
	case "assert":
		var request AssertRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		assertion, err := assertionFromWire(request.Assertion)
		if err != nil {
			return nil, err
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationAssert, Assertion: assertion, Poll: pollFromWire(request.Poll)})
	default:
		return nil, NewError(CodeInvalidArgument, fmt.Sprintf("unsupported method %q", method))
	}
}

func decodeParams(params json.RawMessage, destination any) *ProtocolError {
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(params, destination); err != nil {
		return NewError(CodeInvalidArgument, fmt.Sprintf("invalid request parameters: %v", err))
	}
	return nil
}

func (s *Server) execute(ctx context.Context, sessionID string, operation Operation) (any, *ProtocolError) {
	entry, err := s.session(sessionID)
	if err != nil {
		return nil, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	result, executeErr := entry.session.Execute(ctx, operation)
	if executeErr != nil {
		return nil, normalizeError(executeErr)
	}
	return resultToWire(result), nil
}

func (s *Server) session(id string) (*sessionEntry, *ProtocolError) {
	if id == "" {
		return nil, NewError(CodeInvalidArgument, "sessionId is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, exists := s.sessions[id]
	if !exists {
		return nil, NewError(CodeTargetNotFound, "session not found")
	}
	return entry, nil
}

func (s *Server) closeSession(id string) (any, *ProtocolError) {
	if id == "" {
		return nil, NewError(CodeInvalidArgument, "sessionId is required")
	}
	s.mu.Lock()
	entry, exists := s.sessions[id]
	if exists {
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	if !exists {
		return nil, NewError(CodeTargetNotFound, "session not found")
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if err := entry.session.Close(); err != nil {
		return nil, normalizeError(err)
	}
	return struct{}{}, nil
}

func (s *Server) Close() error {
	s.mu.Lock()
	entries := make([]*sessionEntry, 0, len(s.sessions))
	for id, entry := range s.sessions {
		entries = append(entries, entry)
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	var closeErrors []error
	for _, entry := range entries {
		entry.mu.Lock()
		closeErrors = append(closeErrors, entry.session.Close())
		entry.mu.Unlock()
	}
	closeErrors = append(closeErrors, s.backend.Close())
	return errors.Join(closeErrors...)
}

func randomID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func pollFromWire(poll PollOptions) PollPolicy {
	return PollPolicy{Timeout: time.Duration(poll.TimeoutMS) * time.Millisecond, Interval: time.Duration(poll.IntervalMS) * time.Millisecond}
}

func locatorFromWire(locator *WireLocator) (Locator, *ProtocolError) {
	if locator == nil {
		return Locator{}, NewError(CodeInvalidArgument, "locator is required")
	}
	kinds := map[string]LocatorKind{"CSS": LocatorCSS, "TEST_ID": LocatorTestID, "TEXT": LocatorText, "ROLE": LocatorRole}
	kind, exists := kinds[locator.Kind]
	if !exists {
		return Locator{}, NewError(CodeInvalidArgument, "locator kind is required")
	}
	if kind == LocatorRole && locator.Role == "" {
		return Locator{}, NewError(CodeInvalidArgument, "role locator requires role")
	}
	if kind != LocatorRole && locator.Value == "" {
		return Locator{}, NewError(CodeInvalidArgument, "locator value is required")
	}
	match := MatchExact
	if locator.Match == "CONTAINS" {
		match = MatchContains
	} else if locator.Match != "" && locator.Match != "EXACT" {
		return Locator{}, NewError(CodeInvalidArgument, "unsupported locator match mode")
	}
	return Locator{Kind: kind, Value: locator.Value, Role: locator.Role, Name: locator.Name, Match: match, First: locator.First}, nil
}

func assertionFromWire(assertion *WireAssertion) (Assertion, *ProtocolError) {
	if assertion == nil || assertion.Kind == "" {
		return Assertion{}, NewError(CodeInvalidArgument, "assertion kind is required")
	}
	kinds := map[string]AssertionKind{"VISIBLE": AssertionVisible, "TEXT": AssertionText, "COUNT": AssertionCount, "ATTRIBUTE": AssertionAttribute, "VALUE": AssertionValue, "URL": AssertionURL, "EVALUATE": AssertionEvaluate}
	kind, exists := kinds[assertion.Kind]
	if !exists {
		return Assertion{}, NewError(CodeInvalidArgument, "unsupported assertion")
	}
	match := MatchExact
	if assertion.Match == "CONTAINS" {
		match = MatchContains
	} else if assertion.Match != "" && assertion.Match != "EXACT" {
		return Assertion{}, NewError(CodeInvalidArgument, "unsupported assertion match mode")
	}
	result := Assertion{Kind: kind, Attribute: assertion.Attribute, Expression: assertion.Expression, ExpectedString: assertion.ExpectedString, ExpectedCount: assertion.ExpectedCount, ExpectedJSON: assertion.ExpectedJSON, Match: match}
	if kind != AssertionURL && kind != AssertionEvaluate {
		locator, err := locatorFromWire(assertion.Locator)
		if err != nil {
			return Assertion{}, err
		}
		result.Locator = locator
	}
	return result, nil
}

func resultToWire(result Result) OperationResult {
	trajectory := make([]PollObservation, len(result.Trajectory))
	for i, observation := range result.Trajectory {
		trajectory[i] = PollObservation{Attempt: observation.Attempt, ElapsedMS: observation.Elapsed.Milliseconds(), ObservedJSON: observation.ObservedJSON, RetryReason: observation.RetryReason}
	}
	wire := OperationResult{
		Matched: result.Matched, ObservedJSON: result.ObservedJSON, AttemptCount: result.Attempts, Trajectory: trajectory,
		Timings:         Timings{StartedUnixMS: result.StartedAt.UnixMilli(), ElapsedMS: result.Elapsed.Milliseconds()},
		RPCRequestCount: 1, RPCResponseCount: 1,
	}
	if result.Diagnostics != (Diagnostics{}) {
		diagnostics := result.Diagnostics
		wire.Diagnostics = &diagnostics
	}
	return wire
}

func normalizeError(err error) *ProtocolError {
	var protocolErr *ProtocolError
	if errors.As(err, &protocolErr) {
		return protocolErr
	}
	switch {
	case errors.Is(err, context.Canceled):
		return NewError(CodeCancelled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return NewError(CodeTimeout, err.Error())
	default:
		return NewError(CodeDriver, fmt.Sprintf("daemon operation failed: %v", err))
	}
}

package protocol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

const Version = "1"

var Capabilities = []string{
	"locator.css", "locator.xpath", "locator.test_id", "locator.text", "locator.role", "locator.label", "locator.placeholder", "locator.alt_text", "locator.title", "locator.refinements", "locator.first",
	"session.prepare", "session.new_tab", "session.add_init_script", "session.activate", "navigation", "cookies", "action.click", "action.set_value", "action.realistic", "action.type", "action.send_keys", "action.drag_to",
	"action.set_upload", "viewport.set", "evaluate", "evaluate.async", "assert.visible", "assert.text", "assert.count", "assert.attribute",
	"assert.value", "assert.url", "assert.evaluate", "poll.server_side", "diagnostics.structured",
	"dom.typed", "dom.collections", "dom.geometry", "dom.style", "dom.selection", "action.pointer_options", "action.scroll", "action.element_javascript", "keyboard.modifiers",
	"lifecycle.tabs", "lifecycle.context_identity", "lifecycle.cookies", "lifecycle.storage", "lifecycle.javascript", "lifecycle.page_state", "lifecycle.console", "lifecycle.emulation", "lifecycle.frames",
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

type TabSession interface {
	Session
	NewTab(context.Context) (Session, error)
}

// DiscoverableSession exposes live page and frame handles without leaking the underlying CDP
// representation across the runner-neutral protocol boundary.
type DiscoverableSession interface {
	Session
	Metadata() SessionMetadata
	Tabs(context.Context) ([]Session, error)
	WaitForTab(context.Context, TabQuery, PollPolicy) (Session, error)
	Frames(context.Context) ([]Session, error)
	WaitForFrame(context.Context, FrameQuery, PollPolicy) (Session, error)
}

type SessionMetadata struct {
	ContextID   string
	TargetID    string
	OpenerID    string
	OwnsContext bool
	Frame       bool
	URL         string
}

type TabQuery struct {
	SpawnedOnly bool
	Title       *Expectation
	URL         *Expectation
	HasElement  *Locator
}

type FrameQuery struct {
	Title      *Expectation
	URL        *Expectation
	HasElement *Locator
}

type OperationKind uint8

const (
	OperationNavigate OperationKind = iota + 1
	OperationSetCookies
	OperationClick
	OperationSetValue
	OperationEvaluate
	OperationAssert
	OperationType
	OperationSendKeys
	OperationSetWindowSize
	OperationSetUpload
	OperationHoldResponse
	OperationAwaitResponseHold
	OperationReleaseResponseHold
	OperationDragTo
	OperationAddInitScript
	OperationActivate
	OperationDOM
	OperationLifecycle
)

type LifecycleOperationKind uint8

const (
	LifecycleGetCookies LifecycleOperationKind = iota + 1
	LifecycleClearCookies
	LifecycleCookieQuery
	LifecycleStorageSet
	LifecycleStorageGet
	LifecycleStorageGetAll
	LifecycleStorageRemove
	LifecycleStorageClear
	LifecycleStorageLength
	LifecycleWaitForDefined
	LifecycleURL
	LifecycleTitle
	LifecycleWindowSize
	LifecycleOutline
	LifecycleAccessibilityOutline
	LifecycleConsoleMessages
	LifecycleSetDeviceMetrics
	LifecycleClearDeviceMetrics
	LifecycleSetGeolocation
	LifecycleClearGeolocation
	LifecycleSetPermissions
	LifecycleResetPermissions
	LifecycleSetLocale
	LifecycleClearLocale
	LifecycleSetTimezone
	LifecycleClearTimezone
	LifecycleSetMedia
	LifecycleClearMedia
)

type Operation struct {
	Kind OperationKind
	URL  string
	// ExpectedStatus is always concrete by the time an Operation is built - Dispatch resolves the
	// request's omitted 0 to 200 - so nothing downstream has to know the default.
	ExpectedStatus int
	Cookies        []Cookie
	Locator        Locator
	Target         Locator
	Poll           PollPolicy
	ValueJSON      string
	Expression     string
	ArgumentsJSON  string
	Invoke         *bool
	Assertion      Assertion
	Keys           string
	Realistic      bool
	AwaitPromise   bool
	Width          int
	Height         int
	Paths          []string
	Expectation    Expectation
	HoldID         string
	DOM            DOMOperation
	Lifecycle      LifecycleOperation
}

type LifecycleOperation struct {
	Kind              LifecycleOperationKind
	Area              string
	Key               string
	ValueJSON         string
	Expression        string
	Expectation       Expectation
	Cookie            CookieQuery
	Count             bool
	Width             int
	Height            int
	DeviceScaleFactor float64
	Mobile            bool
	Latitude          float64
	Longitude         float64
	Accuracy          float64
	Origin            string
	Permissions       map[string]string
	Locale            string
	Timezone          string
	MediaType         string
	ColorScheme       string
	ReducedMotion     string
}

type CookieQuery struct {
	Name     *Expectation
	Value    *Expectation
	Domain   *Expectation
	Path     *Expectation
	SameSite *Expectation
	Secure   *bool
	HTTPOnly *bool
}

type LocatorKind uint8

const (
	LocatorCSS LocatorKind = iota + 1
	LocatorXPath
	LocatorTestID
	LocatorText
	LocatorRole
	LocatorLabel
	LocatorPlaceholder
	LocatorAltText
	LocatorTitle
	LocatorAnd
	LocatorOr
)

type MatchMode uint8

const (
	MatchExact MatchMode = iota + 1
	MatchContains
)

type Locator struct {
	Kind      LocatorKind
	Value     string
	Role      string
	Name      string
	Attribute string
	Match     MatchMode
	Operands  []Locator
	Within    *Locator
	Filters   []LocatorFilter
	Level     int
	LevelSet  bool
	States    []string
	Nth       int
	NthSet    bool
}

type DOMOperationKind uint8

const (
	DOMText DOMOperationKind = iota + 1
	DOMTexts
	DOMClasses
	DOMClassesForEach
	DOMDistinctAttributeCount
	DOMAttributes
	DOMAttributesForEach
	DOMJSONAttribute
	DOMProperties
	DOMPropertiesForEach
	DOMPropertyForEach
	DOMValues
	DOMState
	DOMAllState
	DOMSetProperty
	DOMFocus
	DOMBlur
	DOMHover
	DOMType
	DOMSendKeys
	DOMClick
	DOMClickEach
	DOMTap
	DOMDrag
	DOMScrollIntoView
	DOMScrollWheel
	DOMSelect
	DOMClearSelection
	DOMInvokeMethod
	DOMInvokeFunction
	DOMInvokeMethodForEach
	DOMInvokeFunctionForEach
	DOMBoundingBox
	DOMScrollOffset
	DOMOffsetWithin
	DOMRelativeBoxes
	DOMGeometryRelation
	DOMGapBetween
	DOMInViewport
	DOMDocumentOrder
	DOMComputedStyle
	DOMComputedStyleNumber
	DOMNormalizeColor
)

type NameSpec struct {
	Name         string
	AllowMissing bool
}

type DOMOperation struct {
	Kind          DOMOperationKind
	Expectation   Expectation
	Locator       Locator
	Target        Locator
	Container     Locator
	TextMode      string
	Names         []NameSpec
	Name          string
	ValueJSON     string
	All           bool
	Every         bool
	ProjectName   string
	State         string
	Realistic     bool
	Button        string
	ClickCount    int
	OffsetX       float64
	OffsetY       float64
	HasOffset     bool
	Modifiers     []string
	Keys          string
	TopOffset     float64
	HasTopOffset  bool
	DeltaX        float64
	DeltaY        float64
	Substring     string
	Occurrence    int
	Start         int
	End           int
	Range         bool
	Method        string
	Expression    string
	ArgumentsJSON string
	Fully         bool
	Relation      string
}

type LocatorFilterKind uint8

const (
	LocatorFilterContainsText LocatorFilterKind = iota + 1
	LocatorFilterContains
	LocatorFilterWithin
)

type LocatorFilter struct {
	Kind     LocatorFilterKind
	Value    string
	Match    MatchMode
	Selector *Locator
	Negate   bool
}

type PollPolicy struct {
	Timeout  time.Duration
	Interval time.Duration
	Mode     PollMode
}

type PollMode uint8

const (
	PollEventually PollMode = iota + 1
	PollImmediate
	PollConsistently
)

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
	AssertionExists
	AssertionEnabled
	AssertionClickable
	AssertionProperty
	AssertionAllText
	AssertionRequest
)

type ExpectationKind uint8

const (
	ExpectEqual ExpectationKind = iota + 1
	ExpectContains
	ExpectRegexp
	ExpectPrefix
	ExpectSuffix
	ExpectNumber
	ExpectEmpty
	ExpectAll
	ExpectAny
	ExpectNot
	ExpectAnything
)

type Expectation struct {
	Kind         ExpectationKind
	ExpectedJSON string
	Operator     string
	Children     []Expectation
}

type Assertion struct {
	Kind           AssertionKind
	Locator        Locator
	Attribute      string
	Property       string
	Method         string
	Expression     string
	ExpectedString string
	ExpectedCount  int64
	ExpectedJSON   string
	Match          MatchMode
	Expectation    Expectation
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
	SessionID   string `json:"sessionId"`
	ContextID   string `json:"contextId,omitempty"`
	TargetID    string `json:"targetId,omitempty"`
	OpenerID    string `json:"openerId,omitempty"`
	OwnsContext bool   `json:"ownsContext,omitempty"`
	Frame       bool   `json:"frame,omitempty"`
	URL         string `json:"url,omitempty"`
}

type SessionRequest struct {
	SessionID string `json:"sessionId"`
}

type TabQueryRequest struct {
	SpawnedOnly bool             `json:"spawnedOnly,omitempty"`
	Title       *WireExpectation `json:"title,omitempty"`
	URL         *WireExpectation `json:"url,omitempty"`
	Has         *WireLocator     `json:"has,omitempty"`
}

type ListHandlesRequest struct {
	SessionID   string `json:"sessionId"`
	SpawnedOnly bool   `json:"spawnedOnly,omitempty"`
}

type WaitForTabRequest struct {
	SessionID string          `json:"sessionId"`
	Query     TabQueryRequest `json:"query"`
	Poll      PollOptions     `json:"poll,omitempty"`
}

type WaitForFrameRequest struct {
	SessionID string          `json:"sessionId"`
	Query     TabQueryRequest `json:"query"`
	Poll      PollOptions     `json:"poll,omitempty"`
}

type HandleListResponse struct {
	Handles []OpenSessionResponse `json:"handles"`
}
type InvalidationResponse struct {
	InvalidatedSessionIDs []string `json:"invalidatedSessionIds,omitempty"`
}

type NavigateRequest struct {
	SessionID string `json:"sessionId"`
	URL       string `json:"url"`
	// ExpectedStatus is the HTTP status the main document must answer with.  Omitted (0) means 200,
	// so a client that does not care keeps sending exactly what it sent before.
	ExpectedStatus int `json:"expectedStatus,omitempty"`
}

type PollOptions struct {
	TimeoutMS  int64  `json:"timeoutMs,omitempty"`
	IntervalMS int64  `json:"intervalMs,omitempty"`
	Mode       string `json:"mode,omitempty"`
}

type WireLocator struct {
	Kind      string              `json:"kind"`
	Value     string              `json:"value,omitempty"`
	Role      string              `json:"role,omitempty"`
	Name      string              `json:"name,omitempty"`
	Attribute string              `json:"attribute,omitempty"`
	Match     string              `json:"match,omitempty"`
	Operands  []*WireLocator      `json:"operands,omitempty"`
	Within    *WireLocator        `json:"within,omitempty"`
	Filters   []WireLocatorFilter `json:"filters,omitempty"`
	Level     int                 `json:"level,omitempty"`
	LevelSet  bool                `json:"levelSet,omitempty"`
	States    []string            `json:"states,omitempty"`
	Nth       int                 `json:"nth,omitempty"`
	NthSet    bool                `json:"nthSet,omitempty"`
	First     bool                `json:"first"`
}

type WireNameSpec struct {
	Name         string `json:"name"`
	AllowMissing bool   `json:"allowMissing,omitempty"`
}

type WirePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type WireDOMOperation struct {
	Kind          string         `json:"kind"`
	Locator       *WireLocator   `json:"locator,omitempty"`
	Target        *WireLocator   `json:"target,omitempty"`
	Container     *WireLocator   `json:"container,omitempty"`
	TextMode      string         `json:"textMode,omitempty"`
	Names         []WireNameSpec `json:"names,omitempty"`
	Name          string         `json:"name,omitempty"`
	ValueJSON     string         `json:"valueJson,omitempty"`
	All           bool           `json:"all,omitempty"`
	Every         bool           `json:"every,omitempty"`
	ProjectName   string         `json:"projectName,omitempty"`
	State         string         `json:"state,omitempty"`
	Realistic     bool           `json:"realistic,omitempty"`
	Button        string         `json:"button,omitempty"`
	ClickCount    int            `json:"clickCount,omitempty"`
	Offset        *WirePoint     `json:"offset,omitempty"`
	Modifiers     []string       `json:"modifiers,omitempty"`
	Keys          string         `json:"keys,omitempty"`
	TopOffset     float64        `json:"topOffset,omitempty"`
	HasTopOffset  bool           `json:"hasTopOffset,omitempty"`
	DeltaX        float64        `json:"deltaX,omitempty"`
	DeltaY        float64        `json:"deltaY,omitempty"`
	Substring     string         `json:"substring,omitempty"`
	Occurrence    int            `json:"occurrence,omitempty"`
	Start         int            `json:"start,omitempty"`
	End           int            `json:"end,omitempty"`
	Range         bool           `json:"range,omitempty"`
	Method        string         `json:"method,omitempty"`
	Expression    string         `json:"expression,omitempty"`
	ArgumentsJSON string         `json:"argumentsJson,omitempty"`
	Fully         bool           `json:"fully,omitempty"`
	Relation      string         `json:"relation,omitempty"`
}

type DOMRequest struct {
	SessionID   string            `json:"sessionId"`
	Operation   *WireDOMOperation `json:"operation"`
	Expectation *WireExpectation  `json:"expectation,omitempty"`
	Poll        PollOptions       `json:"poll,omitempty"`
}

type WireLocatorFilter struct {
	Kind     string       `json:"kind"`
	Value    string       `json:"value,omitempty"`
	Match    string       `json:"match,omitempty"`
	Selector *WireLocator `json:"selector,omitempty"`
	Negate   bool         `json:"negate,omitempty"`
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

type WireCookieQuery struct {
	Name     *WireExpectation `json:"name,omitempty"`
	Value    *WireExpectation `json:"value,omitempty"`
	Domain   *WireExpectation `json:"domain,omitempty"`
	Path     *WireExpectation `json:"path,omitempty"`
	SameSite *WireExpectation `json:"sameSite,omitempty"`
	Secure   *bool            `json:"secure,omitempty"`
	HTTPOnly *bool            `json:"httpOnly,omitempty"`
}

type WireDeviceMetrics struct {
	Width             int     `json:"width"`
	Height            int     `json:"height"`
	DeviceScaleFactor float64 `json:"deviceScaleFactor"`
	Mobile            bool    `json:"mobile,omitempty"`
}

type WireGeolocation struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Accuracy  float64 `json:"accuracy,omitempty"`
}

type WireMedia struct {
	Type          string `json:"type,omitempty"`
	ColorScheme   string `json:"colorScheme,omitempty"`
	ReducedMotion string `json:"reducedMotion,omitempty"`
}

type WireLifecycleOperation struct {
	Kind        string             `json:"kind"`
	Area        string             `json:"area,omitempty"`
	Key         string             `json:"key,omitempty"`
	ValueJSON   string             `json:"valueJson,omitempty"`
	Expression  string             `json:"expression,omitempty"`
	Cookie      *WireCookieQuery   `json:"cookie,omitempty"`
	Count       bool               `json:"count,omitempty"`
	Device      *WireDeviceMetrics `json:"device,omitempty"`
	Geolocation *WireGeolocation   `json:"geolocation,omitempty"`
	Origin      string             `json:"origin,omitempty"`
	Permissions map[string]string  `json:"permissions,omitempty"`
	Locale      string             `json:"locale,omitempty"`
	Timezone    string             `json:"timezone,omitempty"`
	Media       *WireMedia         `json:"media,omitempty"`
}

type LifecycleRequest struct {
	SessionID   string                  `json:"sessionId"`
	Operation   *WireLifecycleOperation `json:"operation"`
	Expectation *WireExpectation        `json:"expectation,omitempty"`
	Poll        PollOptions             `json:"poll,omitempty"`
}

type CookieListResponse struct {
	Cookies []WireCookie `json:"cookies"`
}

type LocatorRequest struct {
	SessionID string       `json:"sessionId"`
	Locator   *WireLocator `json:"locator"`
	Poll      PollOptions  `json:"poll,omitempty"`
	Realistic bool         `json:"realistic,omitempty"`
}

type SetValueRequest struct {
	SessionID string       `json:"sessionId"`
	Locator   *WireLocator `json:"locator"`
	ValueJSON string       `json:"valueJson"`
	Poll      PollOptions  `json:"poll,omitempty"`
	Realistic bool         `json:"realistic,omitempty"`
}

type TypeRequest struct {
	SessionID string       `json:"sessionId"`
	Locator   *WireLocator `json:"locator"`
	Keys      string       `json:"keys"`
	Poll      PollOptions  `json:"poll,omitempty"`
	Realistic bool         `json:"realistic,omitempty"`
}

type SendKeysRequest struct {
	SessionID string `json:"sessionId"`
	Keys      string `json:"keys"`
}

type SetWindowSizeRequest struct {
	SessionID string `json:"sessionId"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type SetUploadRequest struct {
	SessionID string       `json:"sessionId"`
	Locator   *WireLocator `json:"locator"`
	Paths     []string     `json:"paths"`
	Poll      PollOptions  `json:"poll,omitempty"`
}

type DragToRequest struct {
	SessionID string       `json:"sessionId"`
	Source    *WireLocator `json:"source"`
	Target    *WireLocator `json:"target"`
	Poll      PollOptions  `json:"poll,omitempty"`
	Realistic bool         `json:"realistic,omitempty"`
}

type AddInitScriptRequest struct {
	SessionID string `json:"sessionId"`
	Script    string `json:"script"`
}

type HoldResponseRequest struct {
	SessionID   string           `json:"sessionId"`
	Expectation *WireExpectation `json:"expectation"`
}

type ResponseHoldRequest struct {
	SessionID string `json:"sessionId"`
	HoldID    string `json:"holdId"`
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
	Invoke       *bool `json:"invoke,omitempty"`
	AwaitPromise bool  `json:"awaitPromise,omitempty"`
}

type WireAssertion struct {
	Kind           string           `json:"kind"`
	Locator        *WireLocator     `json:"locator,omitempty"`
	Attribute      string           `json:"attribute,omitempty"`
	Property       string           `json:"property,omitempty"`
	Method         string           `json:"method,omitempty"`
	Expression     string           `json:"expression,omitempty"`
	ExpectedString string           `json:"expectedString,omitempty"`
	ExpectedCount  int64            `json:"expectedCount,omitempty"`
	ExpectedJSON   string           `json:"expectedJson,omitempty"`
	Match          string           `json:"match,omitempty"`
	Expectation    *WireExpectation `json:"expectation,omitempty"`
}

type WireExpectation struct {
	Kind         string             `json:"kind"`
	ExpectedJSON string             `json:"expectedJson,omitempty"`
	Operator     string             `json:"operator,omitempty"`
	Children     []*WireExpectation `json:"children,omitempty"`
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
		opened, openErr := s.registerSession(session)
		if openErr != nil {
			_ = session.Close()
			return nil, openErr
		}
		return opened, nil
	case "newTab":
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
		parent, ok := entry.session.(TabSession)
		if !ok {
			return nil, NewError(CodeDriver, "session backend does not support new tabs")
		}
		sibling, openErr := parent.NewTab(ctx)
		if openErr != nil {
			return nil, normalizeError(openErr)
		}
		opened, registerErr := s.registerSession(sibling)
		if registerErr != nil {
			_ = sibling.Close()
			return nil, registerErr
		}
		return opened, nil
	case "listTabs", "listFrames":
		var request ListHandlesRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		entry, err := s.session(request.SessionID)
		if err != nil {
			return nil, err
		}
		discoverable, ok := entry.session.(DiscoverableSession)
		if !ok {
			return nil, NewError(CodeDriver, "session backend does not support handle discovery")
		}
		var handles []Session
		var discoverErr error
		if method == "listFrames" {
			handles, discoverErr = discoverable.Frames(ctx)
		} else {
			handles, discoverErr = discoverable.Tabs(ctx)
		}
		if discoverErr != nil {
			return nil, normalizeError(discoverErr)
		}
		response := HandleListResponse{Handles: []OpenSessionResponse{}}
		for _, handle := range handles {
			meta, hasMeta := handle.(DiscoverableSession)
			if method == "listTabs" && request.SpawnedOnly && (!hasMeta || meta.Metadata().OpenerID == "") {
				continue
			}
			opened, registerErr := s.registerSession(handle)
			if registerErr != nil {
				return nil, registerErr
			}
			response.Handles = append(response.Handles, opened)
		}
		return response, nil
	case "waitForTab", "waitForFrame":
		var request WaitForTabRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		entry, err := s.session(request.SessionID)
		if err != nil {
			return nil, err
		}
		discoverable, ok := entry.session.(DiscoverableSession)
		if !ok {
			return nil, NewError(CodeDriver, "session backend does not support handle discovery")
		}
		query, queryErr := tabQueryFromWire(request.Query)
		if queryErr != nil {
			return nil, queryErr
		}
		policy, policyErr := pollFromWire(request.Poll)
		if policyErr != nil {
			return nil, policyErr
		}
		var handle Session
		var waitErr error
		if method == "waitForFrame" {
			handle, waitErr = discoverable.WaitForFrame(ctx, FrameQuery{Title: query.Title, URL: query.URL, HasElement: query.HasElement}, policy)
		} else {
			handle, waitErr = discoverable.WaitForTab(ctx, query, policy)
		}
		if waitErr != nil {
			return nil, normalizeError(waitErr)
		}
		return s.registerSession(handle)
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
		return InvalidationResponse{InvalidatedSessionIDs: s.invalidateContext(entry.session, request.SessionID, true)}, nil
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
	case "getCookies":
		var request SessionRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		result, executeErr := s.execute(ctx, request.SessionID, Operation{Kind: OperationLifecycle, Lifecycle: LifecycleOperation{Kind: LifecycleGetCookies}})
		if executeErr != nil {
			return nil, executeErr
		}
		operationResult := result.(OperationResult)
		var cookies []WireCookie
		if operationResult.ObservedJSON != "" {
			if err := json.Unmarshal([]byte(operationResult.ObservedJSON), &cookies); err != nil {
				return nil, NewError(CodeDriver, "decode cookie snapshot: "+err.Error())
			}
		}
		if cookies == nil {
			cookies = []WireCookie{}
		}
		return CookieListResponse{Cookies: cookies}, nil
	case "clearCookies":
		var request SessionRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationLifecycle, Lifecycle: LifecycleOperation{Kind: LifecycleClearCookies}})
	case "lifecycle":
		var request LifecycleRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		operation, operationErr := lifecycleOperationFromWire(request.Operation, request.Expectation)
		if operationErr != nil {
			return nil, operationErr
		}
		policy, policyErr := pollFromWire(request.Poll)
		if policyErr != nil {
			return nil, policyErr
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationLifecycle, Lifecycle: operation, Poll: policy})
	case "click":
		var request LocatorRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		locator, err := locatorFromWire(request.Locator)
		if err != nil {
			return nil, err
		}
		poll, pollErr := pollFromWire(request.Poll)
		if pollErr != nil {
			return nil, pollErr
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationClick, Locator: locator, Poll: poll, Realistic: request.Realistic})
	case "setValue":
		var request SetValueRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		locator, err := locatorFromWire(request.Locator)
		if err != nil {
			return nil, err
		}
		poll, pollErr := pollFromWire(request.Poll)
		if pollErr != nil {
			return nil, pollErr
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationSetValue, Locator: locator, Poll: poll, ValueJSON: request.ValueJSON, Realistic: request.Realistic})
	case "type":
		var request TypeRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		locator, err := locatorFromWire(request.Locator)
		if err != nil {
			return nil, err
		}
		if request.Keys == "" {
			return nil, NewError(CodeInvalidArgument, "keys are required")
		}
		poll, pollErr := pollFromWire(request.Poll)
		if pollErr != nil {
			return nil, pollErr
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationType, Locator: locator, Keys: request.Keys, Poll: poll, Realistic: request.Realistic})
	case "sendKeys":
		var request SendKeysRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if request.Keys == "" {
			return nil, NewError(CodeInvalidArgument, "keys are required")
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationSendKeys, Keys: request.Keys})
	case "setWindowSize":
		var request SetWindowSizeRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if request.Width <= 0 || request.Height <= 0 {
			return nil, NewError(CodeInvalidArgument, "width and height must be positive")
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationSetWindowSize, Width: request.Width, Height: request.Height})
	case "setUpload":
		var request SetUploadRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		locator, err := locatorFromWire(request.Locator)
		if err != nil {
			return nil, err
		}
		if len(request.Paths) == 0 {
			return nil, NewError(CodeInvalidArgument, "paths must contain at least one file")
		}
		for i, path := range request.Paths {
			if path == "" {
				return nil, NewError(CodeInvalidArgument, fmt.Sprintf("paths[%d] is required", i))
			}
		}
		poll, pollErr := pollFromWire(request.Poll)
		if pollErr != nil {
			return nil, pollErr
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationSetUpload, Locator: locator, Paths: request.Paths, Poll: poll})
	case "dragTo":
		var request DragToRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		source, err := locatorFromWire(request.Source)
		if err != nil {
			return nil, err
		}
		targetLocator, err := locatorFromWire(request.Target)
		if err != nil {
			return nil, err
		}
		poll, pollErr := pollFromWire(request.Poll)
		if pollErr != nil {
			return nil, pollErr
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationDragTo, Locator: source, Target: targetLocator, Poll: poll, Realistic: request.Realistic})
	case "addInitScript":
		var request AddInitScriptRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if request.Script == "" {
			return nil, NewError(CodeInvalidArgument, "script is required")
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationAddInitScript, Expression: request.Script})
	case "activate":
		var request SessionRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationActivate})
	case "holdResponse":
		var request HoldResponseRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		expectation, err := expectationFromWire(request.Expectation, 0)
		if err != nil {
			return nil, err
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationHoldResponse, Expectation: expectation})
	case "awaitResponseHold", "releaseResponseHold":
		var request ResponseHoldRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if request.HoldID == "" {
			return nil, NewError(CodeInvalidArgument, "holdId is required")
		}
		kind := OperationAwaitResponseHold
		if method == "releaseResponseHold" {
			kind = OperationReleaseResponseHold
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: kind, HoldID: request.HoldID})
	case "evaluate":
		var request EvaluateRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		if request.Expression == "" {
			return nil, NewError(CodeInvalidArgument, "expression is required")
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationEvaluate, Expression: request.Expression, ArgumentsJSON: request.ArgumentsJSON, Invoke: request.Invoke, AwaitPromise: request.AwaitPromise})
	case "assert":
		var request AssertRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		assertion, err := assertionFromWire(request.Assertion)
		if err != nil {
			return nil, err
		}
		poll, pollErr := pollFromWire(request.Poll)
		if pollErr != nil {
			return nil, pollErr
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationAssert, Assertion: assertion, Poll: poll})
	case "dom":
		var request DOMRequest
		if err := decodeParams(params, &request); err != nil {
			return nil, err
		}
		domOperation, err := domOperationFromWire(request.Operation)
		if err != nil {
			return nil, err
		}
		if request.Expectation != nil {
			expectation, expectationErr := expectationFromWire(request.Expectation, 0)
			if expectationErr != nil {
				return nil, expectationErr
			}
			domOperation.Expectation = expectation
		}
		poll, pollErr := pollFromWire(request.Poll)
		if pollErr != nil {
			return nil, pollErr
		}
		return s.execute(ctx, request.SessionID, Operation{Kind: OperationDOM, DOM: domOperation, Poll: poll})
	default:
		return nil, NewError(CodeInvalidArgument, fmt.Sprintf("unsupported method %q", method))
	}
}

func domOperationFromWire(operation *WireDOMOperation) (DOMOperation, *ProtocolError) {
	if operation == nil || operation.Kind == "" {
		return DOMOperation{}, NewError(CodeInvalidArgument, "DOM operation kind is required")
	}
	kinds := map[string]DOMOperationKind{
		"TEXT": DOMText, "TEXTS": DOMTexts, "CLASSES": DOMClasses, "CLASSES_FOR_EACH": DOMClassesForEach,
		"DISTINCT_ATTRIBUTE_COUNT": DOMDistinctAttributeCount, "ATTRIBUTES": DOMAttributes, "ATTRIBUTES_FOR_EACH": DOMAttributesForEach,
		"JSON_ATTRIBUTE": DOMJSONAttribute, "PROPERTIES": DOMProperties, "PROPERTIES_FOR_EACH": DOMPropertiesForEach,
		"PROPERTY_FOR_EACH": DOMPropertyForEach, "VALUES": DOMValues, "STATE": DOMState, "ALL_STATE": DOMAllState,
		"SET_PROPERTY": DOMSetProperty, "FOCUS": DOMFocus, "BLUR": DOMBlur, "HOVER": DOMHover, "TYPE": DOMType,
		"SEND_KEYS": DOMSendKeys, "CLICK": DOMClick, "CLICK_EACH": DOMClickEach, "TAP": DOMTap, "DRAG": DOMDrag,
		"SCROLL_INTO_VIEW": DOMScrollIntoView, "SCROLL_WHEEL": DOMScrollWheel, "SELECT": DOMSelect,
		"CLEAR_SELECTION": DOMClearSelection, "INVOKE_METHOD": DOMInvokeMethod, "INVOKE_FUNCTION": DOMInvokeFunction,
		"INVOKE_METHOD_FOR_EACH": DOMInvokeMethodForEach, "INVOKE_FUNCTION_FOR_EACH": DOMInvokeFunctionForEach,
		"BOUNDING_BOX": DOMBoundingBox, "SCROLL_OFFSET": DOMScrollOffset, "OFFSET_WITHIN": DOMOffsetWithin,
		"RELATIVE_BOXES": DOMRelativeBoxes, "GEOMETRY_RELATION": DOMGeometryRelation, "GAP_BETWEEN": DOMGapBetween,
		"IN_VIEWPORT": DOMInViewport, "DOCUMENT_ORDER": DOMDocumentOrder, "COMPUTED_STYLE": DOMComputedStyle,
		"COMPUTED_STYLE_NUMBER": DOMComputedStyleNumber, "NORMALIZE_COLOR": DOMNormalizeColor,
	}
	kind, ok := kinds[operation.Kind]
	if !ok {
		return DOMOperation{}, NewError(CodeInvalidArgument, fmt.Sprintf("unsupported DOM operation %q", operation.Kind))
	}
	result := DOMOperation{Kind: kind, TextMode: operation.TextMode, Name: operation.Name, ValueJSON: operation.ValueJSON, All: operation.All, Every: operation.Every, ProjectName: operation.ProjectName, State: operation.State, Realistic: operation.Realistic, Button: operation.Button, ClickCount: operation.ClickCount, Modifiers: append([]string(nil), operation.Modifiers...), Keys: operation.Keys, TopOffset: operation.TopOffset, HasTopOffset: operation.HasTopOffset, DeltaX: operation.DeltaX, DeltaY: operation.DeltaY, Substring: operation.Substring, Occurrence: operation.Occurrence, Start: operation.Start, End: operation.End, Range: operation.Range, Method: operation.Method, Expression: operation.Expression, ArgumentsJSON: operation.ArgumentsJSON, Fully: operation.Fully, Relation: operation.Relation}
	if operation.Offset != nil {
		result.HasOffset, result.OffsetX, result.OffsetY = true, operation.Offset.X, operation.Offset.Y
	}
	for _, name := range operation.Names {
		if name.Name == "" {
			return DOMOperation{}, NewError(CodeInvalidArgument, "DOM operation names must not be empty")
		}
		result.Names = append(result.Names, NameSpec{Name: name.Name, AllowMissing: name.AllowMissing})
	}
	withoutLocator := kind == DOMSendKeys || kind == DOMClearSelection || kind == DOMNormalizeColor
	if !withoutLocator {
		locator, err := locatorFromWire(operation.Locator)
		if err != nil {
			return DOMOperation{}, err
		}
		result.Locator = locator
	}
	needsTarget := kind == DOMDrag || kind == DOMOffsetWithin || kind == DOMRelativeBoxes || kind == DOMGeometryRelation || kind == DOMGapBetween || kind == DOMDocumentOrder
	if needsTarget {
		target, err := locatorFromWire(operation.Target)
		if err != nil {
			return DOMOperation{}, err
		}
		result.Target = target
	}
	if operation.Container != nil {
		container, err := locatorFromWire(operation.Container)
		if err != nil {
			return DOMOperation{}, err
		}
		result.Container = container
	}
	if kind == DOMText || kind == DOMTexts {
		if operation.TextMode != "INNER_TEXT" && operation.TextMode != "TEXT_CONTENT" && operation.TextMode != "NORMALIZED_TEXT" {
			return DOMOperation{}, NewError(CodeInvalidArgument, "unsupported DOM text mode")
		}
	}
	if (kind == DOMAttributes || kind == DOMProperties || kind == DOMAttributesForEach || kind == DOMPropertiesForEach) && len(result.Names) == 0 {
		return DOMOperation{}, NewError(CodeInvalidArgument, "DOM operation requires names")
	}
	if operation.Every && kind != DOMTexts && kind != DOMClassesForEach && kind != DOMPropertyForEach {
		if kind != DOMAttributesForEach || operation.ProjectName == "" {
			return DOMOperation{}, NewError(CodeInvalidArgument, "every is only valid for DOM collection assertions")
		}
	}
	if operation.ProjectName != "" && (kind != DOMAttributesForEach || !operation.Every) {
		return DOMOperation{}, NewError(CodeInvalidArgument, "projectName is only valid for all-element attribute assertions")
	}
	if (kind == DOMJSONAttribute || kind == DOMPropertyForEach || kind == DOMDistinctAttributeCount || kind == DOMSetProperty || kind == DOMComputedStyle || kind == DOMComputedStyleNumber) && operation.Name == "" {
		return DOMOperation{}, NewError(CodeInvalidArgument, "DOM operation requires name")
	}
	if (kind == DOMType || kind == DOMSendKeys) && operation.Keys == "" {
		return DOMOperation{}, NewError(CodeInvalidArgument, "DOM operation requires keys")
	}
	if (kind == DOMInvokeMethod || kind == DOMInvokeMethodForEach) && operation.Method == "" {
		return DOMOperation{}, NewError(CodeInvalidArgument, "DOM operation requires method")
	}
	if (kind == DOMInvokeFunction || kind == DOMInvokeFunctionForEach) && operation.Expression == "" {
		return DOMOperation{}, NewError(CodeInvalidArgument, "DOM operation requires expression")
	}
	validModifiers := map[string]bool{"Shift": true, "Control": true, "Alt": true, "Meta": true}
	for _, modifier := range operation.Modifiers {
		if !validModifiers[modifier] {
			return DOMOperation{}, NewError(CodeInvalidArgument, fmt.Sprintf("unsupported DOM modifier %q", modifier))
		}
	}
	if kind == DOMClick {
		if operation.ClickCount != 1 && operation.ClickCount != 2 {
			return DOMOperation{}, NewError(CodeInvalidArgument, "DOM clickCount must be one or two")
		}
		if operation.Button != "left" && operation.Button != "right" && operation.Button != "middle" {
			return DOMOperation{}, NewError(CodeInvalidArgument, "unsupported DOM mouse button")
		}
	}
	if kind == DOMState || kind == DOMAllState {
		valid := map[string]bool{"visible": true, "enabled": true, "clickable": true, "checked": true, "focused": true}
		if !valid[operation.State] {
			return DOMOperation{}, NewError(CodeInvalidArgument, "unsupported DOM element state")
		}
		if kind == DOMAllState && operation.State != "visible" && operation.State != "enabled" {
			return DOMOperation{}, NewError(CodeInvalidArgument, "all-state only supports visible or enabled")
		}
	}
	if kind == DOMGeometryRelation {
		valid := map[string]bool{"above": true, "below": true, "leftOf": true, "rightOf": true, "encloses": true, "overlaps": true}
		if !valid[operation.Relation] {
			return DOMOperation{}, NewError(CodeInvalidArgument, "unsupported DOM geometry relation")
		}
	}
	if kind == DOMSetProperty && operation.ValueJSON == "" {
		return DOMOperation{}, NewError(CodeInvalidArgument, "set property requires valueJson")
	}
	if operation.ValueJSON != "" && kind != DOMNormalizeColor {
		var decoded any
		if err := json.Unmarshal([]byte(operation.ValueJSON), &decoded); err != nil {
			return DOMOperation{}, NewError(CodeInvalidArgument, fmt.Sprintf("DOM valueJson: %v", err))
		}
	}
	if operation.ArgumentsJSON != "" {
		var decoded []any
		if err := json.Unmarshal([]byte(operation.ArgumentsJSON), &decoded); err != nil {
			return DOMOperation{}, NewError(CodeInvalidArgument, fmt.Sprintf("DOM argumentsJson must be an array: %v", err))
		}
	}
	if kind == DOMNormalizeColor && operation.ValueJSON == "" {
		return DOMOperation{}, NewError(CodeInvalidArgument, "normalize color requires a color")
	}
	if kind == DOMSelect {
		if operation.Range && (operation.Start < 0 || operation.End < operation.Start) {
			return DOMOperation{}, NewError(CodeInvalidArgument, "invalid DOM selection range")
		}
		if operation.Substring != "" && operation.Occurrence < 1 {
			return DOMOperation{}, NewError(CodeInvalidArgument, "DOM selection occurrence must be positive")
		}
	}
	return result, nil
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
	invalidated := s.invalidateContext(entry.session, id, false)
	invalidated = append([]string{id}, invalidated...)
	return InvalidationResponse{InvalidatedSessionIDs: invalidated}, nil
}

func (s *Server) registerSession(session Session) (OpenSessionResponse, *ProtocolError) {
	metadata := SessionMetadata{}
	if discoverable, ok := session.(DiscoverableSession); ok {
		metadata = discoverable.Metadata()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if metadata.TargetID != "" {
		for id, entry := range s.sessions {
			if existing, ok := entry.session.(DiscoverableSession); ok && existing.Metadata().TargetID == metadata.TargetID {
				return openSessionResponse(id, metadata), nil
			}
		}
	}
	id, err := randomID()
	if err != nil {
		return OpenSessionResponse{}, NewError(CodeDriver, "generate session id")
	}
	s.sessions[id] = &sessionEntry{session: session}
	return openSessionResponse(id, metadata), nil
}

func openSessionResponse(id string, metadata SessionMetadata) OpenSessionResponse {
	return OpenSessionResponse{SessionID: id, ContextID: metadata.ContextID, TargetID: metadata.TargetID, OpenerID: metadata.OpenerID, OwnsContext: metadata.OwnsContext, Frame: metadata.Frame, URL: metadata.URL}
}

func (s *Server) invalidateContext(session Session, keepID string, keepSelf bool) []string {
	discoverable, ok := session.(DiscoverableSession)
	if !ok || discoverable.Metadata().ContextID == "" {
		return nil
	}
	metadata := discoverable.Metadata()
	s.mu.Lock()
	defer s.mu.Unlock()
	invalidated := []string{}
	for id, entry := range s.sessions {
		candidate, ok := entry.session.(DiscoverableSession)
		if !ok || candidate.Metadata().ContextID != metadata.ContextID || (keepSelf && id == keepID) {
			continue
		}
		delete(s.sessions, id)
		invalidated = append(invalidated, id)
	}
	return invalidated
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

func pollFromWire(poll PollOptions) (PollPolicy, *ProtocolError) {
	modes := map[string]PollMode{"": PollEventually, "EVENTUALLY": PollEventually, "IMMEDIATE": PollImmediate, "CONSISTENTLY": PollConsistently}
	mode, ok := modes[poll.Mode]
	if !ok {
		return PollPolicy{}, NewError(CodeInvalidArgument, "unsupported poll mode")
	}
	return PollPolicy{Timeout: time.Duration(poll.TimeoutMS) * time.Millisecond, Interval: time.Duration(poll.IntervalMS) * time.Millisecond, Mode: mode}, nil
}

func tabQueryFromWire(query TabQueryRequest) (TabQuery, *ProtocolError) {
	result := TabQuery{SpawnedOnly: query.SpawnedOnly}
	var err *ProtocolError
	if query.Title != nil {
		value, decodeErr := expectationFromWire(query.Title, 0)
		if decodeErr != nil {
			return TabQuery{}, decodeErr
		}
		result.Title = &value
	}
	if query.URL != nil {
		value, decodeErr := expectationFromWire(query.URL, 0)
		if decodeErr != nil {
			return TabQuery{}, decodeErr
		}
		result.URL = &value
	}
	if query.Has != nil {
		value, decodeErr := locatorFromWire(query.Has)
		if decodeErr != nil {
			return TabQuery{}, decodeErr
		}
		result.HasElement = &value
	}
	return result, err
}

func lifecycleOperationFromWire(operation *WireLifecycleOperation, expected *WireExpectation) (LifecycleOperation, *ProtocolError) {
	if operation == nil {
		return LifecycleOperation{}, NewError(CodeInvalidArgument, "lifecycle operation is required")
	}
	kinds := map[string]LifecycleOperationKind{
		"GET_COOKIES": LifecycleGetCookies, "CLEAR_COOKIES": LifecycleClearCookies, "COOKIE_QUERY": LifecycleCookieQuery,
		"STORAGE_SET": LifecycleStorageSet, "STORAGE_GET": LifecycleStorageGet, "STORAGE_GET_ALL": LifecycleStorageGetAll, "STORAGE_REMOVE": LifecycleStorageRemove, "STORAGE_CLEAR": LifecycleStorageClear, "STORAGE_LENGTH": LifecycleStorageLength,
		"WAIT_FOR_DEFINED": LifecycleWaitForDefined, "URL": LifecycleURL, "TITLE": LifecycleTitle, "WINDOW_SIZE": LifecycleWindowSize, "OUTLINE": LifecycleOutline, "ACCESSIBILITY_OUTLINE": LifecycleAccessibilityOutline, "CONSOLE_MESSAGES": LifecycleConsoleMessages,
		"SET_DEVICE_METRICS": LifecycleSetDeviceMetrics, "CLEAR_DEVICE_METRICS": LifecycleClearDeviceMetrics, "SET_GEOLOCATION": LifecycleSetGeolocation, "CLEAR_GEOLOCATION": LifecycleClearGeolocation,
		"SET_PERMISSIONS": LifecycleSetPermissions, "RESET_PERMISSIONS": LifecycleResetPermissions, "SET_LOCALE": LifecycleSetLocale, "CLEAR_LOCALE": LifecycleClearLocale, "SET_TIMEZONE": LifecycleSetTimezone, "CLEAR_TIMEZONE": LifecycleClearTimezone, "SET_MEDIA": LifecycleSetMedia, "CLEAR_MEDIA": LifecycleClearMedia,
	}
	kind, ok := kinds[operation.Kind]
	if !ok {
		return LifecycleOperation{}, NewError(CodeInvalidArgument, "unsupported lifecycle operation")
	}
	result := LifecycleOperation{Kind: kind, Area: operation.Area, Key: operation.Key, ValueJSON: operation.ValueJSON, Expression: operation.Expression, Count: operation.Count, Origin: operation.Origin, Permissions: operation.Permissions, Locale: operation.Locale, Timezone: operation.Timezone}
	if operation.Device != nil {
		result.Width = operation.Device.Width
		result.Height = operation.Device.Height
		result.DeviceScaleFactor = operation.Device.DeviceScaleFactor
		result.Mobile = operation.Device.Mobile
	}
	if operation.Geolocation != nil {
		result.Latitude = operation.Geolocation.Latitude
		result.Longitude = operation.Geolocation.Longitude
		result.Accuracy = operation.Geolocation.Accuracy
	}
	if operation.Media != nil {
		result.MediaType = operation.Media.Type
		result.ColorScheme = operation.Media.ColorScheme
		result.ReducedMotion = operation.Media.ReducedMotion
	}
	if expected != nil {
		expectation, err := expectationFromWire(expected, 0)
		if err != nil {
			return LifecycleOperation{}, err
		}
		result.Expectation = expectation
	}
	if operation.Cookie != nil {
		result.Cookie.Secure, result.Cookie.HTTPOnly = operation.Cookie.Secure, operation.Cookie.HTTPOnly
		for source, destination := range map[*WireExpectation]**Expectation{operation.Cookie.Name: &result.Cookie.Name, operation.Cookie.Value: &result.Cookie.Value, operation.Cookie.Domain: &result.Cookie.Domain, operation.Cookie.Path: &result.Cookie.Path, operation.Cookie.SameSite: &result.Cookie.SameSite} {
			if source == nil {
				continue
			}
			value, err := expectationFromWire(source, 0)
			if err != nil {
				return LifecycleOperation{}, err
			}
			*destination = &value
		}
	}
	if strings.HasPrefix(operation.Kind, "STORAGE_") && operation.Area != "localStorage" && operation.Area != "sessionStorage" {
		return LifecycleOperation{}, NewError(CodeInvalidArgument, "storage area must be localStorage or sessionStorage")
	}
	if operation.Kind == "STORAGE_SET" && operation.ValueJSON == "" {
		return LifecycleOperation{}, NewError(CodeInvalidArgument, "storage set requires valueJson")
	}
	if operation.ValueJSON != "" {
		var value any
		if err := json.Unmarshal([]byte(operation.ValueJSON), &value); err != nil {
			return LifecycleOperation{}, NewError(CodeInvalidArgument, "invalid lifecycle valueJson: "+err.Error())
		}
	}
	return result, nil
}

func locatorFromWire(locator *WireLocator) (Locator, *ProtocolError) {
	return locatorFromWireAt(locator, 0)
}

func locatorFromWireAt(locator *WireLocator, depth int) (Locator, *ProtocolError) {
	if locator == nil {
		return Locator{}, NewError(CodeInvalidArgument, "locator is required")
	}
	if depth > 64 {
		return Locator{}, NewError(CodeInvalidArgument, "locator nesting exceeds 64 levels")
	}
	kinds := map[string]LocatorKind{
		"CSS": LocatorCSS, "XPATH": LocatorXPath, "TEST_ID": LocatorTestID, "TEXT": LocatorText,
		"ROLE": LocatorRole, "LABEL": LocatorLabel, "PLACEHOLDER": LocatorPlaceholder,
		"ALT_TEXT": LocatorAltText, "TITLE": LocatorTitle, "AND": LocatorAnd, "OR": LocatorOr,
	}
	kind, exists := kinds[locator.Kind]
	if !exists {
		return Locator{}, NewError(CodeInvalidArgument, "locator kind is required")
	}
	if kind == LocatorRole && locator.Role == "" {
		return Locator{}, NewError(CodeInvalidArgument, "role locator requires role")
	}
	if locator.Attribute != "" && kind != LocatorTestID {
		return Locator{}, NewError(CodeInvalidArgument, "custom locator attribute is only valid for test IDs")
	}
	if kind != LocatorRole && kind != LocatorAnd && kind != LocatorOr && locator.Value == "" {
		return Locator{}, NewError(CodeInvalidArgument, "locator value is required")
	}
	if locator.LevelSet && locator.Level <= 0 {
		return Locator{}, NewError(CodeInvalidArgument, "invalid locator refinement: level must be positive")
	}
	validStates := map[string]bool{"checked": true, "disabled": true, "expanded": true, "pressed": true, "selected": true}
	for _, state := range locator.States {
		if !validStates[state] {
			return Locator{}, NewError(CodeInvalidArgument, "invalid locator refinement: unsupported state")
		}
	}
	match, matchErr := matchModeFromWire(locator.Match)
	if matchErr != nil {
		return Locator{}, matchErr
	}
	result := Locator{
		Kind: kind, Value: locator.Value, Role: locator.Role, Name: locator.Name, Attribute: locator.Attribute, Match: match,
		Level: locator.Level, LevelSet: locator.LevelSet, States: append([]string(nil), locator.States...),
		Nth: locator.Nth, NthSet: locator.NthSet || locator.First,
	}
	if locator.First {
		result.Nth = 0
	}
	if kind == LocatorAnd || kind == LocatorOr {
		if len(locator.Operands) < 2 {
			return Locator{}, NewError(CodeInvalidArgument, "combined locator requires at least two operands")
		}
		for _, operand := range locator.Operands {
			converted, err := locatorFromWireAt(operand, depth+1)
			if err != nil {
				return Locator{}, err
			}
			result.Operands = append(result.Operands, converted)
		}
	}
	if locator.Within != nil {
		within, err := locatorFromWireAt(locator.Within, depth+1)
		if err != nil {
			return Locator{}, err
		}
		result.Within = &within
	}
	filterKinds := map[string]LocatorFilterKind{"CONTAINS_TEXT": LocatorFilterContainsText, "CONTAINS": LocatorFilterContains, "WITHIN": LocatorFilterWithin}
	for _, filter := range locator.Filters {
		filterKind, ok := filterKinds[filter.Kind]
		if !ok {
			return Locator{}, NewError(CodeInvalidArgument, "unsupported locator filter")
		}
		filterMatch, err := matchModeFromWire(filter.Match)
		if err != nil {
			return Locator{}, err
		}
		converted := LocatorFilter{Kind: filterKind, Value: filter.Value, Match: filterMatch, Negate: filter.Negate}
		if filterKind == LocatorFilterContainsText {
			if filter.Value == "" {
				return Locator{}, NewError(CodeInvalidArgument, "contains-text filter requires value")
			}
		} else {
			selector, selectorErr := locatorFromWireAt(filter.Selector, depth+1)
			if selectorErr != nil {
				return Locator{}, selectorErr
			}
			converted.Selector = &selector
		}
		result.Filters = append(result.Filters, converted)
	}
	return result, nil
}

func matchModeFromWire(mode string) (MatchMode, *ProtocolError) {
	if mode == "" || mode == "EXACT" {
		return MatchExact, nil
	}
	if mode == "CONTAINS" {
		return MatchContains, nil
	}
	return MatchMode(0), NewError(CodeInvalidArgument, "unsupported locator match mode")
}

func assertionFromWire(assertion *WireAssertion) (Assertion, *ProtocolError) {
	if assertion == nil || assertion.Kind == "" {
		return Assertion{}, NewError(CodeInvalidArgument, "assertion kind is required")
	}
	kinds := map[string]AssertionKind{
		"VISIBLE": AssertionVisible, "TEXT": AssertionText, "COUNT": AssertionCount,
		"ATTRIBUTE": AssertionAttribute, "VALUE": AssertionValue, "URL": AssertionURL,
		"EVALUATE": AssertionEvaluate, "EXISTS": AssertionExists, "ENABLED": AssertionEnabled,
		"CLICKABLE": AssertionClickable, "PROPERTY": AssertionProperty, "ALL_TEXT": AssertionAllText,
		"REQUEST": AssertionRequest,
	}
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
	result := Assertion{Kind: kind, Attribute: assertion.Attribute, Property: assertion.Property, Method: assertion.Method, Expression: assertion.Expression, ExpectedString: assertion.ExpectedString, ExpectedCount: assertion.ExpectedCount, ExpectedJSON: assertion.ExpectedJSON, Match: match}
	if kind != AssertionURL && kind != AssertionEvaluate && kind != AssertionRequest {
		locator, err := locatorFromWire(assertion.Locator)
		if err != nil {
			return Assertion{}, err
		}
		result.Locator = locator
	}
	if assertion.Expectation != nil {
		expectation, err := expectationFromWire(assertion.Expectation, 0)
		if err != nil {
			return Assertion{}, err
		}
		result.Expectation = expectation
	} else {
		result.Expectation = legacyExpectation(result)
	}
	return result, nil
}

func expectationFromWire(expectation *WireExpectation, depth int) (Expectation, *ProtocolError) {
	if expectation == nil || expectation.Kind == "" {
		return Expectation{}, NewError(CodeInvalidArgument, "expectation kind is required")
	}
	if depth > 64 {
		return Expectation{}, NewError(CodeInvalidArgument, "expectation nesting exceeds 64 levels")
	}
	kinds := map[string]ExpectationKind{
		"EQUAL": ExpectEqual, "CONTAINS": ExpectContains, "REGEXP": ExpectRegexp,
		"PREFIX": ExpectPrefix, "SUFFIX": ExpectSuffix, "NUMBER": ExpectNumber,
		"EMPTY": ExpectEmpty, "ALL": ExpectAll, "ANY": ExpectAny, "NOT": ExpectNot,
		"ANYTHING": ExpectAnything,
	}
	kind, ok := kinds[expectation.Kind]
	if !ok {
		return Expectation{}, NewError(CodeInvalidArgument, "unsupported expectation")
	}
	result := Expectation{Kind: kind, ExpectedJSON: expectation.ExpectedJSON, Operator: expectation.Operator}
	if kind >= ExpectEqual && kind <= ExpectNumber {
		if expectation.ExpectedJSON == "" {
			return Expectation{}, NewError(CodeInvalidArgument, "expectation requires expectedJson")
		}
		var decoded any
		if err := json.Unmarshal([]byte(expectation.ExpectedJSON), &decoded); err != nil {
			return Expectation{}, NewError(CodeInvalidArgument, fmt.Sprintf("expectation expectedJson: %v", err))
		}
	}
	if kind == ExpectNumber {
		switch expectation.Operator {
		case "=", "==", "!=", ">", ">=", "<", "<=":
		default:
			return Expectation{}, NewError(CodeInvalidArgument, "unsupported numeric operator")
		}
	}
	for _, child := range expectation.Children {
		converted, err := expectationFromWire(child, depth+1)
		if err != nil {
			return Expectation{}, err
		}
		result.Children = append(result.Children, converted)
	}
	if kind == ExpectNot && len(result.Children) != 1 {
		return Expectation{}, NewError(CodeInvalidArgument, "not expectation requires exactly one child")
	}
	if (kind == ExpectAll || kind == ExpectAny) && len(result.Children) == 0 {
		return Expectation{}, NewError(CodeInvalidArgument, "compound expectation requires at least one child")
	}
	return result, nil
}

func legacyExpectation(assertion Assertion) Expectation {
	switch assertion.Kind {
	case AssertionVisible, AssertionExists, AssertionEnabled, AssertionClickable:
		return Expectation{Kind: ExpectEqual, ExpectedJSON: "true"}
	case AssertionCount:
		return Expectation{Kind: ExpectEqual, ExpectedJSON: fmt.Sprintf("%d", assertion.ExpectedCount)}
	case AssertionValue, AssertionEvaluate:
		return Expectation{Kind: ExpectEqual, ExpectedJSON: assertion.ExpectedJSON}
	default:
		kind := ExpectEqual
		if assertion.Match == MatchContains {
			kind = ExpectContains
		}
		expected, _ := json.Marshal(assertion.ExpectedString)
		return Expectation{Kind: kind, ExpectedJSON: string(expected)}
	}
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

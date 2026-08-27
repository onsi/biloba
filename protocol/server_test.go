package protocol_test

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/onsi/biloba/protocol"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProtocol(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Protocol Suite")
}

var _ = Describe("driver protocol", func() {
	It("negotiates the version and advertises capabilities", func() {
		client, cleanup := startTestServer(&fakeBackend{})
		DeferCleanup(cleanup)

		var response protocol.HandshakeResponse
		Expect(client.call("handshake", protocol.HandshakeRequest{ProtocolVersion: protocol.Version}, &response)).To(Succeed())
		Expect(response.ProtocolVersion).To(Equal(protocol.Version))
		Expect(response.Capabilities).NotTo(BeEmpty())

		err := client.call("handshake", protocol.HandshakeRequest{ProtocolVersion: "999"}, nil)
		Expect(err).To(MatchError(ContainSubstring("protocol version mismatch")))
		Expect(err.Code).To(Equal(protocol.CodeProtocolMismatch))
	})

	It("opens, prepares, and closes a session", func() {
		backend := &fakeBackend{}
		client, cleanup := startTestServer(backend)
		DeferCleanup(cleanup)

		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())
		Expect(opened.SessionID).NotTo(BeEmpty())
		Expect(client.call("prepareSession", protocol.SessionRequest{SessionID: opened.SessionID}, nil)).To(Succeed())
		Expect(client.call("closeSession", protocol.SessionRequest{SessionID: opened.SessionID}, nil)).To(Succeed())
		err := client.call("prepareSession", protocol.SessionRequest{SessionID: opened.SessionID}, nil)
		Expect(err.Code).To(Equal(protocol.CodeTargetNotFound))
		Expect(backend.opened).To(Equal(1))
		Expect(backend.session.prepared).To(Equal(1))
		Expect(backend.session.closed).To(Equal(1))
	})

	// `invoke` is what makes an expression's meaning explicit on the wire: without it the daemon has
	// to guess from the argument count, and the same string means two different things.
	It("carries an explicit invoke flag through to the session", func() {
		recorder := &recordingSession{}
		client, cleanup := startTestServer(&fakeBackend{custom: recorder})
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		Expect(client.call("evaluate", json.RawMessage(`{"sessionId":"`+opened.SessionID+`","expression":"(a) => a + 1","argumentsJson":"[]","invoke":true}`), nil)).To(BeNil())
		Expect(recorder.lastOperation().Invoke).To(HaveValue(BeTrue()))

		Expect(client.call("evaluate", protocol.EvaluateRequest{SessionID: opened.SessionID, Expression: "document.title", ArgumentsJSON: "[]"}, nil)).To(BeNil())
		Expect(recorder.lastOperation().Invoke).To(BeNil(), "a client that says nothing must stay distinguishable from one that says false")
	})

	It("propagates request deadlines to the session", func() {
		backend := &fakeBackend{session: &fakeSession{blockNavigate: true, cancelled: make(chan struct{})}}
		client, cleanup := startTestServer(backend)
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		err := client.callWithTimeout("navigate", protocol.NavigateRequest{SessionID: opened.SessionID, URL: "https://example.test"}, 25, nil)
		Expect(err.Code).To(Equal(protocol.CodeTimeout))
		Eventually(backend.session.cancelled).Should(BeClosed())
	})

	It("cancels an in-flight request explicitly", func() {
		backend := &fakeBackend{session: &fakeSession{blockNavigate: true, cancelled: make(chan struct{})}}
		client, cleanup := startTestServer(backend)
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		requestID, response := client.begin("navigate", protocol.NavigateRequest{SessionID: opened.SessionID, URL: "https://example.test"}, 0)
		Eventually(backend.session.started).Should(BeClosed())
		client.cancel(requestID)
		protocolResponse := <-response
		Expect(protocolResponse.Error.Code).To(Equal(protocol.CodeCancelled))
		Eventually(backend.session.cancelled).Should(BeClosed())
	})

	It("reports many internal poll attempts from one request", func() {
		backend := &fakeBackend{session: &fakeSession{result: protocol.Result{
			Matched: true, ObservedJSON: `"ready"`, Attempts: 3,
			Trajectory: []protocol.Observation{{Attempt: 1}, {Attempt: 2}, {Attempt: 3}},
		}}}
		client, cleanup := startTestServer(backend)
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		var result protocol.OperationResult
		Expect(client.call("assert", protocol.AssertRequest{
			SessionID: opened.SessionID,
			Assertion: &protocol.WireAssertion{Kind: "TEXT", Locator: &protocol.WireLocator{Kind: "CSS", Value: "#status"}, ExpectedString: "ready"},
		}, &result)).To(Succeed())
		Expect(result.AttemptCount).To(Equal(uint32(3)))
		Expect(result.Trajectory).To(HaveLen(3))
		Expect(result.RPCRequestCount).To(Equal(uint32(1)))
		Expect(result.RPCResponseCount).To(Equal(uint32(1)))
	})

	It("carries composed locators without flattening their structure", func() {
		recorder := &recordingSession{}
		client, cleanup := startTestServer(&fakeBackend{custom: recorder})
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		Expect(client.call("click", protocol.LocatorRequest{
			SessionID: opened.SessionID,
			Locator: &protocol.WireLocator{
				Kind: "CSS", Value: ".row", Nth: 1, NthSet: true,
				Within:  &protocol.WireLocator{Kind: "TEST_ID", Value: "results"},
				Filters: []protocol.WireLocatorFilter{{Kind: "CONTAINS_TEXT", Value: "Ada", Match: "CONTAINS"}},
			},
		}, nil)).To(Succeed())

		locator := recorder.lastOperation().Locator
		Expect(locator.NthSet).To(BeTrue())
		Expect(locator.Nth).To(Equal(1))
		Expect(locator.Within).NotTo(BeNil())
		Expect(locator.Within.Kind).To(Equal(protocol.LocatorTestID))
		Expect(locator.Filters).To(HaveLen(1))
		Expect(locator.Filters[0].Kind).To(Equal(protocol.LocatorFilterContainsText))
		Expect(locator.Filters[0].Value).To(Equal("Ada"))
	})

	It("carries typed matcher trees and consistency polling", func() {
		recorder := &recordingSession{}
		client, cleanup := startTestServer(&fakeBackend{custom: recorder})
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		Expect(client.call("assert", protocol.AssertRequest{
			SessionID: opened.SessionID,
			Assertion: &protocol.WireAssertion{
				Kind:     "PROPERTY",
				Locator:  &protocol.WireLocator{Kind: "TEST_ID", Value: "status"},
				Property: "textContent",
				Expectation: &protocol.WireExpectation{Kind: "ALL", Children: []*protocol.WireExpectation{
					{Kind: "CONTAINS", ExpectedJSON: `"ready"`},
					{Kind: "NOT", Children: []*protocol.WireExpectation{{Kind: "EMPTY"}}},
				}},
			},
			Poll: protocol.PollOptions{Mode: "CONSISTENTLY", TimeoutMS: 50},
		}, nil)).To(Succeed())

		operation := recorder.lastOperation()
		Expect(operation.Poll.Mode).To(Equal(protocol.PollConsistently))
		Expect(operation.Assertion.Kind).To(Equal(protocol.AssertionProperty))
		Expect(operation.Assertion.Property).To(Equal("textContent"))
		Expect(operation.Assertion.Expectation.Kind).To(Equal(protocol.ExpectAll))
		Expect(operation.Assertion.Expectation.Children).To(HaveLen(2))
		Expect(operation.Assertion.Expectation.Children[1].Kind).To(Equal(protocol.ExpectNot))
	})

	It("carries a typed request observation assertion", func() {
		recorder := &recordingSession{}
		client, cleanup := startTestServer(&fakeBackend{custom: recorder})
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		Expect(client.call("assert", protocol.AssertRequest{
			SessionID: opened.SessionID,
			Assertion: &protocol.WireAssertion{
				Kind: "REQUEST", Method: "POST",
				Expectation: &protocol.WireExpectation{Kind: "SUFFIX", ExpectedJSON: `"/saved"`},
			},
		}, nil)).To(Succeed())

		operation := recorder.lastOperation()
		Expect(operation.Assertion.Kind).To(Equal(protocol.AssertionRequest))
		Expect(operation.Assertion.Method).To(Equal("POST"))
		Expect(operation.Assertion.Expectation.Kind).To(Equal(protocol.ExpectSuffix))
	})

	It("carries response hold lifecycle operations", func() {
		recorder := &recordingSession{}
		client, cleanup := startTestServer(&fakeBackend{custom: recorder})
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		Expect(client.call("holdResponse", protocol.HoldResponseRequest{
			SessionID:   opened.SessionID,
			Expectation: &protocol.WireExpectation{Kind: "SUFFIX", ExpectedJSON: `"/save"`},
		}, nil)).To(Succeed())
		Expect(recorder.lastOperation().Kind).To(Equal(protocol.OperationHoldResponse))

		Expect(client.call("awaitResponseHold", protocol.ResponseHoldRequest{SessionID: opened.SessionID, HoldID: "hold-1"}, nil)).To(Succeed())
		Expect(recorder.lastOperation().Kind).To(Equal(protocol.OperationAwaitResponseHold))
		Expect(client.call("releaseResponseHold", protocol.ResponseHoldRequest{SessionID: opened.SessionID, HoldID: "hold-1"}, nil)).To(Succeed())
		Expect(recorder.lastOperation().Kind).To(Equal(protocol.OperationReleaseResponseHold))
	})

	It("carries realistic pointer and keyboard actions", func() {
		recorder := &recordingSession{}
		client, cleanup := startTestServer(&fakeBackend{custom: recorder})
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		Expect(client.call("click", protocol.LocatorRequest{
			SessionID: opened.SessionID,
			Locator:   &protocol.WireLocator{Kind: "TEST_ID", Value: "save"},
			Realistic: true,
		}, nil)).To(Succeed())
		Expect(recorder.lastOperation().Realistic).To(BeTrue())

		Expect(client.call("type", protocol.TypeRequest{
			SessionID: opened.SessionID,
			Locator:   &protocol.WireLocator{Kind: "TEST_ID", Value: "name"},
			Keys:      "Ada",
			Realistic: true,
		}, nil)).To(Succeed())
		operation := recorder.lastOperation()
		Expect(operation.Kind).To(Equal(protocol.OperationType))
		Expect(operation.Keys).To(Equal("Ada"))
		Expect(operation.Realistic).To(BeTrue())

		Expect(client.call("sendKeys", protocol.SendKeysRequest{SessionID: opened.SessionID, Keys: "\x1b"}, nil)).To(Succeed())
		Expect(recorder.lastOperation().Kind).To(Equal(protocol.OperationSendKeys))

		Expect(client.call("dragTo", protocol.DragToRequest{
			SessionID: opened.SessionID,
			Source:    &protocol.WireLocator{Kind: "TEST_ID", Value: "card"},
			Target:    &protocol.WireLocator{Kind: "TEST_ID", Value: "column"},
		}, nil)).To(Succeed())
		operation = recorder.lastOperation()
		Expect(operation.Kind).To(Equal(protocol.OperationDragTo))
		Expect(operation.Locator.Value).To(Equal("card"))
		Expect(operation.Target.Value).To(Equal("column"))
	})

	It("carries async evaluation, viewport, and upload operations", func() {
		recorder := &recordingSession{}
		client, cleanup := startTestServer(&fakeBackend{custom: recorder})
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		Expect(client.call("evaluate", protocol.EvaluateRequest{
			SessionID: opened.SessionID, Expression: "Promise.resolve(1)", AwaitPromise: true,
		}, nil)).To(Succeed())
		Expect(recorder.lastOperation().AwaitPromise).To(BeTrue())

		Expect(client.call("setWindowSize", protocol.SetWindowSizeRequest{
			SessionID: opened.SessionID, Width: 375, Height: 812,
		}, nil)).To(Succeed())
		operation := recorder.lastOperation()
		Expect(operation.Kind).To(Equal(protocol.OperationSetWindowSize))
		Expect(operation.Width).To(Equal(375))
		Expect(operation.Height).To(Equal(812))

		Expect(client.call("setUpload", protocol.SetUploadRequest{
			SessionID: opened.SessionID,
			Locator:   &protocol.WireLocator{Kind: "TEST_ID", Value: "upload"},
			Paths:     []string{"/tmp/avatar.txt"},
		}, nil)).To(Succeed())
		operation = recorder.lastOperation()
		Expect(operation.Kind).To(Equal(protocol.OperationSetUpload))
		Expect(operation.Paths).To(Equal([]string{"/tmp/avatar.txt"}))
	})

	It("opens sibling tabs and carries target lifecycle operations", func() {
		recorder := &recordingSession{}
		client, cleanup := startTestServer(&fakeBackend{custom: recorder})
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		var sibling protocol.OpenSessionResponse
		Expect(client.call("newTab", protocol.SessionRequest{SessionID: opened.SessionID}, &sibling)).To(Succeed())
		Expect(sibling.SessionID).NotTo(BeEmpty())
		Expect(recorder.newTabCalls).To(Equal(1))

		Expect(client.call("addInitScript", protocol.AddInitScriptRequest{
			SessionID: sibling.SessionID, Script: "window.ready = true",
		}, nil)).To(Succeed())
		operation := recorder.lastOperation()
		Expect(operation.Kind).To(Equal(protocol.OperationAddInitScript))
		Expect(operation.Expression).To(Equal("window.ready = true"))

		Expect(client.call("activate", protocol.SessionRequest{SessionID: sibling.SessionID}, nil)).To(Succeed())
		Expect(recorder.lastOperation().Kind).To(Equal(protocol.OperationActivate))
	})

	It("preserves a structured backend error", func() {
		backend := &fakeBackend{session: &fakeSession{executeErr: protocol.NewError(protocol.CodeInvalidArgument, "bad value")}}
		client, cleanup := startTestServer(backend)
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())
		err := client.call("navigate", protocol.NavigateRequest{SessionID: opened.SessionID, URL: "https://example.test"}, nil)
		Expect(err.Code).To(Equal(protocol.CodeInvalidArgument))
		Expect(err.Message).To(Equal("bad value"))
	})

	It("serializes commands within a session", func() {
		blocking := &blockingSession{entered: make(chan string, 2), release: make(chan struct{}, 2)}
		client, cleanup := startTestServer(&fakeBackend{custom: blocking})
		DeferCleanup(cleanup)
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())
		results := make(chan error, 2)
		for _, destination := range []string{"https://example.test/first", "https://example.test/second"} {
			go func(destination string) {
				results <- client.call("navigate", protocol.NavigateRequest{SessionID: opened.SessionID, URL: destination}, nil)
			}(destination)
		}

		Eventually(blocking.entered).Should(Receive())
		Consistently(blocking.entered, 50*time.Millisecond).ShouldNot(Receive())
		blocking.release <- struct{}{}
		Eventually(results).Should(Receive(Succeed()))
		Eventually(blocking.entered).Should(Receive())
		blocking.release <- struct{}{}
		Eventually(results).Should(Receive(Succeed()))
		Expect(blocking.maxActive).To(Equal(1))
	})
})

type testClient struct {
	connection net.Conn
	writer     *protocol.FramedWriter
	nextID     atomic.Uint64
	mu         sync.Mutex
	pending    map[uint64]chan protocol.Response
}

func startTestServer(backend protocol.Backend) (*testClient, func()) {
	clientConnection, serverConnection := net.Pipe()
	server := protocol.NewServer(backend)
	client := &testClient{connection: clientConnection, writer: protocol.NewFramedWriter(clientConnection), pending: map[uint64]chan protocol.Response{}}
	go func() { _ = protocol.ServeStdio(context.Background(), server, serverConnection, serverConnection) }()
	go client.readResponses()
	return client, func() {
		Expect(clientConnection.Close()).To(Succeed())
		Expect(serverConnection.Close()).To(Succeed())
		Expect(server.Close()).To(Succeed())
	}
}

func (c *testClient) readResponses() {
	reader := protocol.NewFramedReader(c.connection)
	for {
		var response protocol.Response
		if reader.Read(&response) != nil {
			return
		}
		c.mu.Lock()
		pending := c.pending[response.ID]
		delete(c.pending, response.ID)
		c.mu.Unlock()
		if pending != nil {
			pending <- response
		}
	}
}

func (c *testClient) begin(method string, params any, timeoutMS int64) (uint64, <-chan protocol.Response) {
	id := c.nextID.Add(1)
	encoded, err := json.Marshal(params)
	Expect(err).NotTo(HaveOccurred())
	response := make(chan protocol.Response, 1)
	c.mu.Lock()
	c.pending[id] = response
	c.mu.Unlock()
	Expect(c.writer.Write(protocol.Request{ID: id, Method: method, Params: encoded, TimeoutMS: timeoutMS})).To(Succeed())
	return id, response
}

func (c *testClient) call(method string, params any, result any) *protocol.ProtocolError {
	return c.callWithTimeout(method, params, 0, result)
}

func (c *testClient) callWithTimeout(method string, params any, timeoutMS int64, result any) *protocol.ProtocolError {
	_, responses := c.begin(method, params, timeoutMS)
	response := <-responses
	if response.Error != nil {
		return response.Error
	}
	if result != nil {
		encoded, err := json.Marshal(response.Result)
		Expect(err).NotTo(HaveOccurred())
		Expect(json.Unmarshal(encoded, result)).To(Succeed())
	}
	return nil
}

func (c *testClient) cancel(requestID uint64) {
	params, err := json.Marshal(protocol.CancelRequest{RequestID: requestID})
	Expect(err).NotTo(HaveOccurred())
	Expect(c.writer.Write(protocol.Request{Method: "cancel", Params: params})).To(Succeed())
}

type fakeBackend struct {
	mu      sync.Mutex
	opened  int
	session *fakeSession
	custom  protocol.Session
}

func (b *fakeBackend) OpenSession(context.Context) (protocol.Session, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.opened++
	if b.custom != nil {
		return b.custom, nil
	}
	if b.session == nil {
		b.session = &fakeSession{}
	}
	if b.session.started == nil {
		b.session.started = make(chan struct{})
	}
	return b.session, nil
}

func (b *fakeBackend) Close() error { return nil }

type blockingSession struct {
	mu                sync.Mutex
	active, maxActive int
	entered           chan string
	release           chan struct{}
}

func (*blockingSession) Prepare(context.Context) error { return nil }
func (*blockingSession) Close() error                  { return nil }
func (s *blockingSession) Execute(_ context.Context, operation protocol.Operation) (protocol.Result, error) {
	s.mu.Lock()
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.mu.Unlock()
	s.entered <- operation.URL
	<-s.release
	s.mu.Lock()
	s.active--
	s.mu.Unlock()
	return protocol.Result{Matched: true, Attempts: 1}, nil
}

// recordingSession keeps the last operation the server handed down, so a spec can pin what a wire
// request actually decodes into.
type recordingSession struct {
	mu          sync.Mutex
	operation   protocol.Operation
	newTabCalls int
}

func (*recordingSession) Prepare(context.Context) error { return nil }
func (*recordingSession) Close() error                  { return nil }
func (s *recordingSession) NewTab(context.Context) (protocol.Session, error) {
	s.mu.Lock()
	s.newTabCalls++
	s.mu.Unlock()
	return s, nil
}
func (s *recordingSession) Execute(_ context.Context, operation protocol.Operation) (protocol.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.operation = operation
	return protocol.Result{Matched: true, Attempts: 1}, nil
}

func (s *recordingSession) lastOperation() protocol.Operation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.operation
}

type fakeSession struct {
	prepared, closed      int
	blockNavigate         bool
	started, cancelled    chan struct{}
	startOnce, cancelOnce sync.Once
	result                protocol.Result
	executeErr            error
}

func (s *fakeSession) Prepare(context.Context) error { s.prepared++; return nil }
func (s *fakeSession) Execute(ctx context.Context, operation protocol.Operation) (protocol.Result, error) {
	if s.executeErr != nil {
		return protocol.Result{}, s.executeErr
	}
	if s.blockNavigate && operation.Kind == protocol.OperationNavigate {
		s.startOnce.Do(func() { close(s.started) })
		<-ctx.Done()
		s.cancelOnce.Do(func() { close(s.cancelled) })
		return protocol.Result{}, ctx.Err()
	}
	if s.result.Attempts != 0 {
		return s.result, nil
	}
	return protocol.Result{Matched: true, Attempts: 1}, nil
}
func (s *fakeSession) Close() error { s.closed++; return nil }

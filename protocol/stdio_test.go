package protocol_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"github.com/onsi/biloba/protocol"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// One worker owns one daemon and one daemon owns every session that worker is driving, so a
// malformed or hostile request has to cost that request rather than the process.  These specs pin
// the blast radius: after each of them the same connection must still answer.
var _ = Describe("serving requests over framed stdio", func() {
	var client *testClient
	var backend *fakeBackend

	BeforeEach(func() {
		backend = &fakeBackend{}
		var cleanup func()
		client, cleanup = startTestServer(backend)
		DeferCleanup(cleanup)
	})

	// stillServing proves the connection survived whatever the previous request did to it.
	stillServing := func() {
		GinkgoHelper()
		var response protocol.HandshakeResponse
		Expect(client.call("handshake", protocol.HandshakeRequest{ProtocolVersion: protocol.Version}, &response)).To(Succeed())
		Expect(response.ProtocolVersion).To(Equal(protocol.Version))
	}

	It("rejects an unknown method and keeps serving", func() {
		err := client.call("teleport", struct{}{}, nil)

		Expect(err.Code).To(Equal(protocol.CodeInvalidArgument))
		Expect(err.Message).To(ContainSubstring(`unsupported method "teleport"`))
		stillServing()
	})

	It("rejects params that are not the shape the method expects", func() {
		_, responses := client.beginRaw("navigate", json.RawMessage(`{"sessionId": 17}`), 0)
		response := <-responses

		Expect(response.Error.Code).To(Equal(protocol.CodeInvalidArgument))
		Expect(response.Error.Message).To(ContainSubstring("invalid request parameters"))
		stillServing()
	})

	It("answers a zero request id instead of tearing the daemon down", func() {
		// A zero id cannot be correlated, so the answer is the point: the daemon has to survive a
		// client that sends one, because every other session on this worker rides the same pipe.
		responses := client.watch(0)
		Expect(client.writer.Write(protocol.Request{ID: 0, Method: "handshake"})).To(Succeed())

		var response protocol.Response
		Eventually(responses).Should(Receive(&response))
		Expect(response.Error.Code).To(Equal(protocol.CodeInvalidArgument))
		Expect(response.Error.Message).To(ContainSubstring("non-zero"))
		stillServing()
	})

	It("answers a malformed frame body instead of tearing the daemon down", func() {
		// The length prefix was honoured and the payload was consumed in full, so the stream is
		// still aligned: only that one request is garbage.  Killing the daemon here would take
		// every other session on this worker with it.
		responses := client.watch(0)
		Expect(writeRawFrame(client.connection, []byte(`{"id": 3, "method": `))).To(Succeed())

		var response protocol.Response
		Eventually(responses).Should(Receive(&response))
		Expect(response.Error.Code).To(Equal(protocol.CodeInvalidArgument))
		Expect(response.Error.Message).To(ContainSubstring("malformed request frame"))
		stillServing()
	})

	It("answers a response too large to frame instead of tearing the daemon down", func() {
		// The writer rejects an over-large frame before it writes a byte, so the stream is still
		// aligned: this costs the one request, not the daemon that carries every other session on
		// this worker.  Nothing caps a poll trajectory, so a response can genuinely grow past the
		// frame limit - and the caller has to hear about it rather than wait out its timeout.
		backend.session = &fakeSession{result: protocol.Result{
			Matched: true, Attempts: 1, ObservedJSON: strings.Repeat("x", protocol.MaxFrameSize+1),
		}}
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())

		// Eventually rather than a bare receive: a regression here does not answer at all, and that
		// has to fail this spec rather than hang the suite waiting on a daemon that has gone away.
		_, responses := client.begin("assert", protocol.AssertRequest{
			SessionID: opened.SessionID,
			Assertion: &protocol.WireAssertion{Kind: "TEXT", Locator: &protocol.WireLocator{Kind: "CSS", Value: "#status"}, ExpectedString: "ready"},
		}, 0)

		var response protocol.Response
		Eventually(responses, 5*time.Second).Should(Receive(&response))
		Expect(response.Error.Code).To(Equal(protocol.CodeDriver))
		Expect(response.Error.Message).To(ContainSubstring("the protocol caps a single response"))
		stillServing()
	})

	It("ends the loop when the stream itself is desynced", func() {
		// A truncated frame is the other kind of failure: the reader cannot know where the next
		// frame starts, so there is nothing left to serve and pretending otherwise would answer
		// garbage forever.
		clientConnection, serverConnection := net.Pipe()
		server := protocol.NewServer(&fakeBackend{})
		DeferCleanup(func() { Expect(server.Close()).To(Succeed()) })
		served := make(chan error, 1)
		go func() { served <- protocol.ServeStdio(context.Background(), server, serverConnection, io.Discard) }()

		var header [4]byte
		binary.LittleEndian.PutUint32(header[:], 64)
		_, err := clientConnection.Write(append(header[:], []byte(`{"id":1}`)...))
		Expect(err).NotTo(HaveOccurred())
		Expect(clientConnection.Close()).To(Succeed())

		Eventually(served, time.Second).Should(Receive(MatchError(ContainSubstring("read protocol request"))))
		Expect(serverConnection.Close()).To(Succeed())
	})

	It("rejects a duplicate in-flight id without disturbing the original request", func() {
		backend.session = &fakeSession{blockNavigate: true, cancelled: make(chan struct{})}
		var opened protocol.OpenSessionResponse
		Expect(client.call("openSession", struct{}{}, &opened)).To(Succeed())
		id, _ := client.begin("navigate", protocol.NavigateRequest{SessionID: opened.SessionID, URL: "https://example.test"}, 0)
		Eventually(backend.session.started).Should(BeClosed())

		// same id, while the first one is still in flight
		duplicate := client.watch(id)
		Expect(client.writer.Write(protocol.Request{ID: id, Method: "handshake"})).To(Succeed())
		var response protocol.Response
		Eventually(duplicate).Should(Receive(&response))
		Expect(response.Error.Code).To(Equal(protocol.CodeInvalidArgument))
		Expect(response.Error.Message).To(ContainSubstring("duplicate request id"))

		// the original is untouched: it is still running, and still the request that id cancels
		original := client.watch(id)
		Consistently(original, 50*time.Millisecond).ShouldNot(Receive())
		client.cancel(id)
		Eventually(original).Should(Receive(&response))
		Expect(response.Error.Code).To(Equal(protocol.CodeCancelled))
		Eventually(backend.session.cancelled).Should(BeClosed())
		stillServing()
	})

	It("ignores a cancellation for a request that is not in flight", func() {
		client.cancel(4242)

		stillServing()
	})

	It("cancels in-flight work when the stream ends rather than leaking the request", func() {
		// A vitest worker that dies takes its end of the pipe with it.  The daemon has to unwind
		// the request that was running, or Chrome keeps doing work nobody is waiting for.
		blocking := &fakeSession{blockNavigate: true, cancelled: make(chan struct{}), started: make(chan struct{})}
		clientConnection, serverConnection := net.Pipe()
		server := protocol.NewServer(&fakeBackend{session: blocking})
		DeferCleanup(func() { Expect(server.Close()).To(Succeed()) })
		served := make(chan error, 1)
		go func() {
			served <- protocol.ServeStdio(context.Background(), server, serverConnection, serverConnection)
		}()
		peer := &testClient{connection: clientConnection, writer: protocol.NewFramedWriter(clientConnection), pending: map[uint64]chan protocol.Response{}}
		go peer.readResponses()

		var opened protocol.OpenSessionResponse
		Expect(peer.call("openSession", struct{}{}, &opened)).To(Succeed())
		peer.begin("navigate", protocol.NavigateRequest{SessionID: opened.SessionID, URL: "https://example.test"}, 0)
		Eventually(blocking.started).Should(BeClosed())

		Expect(clientConnection.Close()).To(Succeed())

		Eventually(served, time.Second).Should(Receive(Succeed()))
		Expect(blocking.cancelled).To(BeClosed(), "ServeStdio must cancel in-flight work before returning, not orphan it")
		Expect(serverConnection.Close()).To(Succeed())
	})

	It("returns an asynchronous response write failure even while input remains open", func() {
		clientConnection, serverConnection := net.Pipe()
		server := protocol.NewServer(&fakeBackend{})
		DeferCleanup(func() { Expect(server.Close()).To(Succeed()) })
		served := make(chan error, 1)
		go func() {
			served <- protocol.ServeStdio(context.Background(), server, serverConnection, failingWriter{})
		}()

		Expect(protocol.NewFramedWriter(clientConnection).Write(protocol.Request{
			ID: 1, Method: "handshake", Params: json.RawMessage(`{"protocolVersion":"1"}`),
		})).To(Succeed())

		Eventually(served, time.Second).Should(Receive(MatchError(ContainSubstring("write protocol response"))))
		Expect(clientConnection.Close()).To(Succeed())
		Expect(serverConnection.Close()).To(Succeed())
	})
})

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("output is closed") }

// beginRaw is begin for a payload that is deliberately not the shape the method expects.
func (c *testClient) beginRaw(method string, params json.RawMessage, timeoutMS int64) (uint64, <-chan protocol.Response) {
	GinkgoHelper()
	id := c.nextID.Add(1)
	response := c.watch(id)
	Expect(c.writer.Write(protocol.Request{ID: id, Method: method, Params: params, TimeoutMS: timeoutMS})).To(Succeed())
	return id, response
}

// watch registers interest in an id the test is about to write by hand, so a response the client
// would otherwise drop on the floor can be asserted on.
func (c *testClient) watch(id uint64) <-chan protocol.Response {
	response := make(chan protocol.Response, 1)
	c.mu.Lock()
	c.pending[id] = response
	c.mu.Unlock()
	return response
}

// writeRawFrame writes a payload the FramedWriter would refuse to produce: a well-framed body that
// is not valid JSON.
func writeRawFrame(writer io.Writer, payload []byte) error {
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], uint32(len(payload)))
	_, err := writer.Write(append(header[:], payload...))
	return err
}

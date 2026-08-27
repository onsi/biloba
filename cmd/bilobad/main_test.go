package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/onsi/biloba/engine"

	"github.com/onsi/biloba/protocol"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBilobad(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bilobad Suite")
}

var _ = Describe("bilobad", func() {
	It("parses daemon flags", func() {
		parsed, err := parseConfig([]string{
			"-chrome-path", "/opt/chrome",
			"-chrome-ws-url", "ws://127.0.0.1:9222/devtools/browser/test",
			"-artifact-dir", "/tmp/artifacts",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed).To(Equal(config{
			chromePath: "/opt/chrome", chromeWSURL: "ws://127.0.0.1:9222/devtools/browser/test", artifactDir: "/tmp/artifacts",
		}))
	})

	Describe("evaluation arguments", func() {
		It("preserves an expression for an empty argument array", func() {
			plain, err := evaluationScript("document.title", `[]`, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(plain).To(Equal("document.title"))
		})

		It("applies a JSON argument array", func() {
			script, err := evaluationScript("(left, right) => left + right", `[2,3]`, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(script).To(Equal(`((left, right) => left + right)(...[2,3])`))
		})

		It("rejects a non-array argument value", func() {
			_, err := evaluationScript("value => value", `{}`, nil)
			Expect(err).To(HaveOccurred())
		})

		// Without an explicit signal the same expression means two different things depending on a
		// separate field: a function sent with no arguments evaluates to its own source instead of
		// being called.  `invoke` is what settles it.
		It("calls a function expression with an empty argument array when invoke is set", func() {
			script, err := evaluationScript("(a) => a + 1", `[]`, boolPointer(true))
			Expect(err).NotTo(HaveOccurred())
			Expect(script).To(Equal(`((a) => a + 1)(...[])`))
		})

		It("evaluates an expression verbatim when invoke is explicitly false", func() {
			script, err := evaluationScript("document.title", `[]`, boolPointer(false))
			Expect(err).NotTo(HaveOccurred())
			Expect(script).To(Equal("document.title"))
		})

		It("refuses arguments the client also said not to invoke with", func() {
			_, err := evaluationScript("(a) => a + 1", `[1]`, boolPointer(false))
			Expect(err).To(MatchError(ContainSubstring("invoke")))
		})
	})

	// The client arms its timer at the same deadline it puts on the request, so a fixed diagnostics
	// budget can outlive it - and a late response is dropped, taking the outline, the screenshot,
	// and the trajectory with it.
	Describe("the diagnostics budget", func() {
		It("fits inside what is left of the request deadline", func() {
			now := time.Now()
			ctx, cancel := context.WithDeadline(context.Background(), now.Add(900*time.Millisecond))
			defer cancel()

			budget := diagnosticsBudget(ctx, now)
			Expect(budget).To(BeNumerically("<", 900*time.Millisecond))
			Expect(budget).To(BeNumerically(">", 500*time.Millisecond))
		})

		It("caps a generous deadline", func() {
			now := time.Now()
			ctx, cancel := context.WithDeadline(context.Background(), now.Add(time.Minute))
			defer cancel()

			Expect(diagnosticsBudget(ctx, now)).To(Equal(diagnosticsCap))
		})

		It("still makes a best effort when the deadline is all but gone", func() {
			now := time.Now()
			ctx, cancel := context.WithDeadline(context.Background(), now.Add(time.Millisecond))
			defer cancel()

			Expect(diagnosticsBudget(ctx, now)).To(Equal(diagnosticsFloor))
		})

		It("falls back to the fixed budget for a request with no deadline", func() {
			Expect(diagnosticsBudget(context.Background(), time.Now())).To(Equal(diagnosticsCap))
		})
	})

	Describe("comparing a value assertion against its expected JSON", func() {
		It("matches an equal value and rejects a different one", func() {
			Expect(jsonEqual("selected", `"selected"`)).To(BeTrue())
			Expect(jsonEqual("selected", `"other"`)).To(BeFalse())
		})

		It("treats an unparseable expectation as fatal rather than as a mismatch", func() {
			// A mismatch is retried to the deadline and reported as an assertion timeout quoting the
			// observed value - which says nothing about the expectation being the broken part.
			matched, err := jsonEqual("selected", `{not json`)

			Expect(matched).To(BeFalse())
			Expect(engine.IsFatal(err)).To(BeTrue(), "polling cannot turn invalid JSON into a value")
			var protocolErr *protocol.ProtocolError
			Expect(errors.As(err, &protocolErr)).To(BeTrue())
			Expect(protocolErr.Code).To(Equal(protocol.CodeInvalidArgument))
			Expect(protocolErr.Message).To(ContainSubstring("expectedJson is not valid JSON"))
		})

		It("keeps the fatal failure's own code on the way out to the client", func() {
			_, err := jsonEqual("selected", `{not json`)

			converted := engineRPCError(err)
			var protocolErr *protocol.ProtocolError
			Expect(errors.As(converted, &protocolErr)).To(BeTrue())
			Expect(protocolErr.Code).To(Equal(protocol.CodeInvalidArgument), "flattening this to DRIVER_ERROR would lose why it failed")
		})
	})

	// SpecTimeout, and every pipe operation raced against the daemon exiting.  run() brings Chrome up
	// before ServeStdio reads a byte of stdin, and these are io.Pipes: if the daemon never starts -
	// no chrome-headless-shell on the box, say - nothing ever reads, and an unguarded write to
	// stdinWriter blocks forever.  That burns the whole suite budget and reports as a timeout on this
	// spec, which says nothing about the cause.  Racing `done` surfaces the daemon's own error
	// instead, in milliseconds.
	It("serves a framed handshake and shuts down when its parent closes stdin", func(ctx SpecContext) {
		stdinReader, stdinWriter := io.Pipe()
		stdoutReader, stdoutWriter := io.Pipe()
		done := make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			// no --chrome-path: the daemon resolves Chrome itself, which is the path a
			// TypeScript worker actually takes.
			done <- run(context.Background(), nil, stdoutWriter, stdinReader)
		}()
		// A daemon that died during startup makes every pipe operation below block forever.  Report
		// what it said rather than waiting on a reader that is never coming.
		daemonAlive := func(operation string, run func() error) {
			GinkgoHelper()
			finished := make(chan error, 1)
			go func() { finished <- run() }()
			select {
			case err := <-finished:
				Expect(err).NotTo(HaveOccurred())
			case err := <-done:
				Expect(err).NotTo(HaveOccurred(), "the daemon exited during %s instead of serving it", operation)
				Fail(fmt.Sprintf("the daemon returned before it could serve %s", operation))
			case <-ctx.Done():
				Fail(fmt.Sprintf("timed out waiting for the daemon to serve %s", operation))
			}
		}
		params, err := json.Marshal(protocol.HandshakeRequest{ProtocolVersion: protocol.Version})
		Expect(err).NotTo(HaveOccurred())
		daemonAlive("the handshake request", func() error {
			return protocol.NewFramedWriter(stdinWriter).Write(protocol.Request{ID: 1, Method: "handshake", Params: params})
		})
		var response protocol.Response
		daemonAlive("the handshake response", func() error {
			return protocol.NewFramedReader(stdoutReader).Read(&response)
		})
		Expect(response.ID).To(Equal(uint64(1)))
		Expect(response.Error).To(BeNil())
		Expect(stdinWriter.Close()).To(Succeed())
		Eventually(done, 5*time.Second).Should(Receive(Succeed()))
		Expect(stdoutWriter.Close()).To(Succeed())
		Expect(stdoutReader.Close()).To(Succeed())
		Expect(stdinReader.Close()).To(Succeed())
	}, SpecTimeout(90*time.Second))
})

func boolPointer(value bool) *bool { return &value }

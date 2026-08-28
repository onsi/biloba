package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	Describe("eventful binary wire bounds", func() {
		It("accepts the decoded boundary and rejects one byte beyond it", func() {
			atLimit := base64.StdEncoding.EncodeToString(make([]byte, protocol.MaxDecodedBodySize))
			body, err := decodeBoundedBody(atLimit)
			Expect(err).NotTo(HaveOccurred())
			Expect(body).To(HaveLen(int(protocol.MaxDecodedBodySize)))
			overLimit := base64.StdEncoding.EncodeToString(make([]byte, protocol.MaxDecodedBodySize+1))
			_, err = decodeBoundedBody(overLimit)
			Expect(err).To(MatchError(ContainSubstring("exceeds limit")))
		})
		It("rejects malformed base64", func() {
			_, err := decodeBoundedBody("%%%")
			Expect(err).To(MatchError(ContainSubstring("valid base64")))
		})
	})
	It("parses daemon flags", func() {
		parsed, err := parseConfig([]string{
			"-chrome-path", "/opt/chrome",
			"-chrome-ws-url", "ws://127.0.0.1:9222/devtools/browser/test",
			"-artifact-dir", "/tmp/artifacts",
			"-screenshot-baselines-dir", "/tmp/baselines",
			"-update-screenshots=true",
			"-screenshot-pixel-tolerance", "0.02",
			"-screenshot-channel-tolerance", "8",
			"-max-screenshot-bytes", "1024",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(parsed).To(Equal(config{
			chromePath: "/opt/chrome", chromeWSURL: "ws://127.0.0.1:9222/devtools/browser/test", artifactDir: "/tmp/artifacts",
			screenshotBaselinesDir: "/tmp/baselines", updateScreenshots: true, screenshotPixelTolerance: 0.02, screenshotChannelTolerance: 8, maxScreenshotBytes: 1024,
		}))
	})

	DescribeTable("rejects unsafe screenshot daemon bounds",
		func(arguments ...string) {
			_, err := parseConfig(arguments)
			Expect(err).To(HaveOccurred())
		},
		Entry("NaN pixel tolerance", "-screenshot-pixel-tolerance", "NaN"),
		Entry("positive infinite pixel tolerance", "-screenshot-pixel-tolerance", "+Inf"),
		Entry("negative infinite pixel tolerance", "-screenshot-pixel-tolerance", "-Inf"),
		Entry("negative pixel tolerance", "-screenshot-pixel-tolerance", "-0.01"),
		Entry("pixel tolerance above one", "-screenshot-pixel-tolerance", "1.01"),
		Entry("negative channel tolerance", "-screenshot-channel-tolerance", "-1"),
		Entry("channel tolerance above 255", "-screenshot-channel-tolerance", "256"),
		Entry("zero screenshot bound", "-max-screenshot-bytes", "0"),
		Entry("screenshot bound above the hard limit", "-max-screenshot-bytes", "16777217"),
	)

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

	It("adapts the complete protocol locator tree to the engine", func() {
		selector, err := selectorFromProtocol(protocol.Locator{
			Kind: protocol.LocatorCSS, Value: ".row", Nth: 1, NthSet: true,
			Within:  &protocol.Locator{Kind: protocol.LocatorTestID, Value: "results"},
			Filters: []protocol.LocatorFilter{{Kind: protocol.LocatorFilterContainsText, Value: "Ada", Match: protocol.MatchContains}},
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(selector.Encoded()).To(And(
			ContainSubstring(`"kind":"containsText"`),
			ContainSubstring(`"within":"a{\"attr\":\"data-testid\"`),
			ContainSubstring(`"nth":1`),
		))

		semantic, err := selectorFromProtocol(protocol.Locator{
			Kind: protocol.LocatorLabel, Value: "Email", Match: protocol.MatchExact,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(semantic.Encoded()).To(Equal(`a{"by":"label","value":"Email","valueMode":"exact"}`))
	})

	It("adapts XPath locators to the engine encoding", func() {
		selector, err := selectorFromProtocol(protocol.Locator{
			Kind: protocol.LocatorXPath, Value: `//button[text()="Save"]`,
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(selector.Encoded()).To(Equal(`x//button[text()="Save"]`))
	})

	It("adapts typed matcher trees and polling modes to the engine", func() {
		expectation, err := expectationFromProtocol(protocol.Expectation{
			Kind: protocol.ExpectAll,
			Children: []protocol.Expectation{
				{Kind: protocol.ExpectContains, ExpectedJSON: `"ready"`},
				{Kind: protocol.ExpectNot, Children: []protocol.Expectation{{Kind: protocol.ExpectEmpty}}},
			},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(expectation.Kind).To(Equal(engine.ExpectAll))
		Expect(expectation.Children).To(HaveLen(2))
		Expect(expectation.Children[0].Expected).To(Equal("ready"))

		policy := pollPolicyFromProtocol(protocol.PollPolicy{
			Mode: protocol.PollConsistently, Timeout: time.Second, Interval: time.Millisecond,
		})
		Expect(policy.Mode).To(Equal(engine.PollConsistently))
		Expect(policy.Timeout).To(Equal(time.Second))
	})

	It("uses the Go runner's ten-second default for polling actions", func() {
		policy := withDefaultPollTimeout(pollPolicyFromProtocol(protocol.PollPolicy{}))

		Expect(policy.Timeout).To(Equal(10 * time.Second))
	})

	It("describes typed expectations in failure diagnostics", func() {
		operation := protocol.Operation{Kind: protocol.OperationAssert, Assertion: protocol.Assertion{
			Kind: protocol.AssertionText,
			Expectation: protocol.Expectation{Kind: protocol.ExpectAll, Children: []protocol.Expectation{
				{Kind: protocol.ExpectContains, ExpectedJSON: `"ready"`},
				{Kind: protocol.ExpectNot, Children: []protocol.Expectation{{Kind: protocol.ExpectEmpty}}},
			}},
		}}

		Expect(expectedDescription(operation)).To(Equal(`contain "ready" and not empty`))
	})

	It("describes boolean DOM assertions in user-facing terms", func() {
		visible := protocol.Operation{Kind: protocol.OperationAssert, Assertion: protocol.Assertion{
			Kind:        protocol.AssertionVisible,
			Expectation: protocol.Expectation{Kind: protocol.ExpectEqual, ExpectedJSON: "true"},
		}}
		notExists := protocol.Operation{Kind: protocol.OperationAssert, Assertion: protocol.Assertion{
			Kind:        protocol.AssertionExists,
			Expectation: protocol.Expectation{Kind: protocol.ExpectEqual, ExpectedJSON: "false"},
		}}

		Expect(expectedDescription(visible)).To(Equal("visible"))
		Expect(expectedDescription(notExists)).To(Equal("not exist"))
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

	Describe("matching an observation against a wire expectation", func() {
		// The engine hands back Go types (engine.DocumentOrder, engine.Box, ...) while the expectation
		// arrives as decoded JSON.  reflect.DeepEqual is type-sensitive, so without rendering the
		// observation the way the client sees it, EQUAL against the exact value the matching read just
		// returned is false forever - the assertion can never pass, only time out.
		It("renders a named string type as the string the client sees", func() {
			matched, err := engine.MatchExpectation(jsonShape(engine.DocumentOrder("before")), engine.Expectation{Kind: engine.ExpectEqual, Expected: "before"})
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeTrue())
		})

		It("renders a geometry struct as the object the client sees", func() {
			box := engine.Box{Top: 1, Left: 2, Width: 3, Height: 4}
			encoded, err := json.Marshal(box)
			Expect(err).NotTo(HaveOccurred())
			var expected any
			Expect(json.Unmarshal(encoded, &expected)).To(Succeed())

			matched, err := engine.MatchExpectation(jsonShape(box), engine.Expectation{Kind: engine.ExpectEqual, Expected: expected})
			Expect(err).NotTo(HaveOccurred())
			Expect(matched).To(BeTrue(), "expectBoundingBox fed the value its own read returned must pass")
		})

		It("leaves values that are already in their JSON shape alone", func() {
			Expect(jsonShape(nil)).To(BeNil())
			for _, value := range []any{true, "text", 3.5, 7, []any{"a"}, map[string]any{"k": "v"}} {
				Expect(jsonShape(value)).To(Equal(value))
			}
		})
	})

	Describe("an assertion whose selector matched nothing", func() {
		// biloba.js's one() reports a missing element as an error precisely so a negated matcher cannot
		// pass against a selector that never matched.  Clearing that error whenever the expectation
		// happens to match the zero value throws the distinction away: ShouldNot(BeVisible()) against
		// #missing would pass instantly, and a poll for a not-yet-rendered value would return null.
		notFound := &engine.Error{Code: engine.CodeNotFound, Operation: "isVisible", Message: "could not find DOM element matching selector"}
		answeredNo := &engine.Error{Code: engine.CodeConditionNotMet, Operation: "isVisible", Message: "operation did not succeed"}

		It("keeps the error when the selector matched nothing", func() {
			Expect(clearedReadError(true, notFound)).To(MatchError(notFound))
		})

		It("clears the error when the handler ran and answered no", func() {
			// expectNotVisible against an element that exists but is hidden: "no" is the answer.
			Expect(clearedReadError(true, answeredNo)).NotTo(HaveOccurred())
		})

		It("keeps an error the element itself could not answer", func() {
			// isChecked on a <label> raises rather than answering false, so that expectNotChecked
			// cannot pass forever against an element that could never be checked.
			cannotAnswer := &engine.Error{Code: engine.CodeNotFound, Operation: "isChecked", Message: "DOM element does not have a checked property"}
			Expect(clearedReadError(true, cannotAnswer)).To(MatchError(cannotAnswer))
		})

		It("keeps a fatal error regardless", func() {
			fatal := engine.Fatal(answeredNo)
			Expect(clearedReadError(true, fatal)).To(MatchError(fatal))
		})

		It("keeps the error when the expectation did not match either", func() {
			Expect(clearedReadError(false, answeredNo)).To(MatchError(answeredNo))
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

	Describe("mapping engine error codes onto the protocol", func() {
		It("gives every engine code an explicit mapping", func() {
			// Falling through to DRIVER_ERROR is silent: the client is told the daemon broke for a
			// failure the page caused.  A code added to the engine has to be classified here.
			for code := range engineErrorCodesInSource() {
				Expect(engineProtocolCodes).To(HaveKey(code), "engine.ErrorCode %q has no protocol counterpart in engineProtocolCodes", code)
			}
		})

		It("maps nothing the engine no longer defines", func() {
			declared := engineErrorCodesInSource()
			for code := range engineProtocolCodes {
				Expect(declared).To(HaveKey(code), "engineProtocolCodes maps %q, which the engine does not define", code)
			}
		})

		DescribeTable("converting an engine failure", func(code engine.ErrorCode, expected protocol.ErrorCode) {
			converted := engineRPCError(&engine.Error{Code: code, Operation: "evaluate", Message: "boom"})
			var protocolErr *protocol.ProtocolError
			Expect(errors.As(converted, &protocolErr)).To(BeTrue())
			Expect(protocolErr.Code).To(Equal(expected))
			Expect(protocolErr.Message).To(Equal("evaluate: boom"))
		},
			Entry("a page-level JavaScript error", engine.CodeJavaScript, protocol.CodeJavaScript),
			Entry("a script that will never parse", engine.CodeInvalidScript, protocol.CodeJavaScript),
			Entry("a navigation that landed on the wrong status", engine.CodeNavigation, protocol.CodeNavigation),
			Entry("an action the target refused", engine.CodeActionFailed, protocol.CodeTargetNotReady),
			Entry("an argument the caller got wrong", engine.CodeInvalidArgument, protocol.CodeInvalidArgument),
			Entry("a browser that never started", engine.CodeBrowserStart, protocol.CodeDriver),
			Entry("an unrecognized code", engine.ErrorCode("brand_new"), protocol.CodeDriver),
		)
	})
})

// engineErrorCodesInSource reads the engine's own declarations rather than a list kept here, which
// would go stale in exactly the way the mapping did.
func engineErrorCodesInSource() map[engine.ErrorCode]string {
	GinkgoHelper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "engine", "*.go"))
	Expect(err).NotTo(HaveOccurred())
	pattern := regexp.MustCompile(`(?m)^\s*(Code\w+)\s+ErrorCode\s+=\s+"([a-z_]+)"`)
	codes := map[engine.ErrorCode]string{}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		source, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		for _, match := range pattern.FindAllStringSubmatch(string(source), -1) {
			codes[engine.ErrorCode(match[2])] = match[1]
		}
	}
	Expect(codes).NotTo(BeEmpty(), "could not find the engine's ErrorCode declarations")
	return codes
}

func boolPointer(value bool) *bool { return &value }

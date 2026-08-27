package protocol_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/onsi/biloba/protocol"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// The generated TypeScript is the contract the client codes against, but nothing has been checking
// that it describes what the daemon actually puts on the wire.  `omitempty` is the trap: it does
// nothing for a struct, so a field can be declared optional in TypeScript and be present in every
// single response (that is how "diagnostics":{} rode along on every successful operation).  The
// golden fixtures do not catch it either - protocol-golden.test.ts asserts with toMatchObject,
// which ignores extra keys.
//
// So: marshal each response type and compare the keys that actually appear against the keys the
// generated declaration says are required.
var _ = Describe("the generated protocol declarations", func() {
	var declarations map[string]tsInterface

	BeforeEach(func() {
		declarations = parseGeneratedTypeScript()
	})

	// Only the types the daemon marshals: for request types Go is the decoder, so what Go would
	// emit says nothing about what the client sends.
	DescribeTable("describes what the daemon emits for a zero value",
		func(name string, value any) {
			declaration, found := declarations[name]
			Expect(found).To(BeTrue(), "the generator no longer emits an interface named %s", name)

			emitted := marshalledKeys(value)
			for field, optional := range declaration.fields {
				if optional {
					Expect(emitted).NotTo(ContainElement(field),
						"%s.%s is declared optional in TypeScript but the daemon emits it even for a zero value - either give the Go field a working omitempty (a struct needs to become a pointer) or make the declaration required", name, field)
					continue
				}
				Expect(emitted).To(ContainElement(field),
					"%s.%s is declared required in TypeScript but the daemon does not emit it for a zero value - a client reading it would get undefined", name, field)
			}
			for _, field := range emitted {
				_, declared := declaration.fields[field]
				Expect(declared).To(BeTrue(), "the daemon emits %s.%s but the generated declaration has no such field", name, field)
			}
		},
		Entry("Diagnostics", "Diagnostics", protocol.Diagnostics{}),
		Entry("ProtocolError", "ProtocolError", protocol.ProtocolError{}),
		Entry("HandshakeResponse", "HandshakeResponse", protocol.HandshakeResponse{}),
		Entry("OpenSessionResponse", "OpenSessionResponse", protocol.OpenSessionResponse{}),
		Entry("OperationResult", "OperationResult", protocol.OperationResult{}),
		Entry("PollObservation", "PollObservation", protocol.PollObservation{}),
		Entry("Timings", "Timings", protocol.Timings{}),
	)

	It("keeps a fully-populated operation result inside its declaration", func() {
		// The zero-value pass catches fields that are always present; this catches the opposite
		// mistake - a field that only shows up once something is set, under a name nobody generated.
		diagnostics := protocol.Diagnostics{Locator: `locator("#save")`, Expected: "visible", DOMOutline: "body", ScreenshotPath: "/tmp/x.png", DaemonDetail: "timed out"}
		populated := protocol.OperationResult{
			Matched: true, ObservedJSON: `"Saved"`, AttemptCount: 2,
			Trajectory:  []protocol.PollObservation{{Attempt: 1, ElapsedMS: 5, ObservedJSON: `"Saving"`, RetryReason: "text mismatch"}},
			Timings:     protocol.Timings{StartedUnixMS: 1, ElapsedMS: 2},
			Diagnostics: &diagnostics, RPCRequestCount: 1, RPCResponseCount: 1,
		}

		Expect(marshalledKeys(populated)).To(ConsistOf(keysOf(declarations["OperationResult"])))
		Expect(marshalledKeys(populated.Trajectory[0])).To(ConsistOf(keysOf(declarations["PollObservation"])))
		Expect(marshalledKeys(diagnostics)).To(ConsistOf(keysOf(declarations["Diagnostics"])))
	})

	It("only omits diagnostics when there are none to report", func() {
		// The reason diagnostics is a pointer rather than a value: a failed operation has to carry
		// its outline and screenshot across, and a successful one should not pay for an empty object.
		bare := marshalledKeys(protocol.OperationResult{Matched: true, Timings: protocol.Timings{}})
		Expect(bare).NotTo(ContainElement("diagnostics"))

		diagnostics := protocol.Diagnostics{DOMOutline: "body"}
		reported := marshalledKeys(protocol.OperationResult{Diagnostics: &diagnostics})
		Expect(reported).To(ContainElement("diagnostics"))
	})

	It("declares every error code the server can produce", func() {
		declared := generatedErrorCodes()

		Expect(declared).To(ConsistOf(
			string(protocol.CodeInvalidArgument), string(protocol.CodeTimeout), string(protocol.CodeTargetNotFound),
			string(protocol.CodeTargetNotReady), string(protocol.CodeJavaScript), string(protocol.CodeProtocolMismatch),
			string(protocol.CodeDriverClosed), string(protocol.CodeDriver), string(protocol.CodeCancelled),
			string(protocol.CodeBrowserGone), string(protocol.CodePageCrashed),
		), "a code the daemon can send but TypeScript does not declare is a code the client cannot narrow on")
	})

	It("encodes durations as whole milliseconds, not Go nanoseconds", func() {
		// time.Duration marshals as an int64 nanosecond count by default, which would silently make
		// every elapsedMs 1e6 times too large on the TypeScript side.
		wire, err := json.Marshal(protocol.Timings{ElapsedMS: (1500 * time.Millisecond).Milliseconds()})
		Expect(err).NotTo(HaveOccurred())
		Expect(string(wire)).To(ContainSubstring(`"elapsedMs":1500`))
	})
})

type tsInterface struct {
	fields map[string]bool // field name -> optional
}

var (
	tsInterfacePattern = regexp.MustCompile(`(?m)^export interface (\w+)(?:<[^>]*>)? \{$`)
	tsFieldPattern     = regexp.MustCompile(`^\s{2}(\w+)(\??):`)
)

// parseGeneratedTypeScript reads the committed generated declarations rather than re-deriving them
// from the generator, so a bug in the generator shows up here instead of cancelling itself out.
func parseGeneratedTypeScript() map[string]tsInterface {
	GinkgoHelper()
	source, err := os.ReadFile("../typescript/src/generated/protocol.ts")
	Expect(err).NotTo(HaveOccurred())
	declarations := map[string]tsInterface{}
	var current string
	for _, line := range strings.Split(string(source), "\n") {
		if match := tsInterfacePattern.FindStringSubmatch(line); match != nil {
			current = match[1]
			declarations[current] = tsInterface{fields: map[string]bool{}}
			continue
		}
		if line == "}" {
			current = ""
			continue
		}
		if current == "" {
			continue
		}
		if match := tsFieldPattern.FindStringSubmatch(line); match != nil {
			declarations[current].fields[match[1]] = match[2] == "?"
		}
	}
	Expect(declarations).NotTo(BeEmpty(), "could not parse the generated declarations")
	return declarations
}

func generatedErrorCodes() []string {
	GinkgoHelper()
	source, err := os.ReadFile("../typescript/src/generated/protocol.ts")
	Expect(err).NotTo(HaveOccurred())
	_, rest, found := strings.Cut(string(source), "export type ErrorCode =")
	Expect(found).To(BeTrue())
	body, _, found := strings.Cut(rest, ";")
	Expect(found).To(BeTrue())
	codes := []string{}
	for _, match := range regexp.MustCompile(`"([A-Z_]+)"`).FindAllStringSubmatch(body, -1) {
		codes = append(codes, match[1])
	}
	return codes
}

func marshalledKeys(value any) []string {
	GinkgoHelper()
	encoded, err := json.Marshal(value)
	Expect(err).NotTo(HaveOccurred())
	var decoded map[string]json.RawMessage
	Expect(json.Unmarshal(encoded, &decoded)).To(Succeed())
	keys := []string{}
	for key := range decoded {
		keys = append(keys, key)
	}
	return keys
}

func keysOf(declaration tsInterface) []string {
	keys := []string{}
	for key := range declaration.fields {
		keys = append(keys, key)
	}
	return keys
}

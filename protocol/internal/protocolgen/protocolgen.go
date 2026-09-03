// Package protocolgen renders the generated protocol artifacts from the Go wire definitions.
// It is a package rather than a lone main so the same rendering that `go generate ./protocol`
// writes can be compared against what is on disk by a spec - see protocol/generated_test.go.
// A drift check that shells out to git answers "does the tree match the last commit?"; the
// question worth asking is "do these files match the Go source?", and only the generator can
// answer that.
package protocolgen

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/onsi/biloba/protocol"
)

// Files maps each generated artifact's repository-relative path to its rendered contents.
func Files() map[string][]byte {
	return map[string][]byte{
		"typescript/src/generated/protocol.ts": []byte(typeScript()),
		"protocol/testdata/golden/handshake-request.json": indentJSON(protocol.Request{
			ID: 1, Method: "handshake", Params: raw(protocol.HandshakeRequest{ProtocolVersion: protocol.Version}),
		}),
		"protocol/testdata/golden/protocol-error-response.json": indentJSON(protocol.Response{
			ID: 7, Error: protocol.NewError(protocol.CodeProtocolMismatch, "protocol version mismatch"),
		}),
		"protocol/testdata/golden/operation-response.json": indentJSON(protocol.Response{ID: 9, Result: protocol.OperationResult{
			Matched: true, ObservedJSON: `"Saved"`, AttemptCount: 2,
			Trajectory: []protocol.PollObservation{{Attempt: 1, ObservedJSON: `"Saving"`}, {Attempt: 2, ElapsedMS: 10, ObservedJSON: `"Saved"`}},
			Timings:    protocol.Timings{StartedUnixMS: 1_700_000_000_000, ElapsedMS: 10}, RPCRequestCount: 1, RPCResponseCount: 1,
		}}),
	}
}

func indentJSON(value any) []byte {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(encoded, '\n')
}

var declarations = []reflect.Type{
	reflect.TypeFor[protocol.Diagnostics](),
	reflect.TypeFor[protocol.ProtocolError](),
	reflect.TypeFor[protocol.Request](),
	reflect.TypeFor[protocol.HandshakeRequest](),
	reflect.TypeFor[protocol.HandshakeResponse](),
	reflect.TypeFor[protocol.OpenSessionResponse](),
	reflect.TypeFor[protocol.SessionRequest](),
	reflect.TypeFor[protocol.NavigateRequest](),
	reflect.TypeFor[protocol.PollOptions](),
	reflect.TypeFor[protocol.WireLocator](),
	reflect.TypeFor[protocol.WireCookie](),
	reflect.TypeFor[protocol.SetCookiesRequest](),
	reflect.TypeFor[protocol.LocatorRequest](),
	reflect.TypeFor[protocol.SetValueRequest](),
	reflect.TypeFor[protocol.EvaluateRequest](),
	reflect.TypeFor[protocol.WireAssertion](),
	reflect.TypeFor[protocol.AssertRequest](),
	reflect.TypeFor[protocol.PollObservation](),
	reflect.TypeFor[protocol.Timings](),
	reflect.TypeFor[protocol.OperationResult](),
}

func typeScript() string {
	var output strings.Builder
	output.WriteString("// Code generated from the Go protocol definition; DO NOT EDIT.\n\n")
	output.WriteString("export type ErrorCode =\n")
	for _, code := range []protocol.ErrorCode{protocol.CodeInvalidArgument, protocol.CodeTimeout, protocol.CodeTargetNotFound, protocol.CodeTargetNotReady, protocol.CodeJavaScript, protocol.CodeProtocolMismatch, protocol.CodeDriverClosed, protocol.CodeDriver, protocol.CodeCancelled, protocol.CodeBrowserGone, protocol.CodePageCrashed} {
		fmt.Fprintf(&output, "  | %q\n", code)
	}
	output.WriteString(";\n\n")
	for _, declaration := range declarations {
		writeDeclaration(&output, declaration)
	}
	output.WriteString("export interface Response<Result = unknown> {\n  id: number;\n  result?: Result;\n  error?: ProtocolError;\n}\n\n")
	output.WriteString("export type OpenSessionRequest = Record<string, never>;\n")
	return output.String()
}

func writeDeclaration(output *strings.Builder, declaration reflect.Type) {
	name := tsName(declaration)
	fmt.Fprintf(output, "export interface %s {\n", name)
	for i := range declaration.NumField() {
		field := declaration.Field(i)
		jsonName, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if jsonName == "-" {
			continue
		}
		if jsonName == "" {
			jsonName = field.Name
		}
		optional := strings.Contains(options, "omitempty") || field.Type.Kind() == reflect.Pointer
		fmt.Fprintf(output, "  %s%s: %s;\n", jsonName, map[bool]string{true: "?"}[optional], tsType(declaration, field))
	}
	output.WriteString("}\n\n")
}

func tsType(owner reflect.Type, field reflect.StructField) string {
	if owner == reflect.TypeFor[protocol.WireLocator]() {
		switch field.Name {
		case "Kind":
			return `"CSS" | "TEST_ID" | "TEXT" | "ROLE"`
		case "Match":
			return `"EXACT" | "CONTAINS"`
		}
	}
	if owner == reflect.TypeFor[protocol.WireAssertion]() {
		switch field.Name {
		case "Kind":
			return `"VISIBLE" | "TEXT" | "COUNT" | "ATTRIBUTE" | "VALUE" | "URL" | "EVALUATE"`
		case "Match":
			return `"EXACT" | "CONTAINS"`
		}
	}
	return typeName(field.Type)
}

func typeName(value reflect.Type) string {
	if value.Kind() == reflect.Pointer {
		return typeName(value.Elem())
	}
	if value == reflect.TypeFor[json.RawMessage]() || value.Kind() == reflect.Interface {
		return "unknown"
	}
	if value == reflect.TypeFor[protocol.ErrorCode]() {
		return "ErrorCode"
	}
	switch value.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice:
		return typeName(value.Elem()) + "[]"
	case reflect.Struct:
		return tsName(value)
	default:
		panic("unsupported protocol type " + value.String())
	}
}

func tsName(value reflect.Type) string {
	return strings.TrimPrefix(value.Name(), "Wire")
}

func raw(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

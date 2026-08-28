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
	reflect.TypeFor[protocol.TabQueryRequest](),
	reflect.TypeFor[protocol.ListHandlesRequest](),
	reflect.TypeFor[protocol.WaitForTabRequest](),
	reflect.TypeFor[protocol.WaitForFrameRequest](),
	reflect.TypeFor[protocol.HandleListResponse](),
	reflect.TypeFor[protocol.InvalidationResponse](),
	reflect.TypeFor[protocol.NavigateRequest](),
	reflect.TypeFor[protocol.PollOptions](),
	reflect.TypeFor[protocol.WireLocator](),
	reflect.TypeFor[protocol.WireLocatorFilter](),
	reflect.TypeFor[protocol.WireNameSpec](),
	reflect.TypeFor[protocol.WirePoint](),
	reflect.TypeFor[protocol.WireDOMOperation](),
	reflect.TypeFor[protocol.DOMRequest](),
	reflect.TypeFor[protocol.WireCookie](),
	reflect.TypeFor[protocol.SetCookiesRequest](),
	reflect.TypeFor[protocol.WireCookieQuery](),
	reflect.TypeFor[protocol.WireDeviceMetrics](),
	reflect.TypeFor[protocol.WireGeolocation](),
	reflect.TypeFor[protocol.WireMedia](),
	reflect.TypeFor[protocol.WireLifecycleOperation](),
	reflect.TypeFor[protocol.LifecycleRequest](),
	reflect.TypeFor[protocol.CookieListResponse](),
	reflect.TypeFor[protocol.LocatorRequest](),
	reflect.TypeFor[protocol.SetValueRequest](),
	reflect.TypeFor[protocol.TypeRequest](),
	reflect.TypeFor[protocol.SendKeysRequest](),
	reflect.TypeFor[protocol.SetWindowSizeRequest](),
	reflect.TypeFor[protocol.SetUploadRequest](),
	reflect.TypeFor[protocol.DragToRequest](),
	reflect.TypeFor[protocol.AddInitScriptRequest](),
	reflect.TypeFor[protocol.HoldResponseRequest](),
	reflect.TypeFor[protocol.ResponseHoldRequest](),
	reflect.TypeFor[protocol.EvaluateRequest](),
	reflect.TypeFor[protocol.WireExpectation](),
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
	for _, code := range []protocol.ErrorCode{protocol.CodeInvalidArgument, protocol.CodeTimeout, protocol.CodeTargetNotFound, protocol.CodeTargetNotReady, protocol.CodeNavigation, protocol.CodeJavaScript, protocol.CodeProtocolMismatch, protocol.CodeDriverClosed, protocol.CodeDriver, protocol.CodeCancelled, protocol.CodeBrowserGone, protocol.CodePageCrashed} {
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
			return `"CSS" | "XPATH" | "TEST_ID" | "TEXT" | "ROLE" | "LABEL" | "PLACEHOLDER" | "ALT_TEXT" | "TITLE" | "AND" | "OR"`
		case "Match":
			return `"EXACT" | "CONTAINS"`
		}
	}
	if owner == reflect.TypeFor[protocol.WireLocatorFilter]() {
		switch field.Name {
		case "Kind":
			return `"CONTAINS_TEXT" | "CONTAINS" | "WITHIN"`
		case "Match":
			return `"EXACT" | "CONTAINS"`
		}
	}
	if owner == reflect.TypeFor[protocol.WireDOMOperation]() {
		switch field.Name {
		case "Kind":
			return `"TEXT" | "TEXTS" | "CLASSES" | "CLASSES_FOR_EACH" | "DISTINCT_ATTRIBUTE_COUNT" | "ATTRIBUTES" | "ATTRIBUTES_FOR_EACH" | "JSON_ATTRIBUTE" | "PROPERTIES" | "PROPERTIES_FOR_EACH" | "PROPERTY_FOR_EACH" | "VALUES" | "STATE" | "ALL_STATE" | "SET_PROPERTY" | "FOCUS" | "BLUR" | "HOVER" | "TYPE" | "SEND_KEYS" | "CLICK" | "CLICK_EACH" | "TAP" | "DRAG" | "SCROLL_INTO_VIEW" | "SCROLL_WHEEL" | "SELECT" | "CLEAR_SELECTION" | "INVOKE_METHOD" | "INVOKE_FUNCTION" | "INVOKE_METHOD_FOR_EACH" | "INVOKE_FUNCTION_FOR_EACH" | "BOUNDING_BOX" | "SCROLL_OFFSET" | "OFFSET_WITHIN" | "RELATIVE_BOXES" | "GEOMETRY_RELATION" | "GAP_BETWEEN" | "IN_VIEWPORT" | "DOCUMENT_ORDER" | "COMPUTED_STYLE" | "COMPUTED_STYLE_NUMBER" | "NORMALIZE_COLOR"`
		case "TextMode":
			return `"INNER_TEXT" | "TEXT_CONTENT" | "NORMALIZED_TEXT"`
		case "Button":
			return `"left" | "right" | "middle"`
		case "Modifiers":
			return `("Shift" | "Control" | "Alt" | "Meta")[]`
		case "State":
			return `"visible" | "enabled" | "clickable" | "checked" | "focused"`
		case "Relation":
			return `"above" | "below" | "leftOf" | "rightOf" | "encloses" | "overlaps"`
		}
	}
	if owner == reflect.TypeFor[protocol.WireAssertion]() {
		switch field.Name {
		case "Kind":
			return `"VISIBLE" | "TEXT" | "COUNT" | "ATTRIBUTE" | "VALUE" | "URL" | "EVALUATE" | "EXISTS" | "ENABLED" | "CLICKABLE" | "PROPERTY" | "ALL_TEXT" | "REQUEST"`
		case "Match":
			return `"EXACT" | "CONTAINS"`
		}
	}
	if owner == reflect.TypeFor[protocol.WireLifecycleOperation]() && field.Name == "Kind" {
		return `"GET_COOKIES" | "CLEAR_COOKIES" | "COOKIE_QUERY" | "STORAGE_SET" | "STORAGE_GET" | "STORAGE_GET_ALL" | "STORAGE_REMOVE" | "STORAGE_CLEAR" | "STORAGE_LENGTH" | "WAIT_FOR_DEFINED" | "URL" | "TITLE" | "WINDOW_SIZE" | "OUTLINE" | "ACCESSIBILITY_OUTLINE" | "CONSOLE_MESSAGES" | "SET_DEVICE_METRICS" | "CLEAR_DEVICE_METRICS" | "SET_GEOLOCATION" | "CLEAR_GEOLOCATION" | "SET_PERMISSIONS" | "RESET_PERMISSIONS" | "SET_LOCALE" | "CLEAR_LOCALE" | "SET_TIMEZONE" | "CLEAR_TIMEZONE" | "SET_MEDIA" | "CLEAR_MEDIA"`
	}
	if owner == reflect.TypeFor[protocol.PollOptions]() && field.Name == "Mode" {
		return `"EVENTUALLY" | "IMMEDIATE" | "CONSISTENTLY"`
	}
	if owner == reflect.TypeFor[protocol.WireExpectation]() {
		switch field.Name {
		case "Kind":
			return `"EQUAL" | "CONTAINS" | "REGEXP" | "PREFIX" | "SUFFIX" | "NUMBER" | "EMPTY" | "ALL" | "ANY" | "NOT" | "ANYTHING"`
		case "Operator":
			return `"=" | "==" | "!=" | ">" | ">=" | "<" | "<="`
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
	case reflect.Map:
		return "Record<" + typeName(value.Key()) + ", " + typeName(value.Elem()) + ">"
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

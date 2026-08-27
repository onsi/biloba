package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// HandlerResponse is the typed wire-neutral result returned by a biloba.js atomic handler.
type HandlerResponse struct {
	Success bool   `json:"success"`
	Err     string `json:"error"`
	Result  any    `json:"result"`
	Found   *bool  `json:"found"`
}

// EvaluateContext evaluates JavaScript against an attached chromedp target without changing its executor.
func EvaluateContext(ctx context.Context, script string, awaitPromise bool, result any) error {
	encoded, err := EvaluateRawContext(ctx, script, awaitPromise)
	if err != nil {
		return err
	}
	if result == nil || len(encoded) == 0 {
		return nil
	}
	return json.Unmarshal(encoded, result)
}

// EvaluateRawContext evaluates JavaScript and returns its JSON encoding; undefined is empty bytes.
func EvaluateRawContext(ctx context.Context, script string, awaitPromise bool) ([]byte, error) {
	var encoded []byte
	err := chromedp.Run(ctx, chromedp.EvaluateAsDevTools(script, &encoded, func(params *runtime.EvaluateParams) *runtime.EvaluateParams {
		params = params.WithUserGesture(true)
		if awaitPromise {
			params = params.WithAwaitPromise(true)
		}
		return params
	}))
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// RawJSArg is an argument that must land in a generated call as a raw JavaScript expression
// rather than as a JSON literal - biloba's JSVar is the implementation.  It marshals to a
// unique placeholder string, which EncodeArgs then swaps for the expression, so `app.count`
// is evaluated by the page instead of passed as the string "app.count".
type RawJSArg interface {
	// RawJSPlaceholder is the JSON-encoded token this argument marshals to.
	RawJSPlaceholder() string
	// RawJSExpression is the JavaScript source to substitute for that token.
	RawJSExpression() string
}

// EncodeArgs JSON-encodes an argument list into the `[...]` a spread call takes, honoring any
// RawJSArg in the list.  Every path that generates a JavaScript call from Go arguments goes
// through here - the biloba.js handler calls below and the runner's own JSFunc.Invoke - so an
// argument encodes identically no matter which path carries it.
func EncodeArgs(args ...any) (string, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	out := string(encoded)
	for _, arg := range args {
		if raw, ok := arg.(RawJSArg); ok {
			// each placeholder is unique, so replacing the first occurrence replaces the right one
			out = strings.Replace(out, raw.RawJSPlaceholder(), raw.RawJSExpression(), 1)
		}
	}
	return out, nil
}

// RunHandlerContext invokes one existing biloba.js handler atomically with an encoded selector.
func RunHandlerContext(ctx context.Context, name string, encodedSelector string, args ...any) (HandlerResponse, error) {
	return runHandlerContext(ctx, name, encodedSelector, false, args...)
}

// RunHandlerAsyncContext invokes a biloba.js handler that returns a Promise.
func RunHandlerAsyncContext(ctx context.Context, name string, encodedSelector string, args ...any) (HandlerResponse, error) {
	return runHandlerContext(ctx, name, encodedSelector, true, args...)
}

func runHandlerContext(ctx context.Context, name string, encodedSelector string, awaitPromise bool, args ...any) (HandlerResponse, error) {
	parameters := append([]any{encodedSelector}, args...)
	if encodedSelector == "" {
		parameters = append([]any{}, args...)
	}
	encoded, err := EncodeArgs(parameters...)
	if err != nil {
		return HandlerResponse{}, err
	}
	var response HandlerResponse
	err = EvaluateContext(ctx, fmt.Sprintf("_biloba.%s(...%s)", name, encoded), awaitPromise, &response)
	return response, err
}

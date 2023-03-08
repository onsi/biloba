package biloba

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

func (b *Biloba) EvaluateTo(expected any) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(script string) (bool, error) {
		r, err := b.RunErr(script)
		if err != nil {
			return false, fmt.Errorf("Failed to run script:\n%s\n\n%w", script, err)
		}
		data["Result"] = r
		return matcher.Match(data["Result"])
	}).WithTemplate("Return value for script:\n{{.Actual}}\nFailed with:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

func (b *Biloba) RunErr(script string, args ...any) (any, error) {
	b.blockIfNecessaryToEnsureSuccessfulDownloads()
	var encodedResult []byte
	withUserGesture := func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		return p.WithUserGesture(true)
	}
	err := chromedp.Run(b.Context, chromedp.EvaluateAsDevTools(script, &encodedResult, withUserGesture))
	if err != nil {
		return nil, err
	}

	if len(args) == 0 {
		var result any
		json.Unmarshal(encodedResult, &result)
		return result, nil
	}

	err = json.Unmarshal(encodedResult, args[0])
	return args[0], err
}

func (b *Biloba) Run(script string, args ...any) any {
	b.gt.Helper()
	res, err := b.RunErr(script, args...)
	if err != nil {
		b.gt.Fatalf("Failed to run script:\n%s\n\n%s", script, err.Error())
	}
	return res
}

type JSFunc string

func (b *Biloba) JSFunc(f string) JSFunc {
	return JSFunc("(" + f + ")")
}

func (j JSFunc) Invoke(args ...any) string {
	if len(args) == 0 {
		return string(j) + "()"
	}

	encodedArgsBytes, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	encodedArgs := string(encodedArgsBytes)
	for _, arg := range args {
		if v, ok := arg.(JSVar); ok {
			encodedArgs = v.interpolate(encodedArgs)
		}
	}

	return string(j) + "(..." + string(encodedArgs) + ")"
}

func (b *Biloba) JSVar(v string) JSVar {
	return JSVar{
		v:          v,
		identifier: fmt.Sprintf(`"__biloba_var_%d"`, atomic.AddInt64(&jsVarCounter, 1)),
	}
}

var jsVarCounter int64

type JSVar struct {
	v          string
	identifier string
}

func (j JSVar) MarshalJSON() ([]byte, error)   { return []byte(j.identifier), nil }
func (j JSVar) interpolate(json string) string { return strings.Replace(json, j.identifier, j.v, 1) }

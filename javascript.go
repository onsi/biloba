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

/*
EvaluateTo is a matcher that asserts that the result of running the script passed to Gomega matches expected:

	Eventually("app.users.map(user => user.name)").Should(tab.EvaluateTo(ConsistOf("George", "Sally", "Bob")))

EvaluateTo can be passed a Gomega matcher to assert against the returned value from the script.  Or it can be passed an arbitrary value in which case Equal() is used.

Read https://onsi.github.io/biloba/#running-arbitrary-javascript to learn more about running JavaScript in Biloba
*/
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

/*
RunErr() runs the passed in script and returns the result as well as an error

You should generally use [Biloba.Run] instead of RunErr and let Biloba handle errors for you

Read https://onsi.github.io/biloba/#running-arbitrary-javascript to learn more about running JavaScript in Biloba
*/
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

/*
Run() runs the passed in script and returns the result (as type any):

	tab.Run("1+3") // returns 4.0

You can also pass a single pointer argument if you would like Biloba to decode the result into a specific type (a la json.Unmarshal):

	var result int
	tab.Run("1+3", &result) // result is now 4

# If an error occurs Run() will fail the spec

Read https://onsi.github.io/biloba/#running-arbitrary-javascript to learn more about running JavaScript in Biloba
*/
func (b *Biloba) Run(script string, args ...any) any {
	b.gt.Helper()
	res, err := b.RunErr(script, args...)
	if err != nil {
		b.gt.Fatalf("Failed to run script:\n%s\n\n%s", script, err.Error())
	}
	return res
}

type JSFunc string

/*
JSFunc() allows you to write Javascript functions that are invoked with Go arguments and then passed in to Run:

	adder := b.JSFunc("(...nums) => nums.reduce((s, n) => s + n, 0)")
	var result int
	b.Run(adder.Invoke(1, 2, 3, 4, 5, 10), &result)

Read https://onsi.github.io/biloba/#running-arbitrary-javascript to learn more about running JavaScript in Biloba
*/
func (b *Biloba) JSFunc(f string) JSFunc {
	return JSFunc("(" + f + ")")
}

/*
Invoke() interpolates the passed-in args into the JSFunc and generates a script that cna be passed to Run()

	adder := b.JSFunc("(...nums) => nums.reduce((s, n) => s + n, 0)")
	var result int
	tab.Run(adder.Invoke(1, 2, 3, 4, 5, 10), &result)

Read https://onsi.github.io/biloba/#running-arbitrary-javascript to learn more about running JavaScript in Biloba
*/
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

/*
JSVar() allows you to indicate that the wrapped variable should be not be interpolated by Invoke(), instead it should be provided by the JavaScript environment and not Go.

This is perhaps best understood with an example:

	adder := tab.JSFunc("(...nums) => nums.reduce((s, n) => s + n, 0)")
	tab.Run(adder.Invoke(15, 10, tab.JSVar("app.numRecords"), tab.JSVar("app.numUsers + 10")))

This call to invoke will generate the following literal script:

	((...nums) => nums.reduce((s, n) => s + n, 0))(...[15, 10, app.numRecords, app.numUsers + 10])

which, when eval()'d in JavaScript will pull in the app variable from the global window object and grab the numRecords and numUsers properties.

If were to not wrap these variables in JSVar:

	tab.Run(adder.Invoke(15, 10, "app.numRecords", "app.numUsers + 10")) //wrong!

then the generated script would be:

	//not what you want!
	((...nums) => nums.reduce((s, n) => s + n, 0))(...[15, 10, "app.numRecords", "app.numUsers + 10"])

which would (of course) evaluate to "25app.numRecordsapp.numUsers + 10".  (of course).

Read https://onsi.github.io/biloba/#running-arbitrary-javascript to learn more about running JavaScript in Biloba
*/
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

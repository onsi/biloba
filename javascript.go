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

Note: the returned value is JSON-decoded, so JavaScript numbers come back as float64.  b.EvaluateTo(1) (an int) will therefore fail against a returned float64(1) - prefer a numeric matcher like BeNumerically("==", 1).

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
	return b.runErr(script, false, args...)
}

func (b *Biloba) runErr(script string, awaitPromise bool, args ...any) (any, error) {
	b.blockIfNecessaryToEnsureSuccessfulDownloads()
	var encodedResult []byte
	options := func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
		p = p.WithUserGesture(true)
		if awaitPromise {
			p = p.WithAwaitPromise(true)
		}
		return p
	}
	err := chromedp.Run(b.Context, chromedp.EvaluateAsDevTools(script, &encodedResult, options))
	if err != nil {
		if strings.Contains(err.Error(), "_biloba is not defined") {
			b.reloadBiloba()
			return b.runErr(script, awaitPromise, args...)
		}
		return nil, err
	}

	// A nil decode target means "discard the result" - decode into a throwaway any
	// and return it, just as the no-arg form does.  This lets `b.Run(script, nil)`
	// work for side-effect-only scripts instead of failing with `json: Unmarshal(nil)`.
	if len(args) == 0 || args[0] == nil {
		var result any
		json.Unmarshal(encodedResult, &result)
		return result, nil
	}

	// An undefined JS result decodes to empty bytes; unmarshaling that into a real
	// pointer yields a cryptic `unexpected end of JSON input`.  Give a directive error
	// instead - the usual cause is a side-effect-only script that forgot to `return`.
	if len(encodedResult) == 0 {
		return nil, fmt.Errorf("the script returned undefined, so there is nothing to decode into the pointer you provided.\nIf this script runs purely for its side effects, omit the decode target (or pass nil).\nOtherwise make sure the script returns a JSON-serializable value (e.g. `return true`).")
	}

	err = json.Unmarshal(encodedResult, args[0])
	return args[0], err
}

/*
RunErrAsync() runs the passed in script as the body of an async function and awaits the result, returning the result as well as an error

Use await freely and return the value you want out of the script:

	b.RunErrAsync(`return await app.load()`)

You should generally use [Biloba.RunAsync] instead of RunErrAsync and let Biloba handle errors for you

Read https://onsi.github.io/biloba/#running-arbitrary-javascript to learn more about running JavaScript in Biloba
*/
func (b *Biloba) RunErrAsync(script string, args ...any) (any, error) {
	return b.runErr("(async () => {"+script+"\n})()", true, args...)
}

/*
Run() runs the passed in script and returns the result (as type any):

	tab.Run("1+3") // returns 4.0

You can also pass a single pointer argument if you would like Biloba to decode the result into a specific type (a la json.Unmarshal):

	var result int
	tab.Run("1+3", &result) // result is now 4

Note: the result is JSON-decoded, so a returned number comes back as a float64 when you don't pass a typed pointer.  tab.Run("1+3") returns float64(4), which will not Equal(4) (an int) - use BeNumerically("==", 4) or decode into a typed pointer as above.

For a side-effect-only script you don't need a decode target at all - just omit it (or pass nil): tab.Run("app.redraw()").  If you do pass a non-nil pointer the script must return a JSON-serializable value, otherwise Run fails with a directive error.

# If an error occurs Run() will fail the spec

Run does not poll: a thrown error is usually a real bug, not a not-ready condition, so auto-polling would mask it.  For a polling path use [Biloba.RunErr] + Eventually, or [Biloba.EvaluateTo].  Configuring Run (WithTimeout/WithPolling/WithContext/Immediate) is a hard error.

Read https://onsi.github.io/biloba/#running-arbitrary-javascript to learn more about running JavaScript in Biloba
*/
func (b *Biloba) Run(script string, args ...any) any {
	b.gt.Helper()
	b.guardConfig("Run")
	res := b.run(script, args...)
	b.recordProbe("Run "+script, res)
	return res
}

// run is the unguarded substrate behind Run.  Internal callers (e.g. reloadBiloba on the polling hot
// path, the Storage and WindowSize helpers) use it directly so the public Run's config guard does not
// fire for Biloba's own internal scripting - only for a user who explicitly misconfigures a Run call.
func (b *Biloba) run(script string, args ...any) any {
	b.gt.Helper()
	res, err := b.RunErr(script, args...)
	if err != nil {
		b.gt.Fatalf("Failed to run script:\n%s\n\n%s%s", script, err.Error(), illegalReturnHint(err))
	}
	return res
}

// illegalReturnHint returns a hint to append to a Run failure when the script used a top-level return.
// Run evaluates a synchronous expression (Runtime.evaluate), so a top-level `return` is a syntax error;
// RunAsync wraps the script in a function body where `return` is allowed.
func illegalReturnHint(err error) string {
	if err == nil || !strings.Contains(err.Error(), "Illegal return statement") {
		return ""
	}
	return "\n\nHint: Run evaluates a synchronous expression, so a top-level `return` is not allowed.  Use b.RunAsync (which wraps your script in a function body) or wrap the script in an IIFE: `(() => { ... })()`."
}

/*
RunAsync() runs the passed in script as the body of an async function, awaits the result, and returns it.

Unlike [Biloba.Run] (which evaluates a synchronous expression) RunAsync lets you use await and return the value you care about:

	users := b.RunAsync(`
		const response = await fetch("/api/users")
		return await response.json()
	`)

As with Run you can pass a single pointer argument to decode the result into a specific type:

	var users []User
	b.RunAsync(`return await app.load()`, &users)

# If the script throws or the awaited promise rejects RunAsync will fail the spec

Like [Biloba.Run], RunAsync does not poll and configuring it (WithTimeout/WithPolling/WithContext/Immediate) is a hard error; for a polling path use [Biloba.RunErrAsync] + Eventually.

Read https://onsi.github.io/biloba/#running-arbitrary-javascript to learn more about running JavaScript in Biloba
*/
func (b *Biloba) RunAsync(script string, args ...any) any {
	b.gt.Helper()
	b.guardConfig("RunAsync")
	res, err := b.RunErrAsync(script, args...)
	if err != nil {
		b.gt.Fatalf("Failed to run async script:\n%s\n\n%s", script, err.Error())
	}
	b.recordProbe("RunAsync "+script, res)
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

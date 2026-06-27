package biloba

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

type bilobaJSResponse struct {
	Success bool   `json:"success"`
	Err     string `json:"error"`
	Result  any    `json:"result"`
}

func (r *bilobaJSResponse) Error() error {
	if r.Err == "" {
		return nil
	}
	return errors.New(r.Err)
}
func (r *bilobaJSResponse) MatcherResult() (bool, error) { return r.Success, r.Error() }
func (r *bilobaJSResponse) ResultString() string         { return toString(r.Result) }
func (r *bilobaJSResponse) ResultInt() int               { return toInt(r.Result) }
func (r *bilobaJSResponse) ResultBool() bool             { return toBool(r.Result) }
func (r *bilobaJSResponse) ResultStringSlice() []string  { return toStringSlice(r.Result) }
func (r *bilobaJSResponse) ResultAnySlice() []any        { return toAnySlice(r.Result) }

// encodeSelector turns a CSS string, an XPath, or a "/..."-prefixed string into the
// "s"/"x"-prefixed form that biloba.js's sel()/selEach() expect.
func encodeSelector(selector any) (string, error) {
	switch x := selector.(type) {
	case XPath:
		return "x" + string(x), nil
	case Locator:
		return x.encode()
	case string:
		if x[0] == '/' {
			return "x" + x, nil
		}
		return "s" + x, nil
	default:
		return "", fmt.Errorf("invalid selector type %T", x)
	}
}

func (b *Biloba) runBilobaHandler(name string, selector any, args ...any) *bilobaJSResponse {
	b.ensureBiloba()
	result := &bilobaJSResponse{}
	encoded, err := encodeSelector(selector)
	if err != nil {
		result.Err = err.Error()
		return result
	}
	parameters := []any{encoded}
	parameters = append(parameters, args...)
	_, err = b.RunErr(b.JSFunc("_biloba."+name).Invoke(parameters...), result)
	if err != nil {
		result.Err = err.Error()
	}
	return result
}

// runBilobaHandlerAsync is runBilobaHandler for primitives that return a Promise (e.g. the
// stability-aware scrollToStablePoint): it awaits the promise before decoding the response.
func (b *Biloba) runBilobaHandlerAsync(name string, selector any, args ...any) *bilobaJSResponse {
	b.ensureBiloba()
	result := &bilobaJSResponse{}
	encoded, err := encodeSelector(selector)
	if err != nil {
		result.Err = err.Error()
		return result
	}
	parameters := []any{encoded}
	parameters = append(parameters, args...)
	_, err = b.runErr(b.JSFunc("_biloba."+name).Invoke(parameters...), true, result)
	if err != nil {
		result.Err = err.Error()
	}
	return result
}

/*
HasElement(selector) returns true if an element matching selector is found

HasElement is a snapshot primitive: it captures what is present right now and does not poll (to wait, use Eventually(selector).Should(b.Exist())).  Configuring it (WithTimeout/WithPolling/WithContext/Immediate) is a hard error.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) HasElement(selector any) bool {
	b.gt.Helper()
	b.guardConfig("HasElement")
	r := b.runBilobaHandler("exists", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to check if element exists:\n%s", r.Error())
	}
	return r.Success
}

/*
Exist() is a Gomega matcher that passes if the selector exists.  Use it like this:

	Eventually("div.comment").Should(tab.Exist())

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) Exist() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("exists", selector).MatcherResult()
	}).WithMessage("exist")
}

/*
Count(selector) returns the number of elements matching selector

Count is a snapshot primitive: it captures the current count and does not poll (to gate on a stable count, use Eventually(selector).Should(b.HaveCount(n))).  Configuring it (WithTimeout/WithPolling/WithContext/Immediate) is a hard error.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) Count(selector any) int {
	b.gt.Helper()
	b.guardConfig("Count")
	r := b.runBilobaHandler("count", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to count elements:\n%s", r.Error())
	}
	return r.ResultInt()
}

/*
HaveCount(expected) is a Gomega matcher that passes if the number of elements returned by selector matches expected.  expected can be an integer or Gomega matcher.

Use it like this:

	Expect("div.comment").To(tab.HaveCount(3))
	Eventually("div.comment").Should(tab.HaveCount(BeNumerically(">", 5)))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) HaveCount(expected any) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("count", selector)
		if r.Error() != nil {
			return false, r.Error()
		}
		data["Result"] = r.ResultInt()
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveCount for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
BeVisible() is a Gomega matcher that passes if the first element returned by selector is visible.

Use it like this:

	Expect("div.comment").To(tab.BeVisible())
	Eventually("div.comment").Should(tab.BeVisible())

visibility is determined by non-zero offsetWidth and offsetHeight

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) BeVisible() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("isVisible", selector).MatcherResult()
	}).WithMessage("be visible")
}

/*
BeEnabled() is a Gomega matcher that passes if the first element returned by selector is not disabled.

Use it like this:

	Expect("input").To(tab.BeEnabled())
	Eventually("button").Should(tab.BeEnabled())

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) BeEnabled() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("isEnabled", selector).MatcherResult()
	}).WithMessage("be enabled")
}

// eachEmptyTemplate is the failure rendering shared by every Each* matcher when the selector matches
// no elements.  Each* asserts "there is at least one match AND all matches satisfy" - zero matches is
// a failure (a vacuous pass would be a silent false-positive), and under Eventually/Consistently it
// correctly makes the assertion wait for the elements to appear.
const eachEmptyTemplate = "Expected at least one element to match {{.Actual}}, but none did"

/*
EachBeVisible() is a Gomega matcher that passes if there is at least one element matching selector and every such element is visible.  It fails when no elements match.

Use it like this:

	Expect("div.comment").To(tab.EachBeVisible())
	Eventually("div.comment").Should(tab.EachBeVisible())

visibility is determined by non-zero offsetWidth and offsetHeight

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) EachBeVisible() types.GomegaMatcher {
	data := map[string]any{}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("eachIsVisible", selector)
		if r.Error() != nil {
			return false, r.Error()
		}
		data["Empty"] = r.ResultInt() == 0
		return r.Success, nil
	}).WithTemplate("{{if .Data.Empty}}"+eachEmptyTemplate+"{{else}}Expected {{.Actual}} {{.To}} each be visible{{end}}", data)
}

/*
EachBeEnabled() is a Gomega matcher that passes if there is at least one element matching selector and every such element is not disabled.  It fails when no elements match.

Use it like this:

	Expect("input").To(tab.EachBeEnabled())
	Eventually("button").Should(tab.EachBeEnabled())

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) EachBeEnabled() types.GomegaMatcher {
	data := map[string]any{}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("eachIsEnabled", selector)
		if r.Error() != nil {
			return false, r.Error()
		}
		data["Empty"] = r.ResultInt() == 0
		return r.Success, nil
	}).WithTemplate("{{if .Data.Empty}}"+eachEmptyTemplate+"{{else}}Expected {{.Actual}} {{.To}} each be enabled{{end}}", data)
}

/*
BeClickable() is a Gomega matcher that passes if the first element returned by selector is visible, enabled, and is the topmost element at its own center point - i.e. a real click would land on it rather than on something covering it.

Unlike a plain BeVisible() check it performs a synchronous, atomic occlusion/hittability test (via document.elementFromPoint): it fails if the element is obscured by another element (e.g. an overlay) or if its center is scrolled out of the viewport.  Like all of Biloba's primitives the check is deterministic and fails fast - it does not wait for animations to settle.

Use it like this:

	Expect("#submit").To(tab.BeClickable())
	Eventually("#submit").Should(tab.BeClickable())

Note that Biloba's plain Click() does NOT run this check (it clicks the element directly, even when covered); BeClickable lets you guard against occlusion explicitly.  For interactions that actually route around occlusion use the realistic mode - see [Biloba.Realistic].

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) BeClickable() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("isClickable", selector).MatcherResult()
	}).WithMessage("be clickable (visible, enabled, and not obscured)")
}

/*
GetInnerText(selector) returns the innerText of the first element matching selector.

Like all of Biloba's value-getters it polls by default: it waits until an element matching selector is present (an empty innerText is a valid result), then returns its innerText.  Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) GetInnerText(selector any) string {
	b.gt.Helper()
	return toString(b.GetProperty(selector, "innerText"))
}

/*
HaveInnerText(expected) is a Gomega matcher that passes if the first element returned by selector has innerText matching expected.  expected can be a string, or a Gomega matcher

Use it like this:

	Expect("div.comment").To(tab.HaveInnerText("hello world"))
	Expect("div.comment").To(tab.HaveInnerText(HaveSuffix("world")))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DO
M
*/
func (b *Biloba) HaveInnerText(expected any) types.GomegaMatcher {
	return b.HaveProperty("innerText", expected)
}

/*
CurrentInnerTextForEach(selector) returns a snapshot slice []string of innerText for each element matching selector.  It does not poll - gate on the matches being present first (e.g. Eventually(selector).Should(b.HaveCount(n))) when they appear asynchronously.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) CurrentInnerTextForEach(selector any) []string {
	b.gt.Helper()
	b.guardConfig("CurrentInnerTextForEach")
	return toStringSlice(b.CurrentPropertyForEach(selector, "innerText"))
}

/*
EachHaveInnerText(expected) is a Gomega matcher that passes if there is at least one element matching selector and the []string slice of innerTexts for all matching elements satisfies expected.  expected can be a []string, but you'll probably want to use a Gomega matcher.  It fails when no elements match - to assert that nothing matches use Eventually(selector).Should(b.HaveCount(0)) or ShouldNot(b.Exist()).

Use it like this:

	Eventually("div.comment").Should(tab.EachHaveInnerText(ContainElement("new comment")))
	//equivalent to, but tidier than
	Eventually(tab.CurrentInnerTextForEach).WithArgument("div.comment").Should(ContainElement("new comment"))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DO
M
*/
func (b *Biloba) EachHaveInnerText(args ...any) types.GomegaMatcher {
	return b.EachHaveProperty("innerText", args...)
}

/*
GetTextContent(selector) returns the textContent of the first element matching selector.

Unlike [Biloba.GetInnerText], textContent is computed straight from the DOM tree and does not depend on layout - which makes it reliable in headless Chrome for content that has just been added or changed (innerText can return a stale or partial value before a paint pass).  Note that textContent includes the text of hidden elements and of <script>/<style> children, and does not reflect CSS text-transform; reach for GetInnerText when you specifically need the rendered, visible text.

Like all of Biloba's value-getters it polls by default until an element matching selector is present; configure with WithTimeout/WithPolling/WithContext or Immediate().

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) GetTextContent(selector any) string {
	b.gt.Helper()
	return toString(b.GetProperty(selector, "textContent"))
}

/*
HaveTextContent(expected) is a Gomega matcher that passes if the first element returned by selector has textContent matching expected.  expected can be a string, or a Gomega matcher.

Prefer HaveTextContent over [Biloba.HaveInnerText] when asserting on dynamic content: textContent is layout-independent and so does not flake in headless Chrome the way innerText can.  See [Biloba.GetTextContent] for the semantic differences between the two.

Use it like this:

	Eventually("div.comment").Should(tab.HaveTextContent("hello world"))
	Eventually("div.comment").Should(tab.HaveTextContent(ContainSubstring("world")))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) HaveTextContent(expected any) types.GomegaMatcher {
	return b.HaveProperty("textContent", expected)
}

/*
CurrentTextContentForEach(selector) returns a snapshot slice []string of textContent for each element matching selector.  It does not poll - gate on the matches being present first (e.g. Eventually(selector).Should(b.HaveCount(n))) when they appear asynchronously.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) CurrentTextContentForEach(selector any) []string {
	b.gt.Helper()
	b.guardConfig("CurrentTextContentForEach")
	return toStringSlice(b.CurrentPropertyForEach(selector, "textContent"))
}

/*
EachHaveTextContent(expected) is a Gomega matcher that passes if there is at least one element matching selector and the []string slice of textContents for all matching elements satisfies expected.  expected can be a []string, but you'll probably want to use a Gomega matcher.  It fails when no elements match - to assert that nothing matches use Eventually(selector).Should(b.HaveCount(0)) or ShouldNot(b.Exist()).

Use it like this:

	Eventually("div.comment").Should(tab.EachHaveTextContent(ContainElement("new comment")))
	//equivalent to, but tidier than
	Eventually(tab.CurrentTextContentForEach).WithArgument("div.comment").Should(ContainElement("new comment"))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) EachHaveTextContent(args ...any) types.GomegaMatcher {
	return b.EachHaveProperty("textContent", args...)
}

/*
GetProperty(selector, property) returns the named javascript property from the first element matching selector.

GetProperty polls by default: it waits until an element matching selector is present AND the requested property is defined, then returns the property's value as type any (you'll need to do type assertions yourself, or use a Gomega matcher to handle the types for you).  If the element never appears, or the property never becomes defined, GetProperty times out and fails the spec.

Wrap the property name in [Biloba.AllowMissing] to make an undefined property a valid (nil) result rather than something to wait for:

	tab.GetProperty("div.comment", "dataset.poster")                  // waits until dataset.poster is defined
	tab.GetProperty("div.comment", tab.AllowMissing("dataset.poster")) // returns nil if it's absent

Dot-delimited properties are also supported.  Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) GetProperty(selector any, property any) any {
	b.gt.Helper()
	name := nameOf(property)
	var result any
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		r := b.runBilobaHandler("getPropertiesP", sel, []any{property})
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		result = newProperties(r.Result).Get(name)
		return true, nil
	}).WithMessage(fmt.Sprintf("have property %q", name))
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
CurrentPropertyForEach(selector, property) returns a snapshot of the requested property for all elements matching selector.  It returns []any (nil entries stand in for elements where the property is undefined) and follows the rules of [Biloba.GetProperty].  If no elements are found an empty slice is returned.

Unlike the singular [Biloba.GetProperty], CurrentPropertyForEach does not poll - it captures what is present right now.  Gate on the matches being present first (e.g. Eventually(selector).Should(b.HaveCount(n))) when they appear asynchronously.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) CurrentPropertyForEach(selector any, property string) []any {
	b.gt.Helper()
	b.guardConfig("CurrentPropertyForEach")
	r := b.runBilobaHandler("getPropertyForEach", selector, property)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get property \"%s\" for each:\n%s", property, r.Error())
	}
	return r.ResultAnySlice()
}

/*
GetAttribute(selector, name) returns the named HTML attribute of the first element matching selector.  It returns the raw attribute string, or nil when the attribute is not present.

GetAttribute is the immediate sibling of the [Biloba.HaveAttribute] matcher - reach for it when you want an attribute value in a Go variable for control-flow rather than to assert on.  Unlike [Biloba.GetProperty], it reads the raw markup attribute (e.g. the literal href="/about") rather than the resolved DOM property (the absolute URL).

GetAttribute polls by default: it waits until an element matching selector is present AND the requested attribute is present, then returns the raw attribute string.  Wrap the name in [Biloba.AllowMissing] to make an absent attribute a valid (nil) result rather than something to wait for:

	tab.GetAttribute("#link", "href")                  // waits until href is present
	tab.GetAttribute("#link", tab.AllowMissing("href")) // returns nil if href is absent

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties and attributes
*/
func (b *Biloba) GetAttribute(selector any, name any) any {
	b.gt.Helper()
	attr := nameOf(name)
	var result any
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		r := b.runBilobaHandler("getAttributesP", sel, []any{name})
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		result = newProperties(r.Result).Get(attr)
		return true, nil
	}).WithMessage(fmt.Sprintf("have attribute %q", attr))
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
CurrentAttributeForEach(selector, name) returns a snapshot of the named HTML attribute for all elements matching selector.  It returns []any and follows the rules of [Biloba.GetAttribute] - nil entries stand in for elements that lack the attribute.  If no elements are found an empty slice is returned.

Unlike the singular [Biloba.GetAttribute], CurrentAttributeForEach does not poll - it captures what is present right now.  Gate on the matches being present first (e.g. Eventually(selector).Should(b.HaveCount(n))) when they appear asynchronously.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties and attributes
*/
func (b *Biloba) CurrentAttributeForEach(selector any, name string) []any {
	b.gt.Helper()
	b.guardConfig("CurrentAttributeForEach")
	r := b.runBilobaHandler("getAttributeForEach", selector, name)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get attribute \"%s\" for each:\n%s", name, r.Error())
	}
	return r.ResultAnySlice()
}

/*
HaveProperty() is a Gomega matcher with two modes of operation:

When invoked with only one argument, it passes only if the first element matching selector has the requested javascript property defined on it:

	Eventually("div.comment").Should(tab.HaveProperty("dataset.poster"))

When invoked with two arguments, it only passes if the value of the specified property matches the second argument.  This expect argument can be a Gomega matcher.  Otherwise gomega.Equal is used

	Eventually("div.comment").Should(tab.HaveProperty("dataset.poster", "Jane"))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) HaveProperty(property string, expected ...any) types.GomegaMatcher {
	var data = map[string]any{}
	data["Property"] = property
	if len(expected) == 0 {
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			r := b.runBilobaHandler("hasProperty", selector, property)
			if r.Error() != nil {
				return false, r.Error()
			}
			return r.Success, nil
		}).WithTemplate("Expected {{.Actual}} {{.To}} have property \"{{.Data.Property}}\"", data)
	} else {
		var matcher = matcherOrEqual(expected[0])
		data["Matcher"] = matcher
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			r := b.runBilobaHandler("getProperty", selector, property)
			if r.Error() != nil {
				return false, r.Error()
			}
			data["Result"] = r.Result
			return matcher.Match(data["Result"])
		}).WithTemplate("HaveProperty \"{{.Data.Property}}\" for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
	}
}

/*
EachHaveProperty() is a Gomega matcher with two modes of operation.  Like the rest of the Each* family it requires at least one match: zero matches fails (to assert that nothing matches use Eventually(selector).Should(b.HaveCount(0)) or ShouldNot(b.Exist())).

When invoked with only one argument, it passes only if there is at least one element matching selector and all such elements have the requested javascript property defined on them:

	Eventually("div.comment").Should(tab.EachHaveProperty("dataset.poster"))

When invoked with more than one argument, it only passes if the slice of values representing the property collected from the elements exactly matches the subsequent expected arguments:

	Eventually("div.comment").Should(tab.EachHaveProperty("dataset.poster", "Jane", "George", "Sally"))

Alternatively, you can pass a Gomega matcher as a single expected argument after property.  Biloba will present the slice of properties to that matcher.  For example:

	Eventually("div.comment").Should(tabEach.HaveProperty("dataset.poster", ContainElement("George")))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) EachHaveProperty(property string, expected ...any) types.GomegaMatcher {
	var data = map[string]any{}
	data["Property"] = property
	if len(expected) == 0 {
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			r := b.runBilobaHandler("eachHasProperty", selector, property)
			if r.Error() != nil {
				return false, r.Error()
			}
			data["Empty"] = r.ResultInt() == 0
			return r.Success, nil
		}).WithTemplate("{{if .Data.Empty}}"+eachEmptyTemplate+"{{else}}Expected each {{.Actual}} {{.To}} each have property \"{{.Data.Property}}\"{{end}}", data)
	} else {
		var matcher types.GomegaMatcher
		if x, ok := expected[0].(types.GomegaMatcher); ok && len(expected) == 1 {
			matcher = x
		} else {
			matcher = gomega.HaveExactElements(nilSafeSlice(expected)...)
		}

		data["Matcher"] = matcher
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			r := b.runBilobaHandler("getPropertyForEach", selector, property)
			if r.Error() != nil {
				return false, r.Error()
			}
			data["Result"] = r.Result
			// Fail (with a clear message) on zero matches before handing the empty slice to the
			// value matcher - otherwise a matcher like BeEmpty() would vacuously pass, and the rest
			// would report a confusing slice-length mismatch instead of "no elements matched".
			if s, ok := r.Result.([]any); ok && len(s) == 0 {
				data["Empty"] = true
				return false, nil
			}
			data["Empty"] = false
			return matcher.Match(data["Result"])
		}).WithTemplate("{{if .Data.Empty}}"+eachEmptyTemplate+"{{else}}EachHaveProperty \"{{.Data.Property}}\" for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}{{end}}", data)
	}
}

/*
SetProperty() has two modes of operation:

When invoked with a selector and two arguments:

	tab.SetProperty(selector, property, value)

it polls by default until the first element matching selector is present, then sets the specified property to value - failing the spec if the element never appears before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].  property must have type string but value can be any type

When invoked with just two arguments, tab.SetProperty returns a Gomega matcher that will only succeed once an element is found and its property set:

	Eventually("div.comment").Should(tab.SetProperty("dataset.poster", "George"))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) SetProperty(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) == 3 {
		property, value := args[1], args[2]
		matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("setProperty", selector, property, value).MatcherResult()
		}).WithMessage("be property-settable")
		b.pollOrImmediate(args[0], matcher)
		return nil
	} else {
		b.guardBareMatcher("SetProperty")
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("setProperty", selector, args[0], args[1]).MatcherResult()
		}).WithMessage("be property-settable")
	}
}

/*
SetPropertyForEachImmediately() sets the specified property to the specified value on all DOM elements matching selector. It does nothing if no elements match.

Like the rest of the *Each family it acts immediately and has no matcher form - it does not poll.  Gate it on the matches being present first (e.g. Eventually(selector).Should(b.HaveCount(n))) when they appear asynchronously.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) SetPropertyForEachImmediately(selector any, property string, value any) {
	b.gt.Helper()
	b.guardConfig("SetPropertyForEachImmediately")
	r := b.runBilobaHandler("setPropertyForEach", selector, property, value)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to set property \"%s\" for each:\n%s", property, r.Error())
	}
}

/*
GetProperties() returns a [Properties] struct containing multiple properties from the first DOM element selected by selector.

	p := GetProperties(".notice", "tagName", "classList", "dataset", "disabled")
	p.GetString("tagName") //"DIV"
	p.GetStringSlice("classList") //[]string{"notice", "highlight"}
	p.GetBool("disabled") //false

GetProperties polls by default: it waits until an element matching selector is present AND every requested property is defined, then returns them all.  Wrap individual names in [Biloba.AllowMissing] to let an undefined property come back as nil instead of blocking the poll:

	GetProperties(".notice", "tagName", tab.AllowMissing("dataset.poster"))

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) GetProperties(selector any, properties ...any) Properties {
	b.gt.Helper()
	if len(properties) == 0 {
		b.gt.Fatalf("GetProperties requires at least one property to fetch")
		return nil
	}
	var result Properties
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		r := b.runBilobaHandler("getPropertiesP", sel, properties)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		result = newProperties(r.Result)
		return true, nil
	}).WithMessage(fmt.Sprintf("have properties %s", strings.Join(namesOf(properties), ", ")))
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
GetAttributes() returns a [Properties] struct containing multiple raw HTML attributes (via getAttribute) from the first DOM element selected by selector.  It is the batch sibling of [Biloba.GetAttribute] and the attribute-flavored counterpart of [Biloba.GetProperties].

	a := GetAttributes("#link", "href", "data-role")
	a.GetString("href")      //"/about"
	a.GetString("data-role") //"nav"

GetAttributes polls by default: it waits until an element matching selector is present AND every requested attribute is present, then returns them all.  Wrap individual names in [Biloba.AllowMissing] to let an absent attribute come back as nil instead of blocking the poll:

	GetAttributes("#link", "href", tab.AllowMissing("data-missing"))

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties and attributes
*/
func (b *Biloba) GetAttributes(selector any, names ...any) Properties {
	b.gt.Helper()
	if len(names) == 0 {
		b.gt.Fatalf("GetAttributes requires at least one attribute to fetch")
		return nil
	}
	var result Properties
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		r := b.runBilobaHandler("getAttributesP", sel, names)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		result = newProperties(r.Result)
		return true, nil
	}).WithMessage(fmt.Sprintf("have attributes %s", strings.Join(namesOf(names), ", ")))
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
CurrentPropertiesForEach() returns a [SliceOfProperties] - i.e. a slice of [Properties] - from each DOM element selected by selector.  If no DOM element matches, CurrentPropertiesForEach() returns an empty SliceOfProperties.  If any of the requested properties don't exist - those individual properties will be set to nil.

	p := CurrentPropertiesForEach(".notice", "tagName", "classList", "dataset", "disabled")
	p.GetString("tagName") //[]string{"DIV", "DIV", "DIV"}
	p.GetStringSlice("classList") //[][]string{{"notice", "highlight"}, {"notice", "gray"}, {"notice"}}
	p.GetBool("disabled") //[]bool{false, true, false}

Unlike the singular [Biloba.GetProperties], CurrentPropertiesForEach does not poll - it captures what is present right now.  Gate on the matches being present first (e.g. Eventually(selector).Should(b.HaveCount(n))) when they appear asynchronously.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) CurrentPropertiesForEach(selector any, properties ...string) SliceOfProperties {
	b.gt.Helper()
	b.guardConfig("CurrentPropertiesForEach")
	if len(properties) == 0 {
		b.gt.Fatalf("CurrentPropertiesForEach requires at least one property to fetch")
		return nil
	}
	r := b.runBilobaHandler("getPropertiesForEach", selector, properties)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get properties %s for each:\n%s", strings.Join(properties, ", "), r.Error())
	}
	return newSliceOfProperties(r.ResultAnySlice())
}

/*
CurrentAttributesForEach() returns a [SliceOfProperties] - i.e. a slice of [Properties] - of the named raw HTML attributes (via getAttribute) from each DOM element selected by selector.  It is the snapshot, for-each sibling of [Biloba.GetAttributes] and the attribute-flavored counterpart of [Biloba.CurrentPropertiesForEach].  If no DOM element matches, CurrentAttributesForEach() returns an empty SliceOfProperties.  An attribute that is absent on a given element comes back as nil for that element.

	a := CurrentAttributesForEach(".link", "href", "data-role")
	a.GetString("href")      //[]string{"/about", "/home"}
	a.GetString("data-role") //[]string{"nav", ""}

Unlike [Biloba.GetAttributes], CurrentAttributesForEach does not poll - it captures whatever is present right now and never blocks on a missing attribute (there is no AllowMissing axis).  Gate on the matches being present first (e.g. Eventually(selector).Should(b.HaveCount(n))) when they appear asynchronously.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties and attributes
*/
func (b *Biloba) CurrentAttributesForEach(selector any, names ...any) SliceOfProperties {
	b.gt.Helper()
	b.guardConfig("CurrentAttributesForEach")
	if len(names) == 0 {
		b.gt.Fatalf("CurrentAttributesForEach requires at least one attribute to fetch")
		return nil
	}
	r := b.runBilobaHandler("getAttributesForEach", selector, namesOf(names))
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get attributes %s for each:\n%s", strings.Join(namesOf(names), ", "), r.Error())
	}
	return newSliceOfProperties(r.ResultAnySlice())
}

/*
GetValue returns the form/input related value for the first element matched by selector

Biloba rationalizes the behavior of all input, select, and textarea elements so you don't have to fiddle with the differences:

	tab.GetValue("textarea") //will be a string representing the text in the textarea
	tab.GetValue("input[type='text']") // will be a string representing the text value of the input
	tab.GetValue("input[type='checkbox']") // will be true or false depending on whether the checkbox is checked
	tab.GetValue("input[type='radio']") // will be the value attribute of the selected radio button in the name group associated with the selected element
	tab.GetValue("select") // will be the value of the selected option of the select element
	tab.GetValue("select.multi-select") // will be a []string of values for all the selected options of the multiple select element

GetValue polls by default: it waits until an element matching selector is present, then returns its value.  An empty string (or an unselected radio group's "") is a valid value - GetValue does not wait for the value to become non-empty.  Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#form-elements to learn more about working with form elements
*/
func (b *Biloba) GetValue(selector any) any {
	b.gt.Helper()
	var result any
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		r := b.runBilobaHandler("getValueP", sel)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		result = r.Result
		return true, nil
	}).WithMessage("have a value")
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
CurrentValueForEach(selector) returns a snapshot []any of the rationalized form/input value for every element matching selector, following the rules of [Biloba.GetValue].  If no elements match it returns an empty slice.

Unlike [Biloba.GetValue], CurrentValueForEach does not poll - it captures whatever is present right now.  Gate on the matches being present first (e.g. Eventually(selector).Should(b.HaveCount(n))) when they appear asynchronously.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#form-elements to learn more about working with form elements
*/
func (b *Biloba) CurrentValueForEach(selector any) []any {
	b.gt.Helper()
	b.guardConfig("CurrentValueForEach")
	r := b.runBilobaHandler("getValueForEach", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get value for each:\n%s", r.Error())
	}
	return r.ResultAnySlice()
}

/*
HaveValue returns a Gomega matcher to assert that the first element matching selector has the expected value.  If you pass in a Gomega matcher it will be used

For example:

	Expect("textarea").To(tab.HaveValue(ContainSubstring("hello")))
	Expect("input[type='text']").To(tab.HaveValue("Sally"))
	Expect("input[type='checkbox']").To(tab.HaveValue(BeTrue()))
	Expect("input[type='radio']").To(tab.HaveValue("red")) //here red is the value of the selected radio button
	Expect("select").To(tab.HaveValue("obi-wan")) //here obi-wan is the value of the selected option
	Expect("select.multi-select").To(tab.HaveValue(ConsistOf("obi-wan", "leia", "han"))) //here we assert that these three options are selected

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#form-elements to learn more about working with form elements
*/
func (b *Biloba) HaveValue(expected any) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("getValue", selector)
		if r.Error() != nil {
			return false, r.Error()
		}
		data["Result"] = r.Result
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveValue for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
SetValue() has two modes of operation:

When invoked with a selector and a value:

	tab.SetValue(selector, value)

it polls by default until the first element matching selector is present, visible, and enabled, then sets its value - failing the spec if that never happens before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with just one argument, tab.SetValue returns a Gomega matcher that will only succeed once an element is found, is visible, and is enabled and its value gets set:

	Eventually("input[type='checkbox']").Should(tab.SetValue(true))

the types you provide `SetValue` will depend on the type of input you are addressing.  See [Biloba.GetValue] for examples.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#form-elements to learn more about working with form elements
*/
func (b *Biloba) SetValue(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) == 2 {
		value := args[1]
		var matcher types.GomegaMatcher
		if b.realistic {
			matcher = gcustom.MakeMatcher(func(selector any) (bool, error) {
				return b.realisticSetValue(selector, value)
			}).WithMessage("be value-settable (realistically)")
		} else {
			matcher = gcustom.MakeMatcher(func(selector any) (bool, error) {
				return b.runBilobaHandler("setValue", selector, value).MatcherResult()
			}).WithMessage("be value-settable")
		}
		b.pollOrImmediate(args[0], matcher)
		return nil
	}
	b.guardBareMatcher("SetValue")
	if b.realistic {
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.realisticSetValue(selector, args[0])
		}).WithMessage("be value-settable (realistically)")
	}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("setValue", selector, args[0]).MatcherResult()
	}).WithMessage("be value-settable")
}

/*
ValueLabel wraps a string so that SetValue targets a <select> option by its visible label (its displayed text) instead of its underlying value:

	tab.SetValue("#model", tab.ValueLabel("Sonnet"))            // selects the <option> whose text is "Sonnet"
	Eventually("#model").Should(tab.SetValue(tab.ValueLabel("Sonnet")))

By default SetValue matches a <select> option by its value attribute; wrap the argument in ValueLabel to match by label instead.  For a multi-select, pass a slice whose entries are ValueLabels (you may mix labels and raw values).  ValueLabel is only meaningful for <select> elements.

Read https://onsi.github.io/biloba/#form-elements to learn more about working with form elements
*/
func (b *Biloba) ValueLabel(label string) ValueLabel {
	return ValueLabel(label)
}

type ValueLabel string

func (v ValueLabel) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"__biloba_value_label": string(v)})
}

/*
AllowMissing wraps a property or attribute name passed to one of the two-axis getters ([Biloba.GetProperty], [Biloba.GetProperties], [Biloba.GetAttribute], [Biloba.GetAttributes]) so that an undefined property / absent attribute is a valid (nil) result instead of something the poll waits for.

By default those getters poll until the element is present AND every named property/attribute is defined.  A name wrapped in AllowMissing is exempt from the "defined" requirement - it comes back as nil if absent and never blocks the poll:

	tab.GetProperty("#user", tab.AllowMissing("dataset.middleName"))             // nil if not set
	tab.GetProperties("#user", "dataset.firstName", tab.AllowMissing("dataset.middleName"))

Sharp edge: a property that simply does not exist on the element type (e.g. "disabled" on a <div>, where "disabled" in div is false) would otherwise block the poll until it times out.  Wrap such names in AllowMissing to get the old nil/zero-value back.

AllowMissing is only meaningful for the property/attribute getters; it has no effect elsewhere.

Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) AllowMissing(name string) AllowMissing {
	return AllowMissing(name)
}

type AllowMissing string

func (a AllowMissing) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]string{"__biloba_allow_missing": string(a)})
}

// nameOf returns the plain property/attribute name carried by a getter name argument - either an
// AllowMissing wrapper or a bare string.  It mirrors biloba.js's parseNameSpec so the Go side can key
// into the result map the handler returns.
func nameOf(spec any) string {
	if a, ok := spec.(AllowMissing); ok {
		return string(a)
	}
	if s, ok := spec.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", spec)
}

// namesOf maps nameOf over a slice (for building human-readable failure messages).
func namesOf(specs []any) []string {
	out := make([]string, len(specs))
	for i, spec := range specs {
		out[i] = nameOf(spec)
	}
	return out
}

/*
HaveClass returns a Gomega matcher to assert that the first element matching selector has the expected class.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) HaveClass(expected string) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = gomega.ContainElement(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("getProperty", selector, "classList")
		if r.Error() != nil {
			return false, r.Error()
		}
		data["Result"] = r.ResultStringSlice()
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveClass for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
EachHaveClass returns a Gomega matcher that passes if there is at least one element matching selector and every such element has the expected class.  It fails when no elements match.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) EachHaveClass(expected string) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = gomega.HaveEach(gomega.ContainElement(expected))
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("getPropertyForEach", selector, "classList")
		if r.Error() != nil {
			return false, r.Error()
		}
		data["Result"] = r.Result
		if classLists, ok := r.Result.([]any); ok && len(classLists) == 0 {
			data["Empty"] = true
			return false, nil // fail (not vacuously pass) when no elements match
		}
		data["Empty"] = false
		return matcher.Match(data["Result"])
	}).WithTemplate("{{if .Data.Empty}}"+eachEmptyTemplate+"{{else}}EachHaveClass for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}{{end}}", data)
}

/*
HaveText(expected) is a Gomega matcher that passes if the first element returned by selector has innerText matching expected, after whitespace normalization.

Unlike [Biloba.HaveInnerText], HaveText trims leading/trailing whitespace and collapses internal runs of whitespace down to a single space before matching.  This prevents spurious failures caused by templating whitespace.  expected can be a string, or a Gomega matcher.

Use it like this:

	Expect("div.comment").To(tab.HaveText("hello world"))
	Expect("div.comment").To(tab.HaveText(HaveSuffix("world")))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) HaveText(expected any) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("getProperty", selector, "innerText")
		if r.Error() != nil {
			return false, r.Error()
		}
		data["Result"] = normalizeWhitespace(r.ResultString())
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveText for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
HaveAttribute() is a Gomega matcher with two modes of operation that matches against an element's HTML attribute (via getAttribute) - distinct from [Biloba.HaveProperty], which matches against a javascript property.

When invoked with only the attribute name, it passes if the first element matching selector has the requested attribute:

	Eventually("a").Should(tab.HaveAttribute("href"))

When invoked with a name and an expected value, it only passes if the value of the attribute matches expected.  expected can be a string or a Gomega matcher:

	Eventually("a").Should(tab.HaveAttribute("href", "/about"))
	Eventually("a").Should(tab.HaveAttribute("href", HaveSuffix("about")))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) HaveAttribute(name string, expected ...any) types.GomegaMatcher {
	var data = map[string]any{}
	data["Name"] = name
	if len(expected) == 0 {
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			r := b.runBilobaHandler("hasAttribute", selector, name)
			if r.Error() != nil {
				return false, r.Error()
			}
			return r.Success, nil
		}).WithTemplate("Expected {{.Actual}} {{.To}} have attribute \"{{.Data.Name}}\"", data)
	}
	var matcher = matcherOrEqual(expected[0])
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("getAttribute", selector, name)
		if r.Error() != nil {
			return false, r.Error()
		}
		data["Result"] = r.Result
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveAttribute \"{{.Data.Name}}\" for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
BeChecked() is a Gomega matcher that passes if the first element matching selector (a checkbox or radio button) is checked.  It is sugar for HaveProperty("checked", true).

Use it like this:

	Expect("input[type='checkbox']").To(tab.BeChecked())
	Eventually("input[type='radio']").Should(tab.BeChecked())

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) BeChecked() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("getProperty", selector, "checked")
		if r.Error() != nil {
			return false, r.Error()
		}
		return r.ResultBool(), nil
	}).WithMessage("be checked")
}

/*
BeFocused() is a Gomega matcher that passes if the first element matching selector is the document's activeElement (i.e. it has focus).

Use it like this:

	Expect("input").To(tab.BeFocused())
	Eventually("input").Should(tab.BeFocused())

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) BeFocused() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("isFocused", selector).MatcherResult()
	}).WithMessage("be focused")
}

/*
HaveComputedStyle(property, expected) is a Gomega matcher that passes if the computed CSS style (via getComputedStyle) of the named property on the first element matching selector matches expected.  expected can be a string or a Gomega matcher.

Use it like this:

	Expect("div.notice").To(tab.HaveComputedStyle("display", "none"))
	Eventually("div.notice").Should(tab.HaveComputedStyle("color", "rgb(255, 0, 0)"))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) HaveComputedStyle(property string, expected any) types.GomegaMatcher {
	var data = map[string]any{}
	data["Property"] = property
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("getComputedStyle", selector, property)
		if r.Error() != nil {
			return false, r.Error()
		}
		data["Result"] = r.Result
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveComputedStyle \"{{.Data.Property}}\" for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

/*
Click() has two modes of operation:

When invoked with a selector:

	tab.Click("#submit")

it polls by default until the first element matching selector exists, is visible, and is enabled, then clicks it - failing the spec if that never happens before the timeout.  Opt out with [Biloba.Immediate] to act once and fail fast, or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with no selector, tab.Click() returns a Gomega matcher you poll yourself:

	Eventually("#submit").Should(tab.Click())

Both forms accept [PointerOption]s after the selector (or in place of it, for the matcher) to target an offset and/or hold keyboard modifiers:

	tab.Click("#canvas", b.At(30, 40), b.Shift())
	Eventually("#canvas").Should(tab.Click(b.At(30, 40), b.Shift()))

A plain tab.Click(selector) dispatches the maximally-faithful native element.click().  Passing any option instead dispatches synthetic mousedown/mouseup/click MouseEvents carrying the coordinates and modifier flags (realistic mode always uses real CDP input) - a deliberate fidelity-for-control tradeoff.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) Click(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("click", "be clickable", args, b.performClick)
}

/*
ClickEachImmediately() clicks on every DOM element matching selector that is visible and enabled.

If no elements match, nothing happens.

Like the rest of the *Each family it acts immediately and has no matcher form - it does not poll.  Gate it on the matches being present first (e.g. Eventually(selector).Should(b.HaveCount(n))) when they appear asynchronously.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) ClickEachImmediately(selector any) {
	b.gt.Helper()
	b.guardConfig("ClickEachImmediately")
	if b.realistic {
		if err := b.realisticClickEach(selector); err != nil {
			b.gt.Fatalf("Failed to click each:\n%s", err.Error())
		}
		return
	}
	r := b.runBilobaHandler("clickEach", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to click each:\n%s", r.Error())
	}
}

/*
DblClick() double-clicks the first element matching selector.

	tab.DblClick("#row")

it polls by default until the element exists, is visible, and is enabled, then double-clicks it (fast mode fires two click events plus a dblclick event; realistic mode dispatches a real double mouse click) - failing the spec if that never happens before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with no selector, tab.DblClick() returns a Gomega matcher:

	Eventually("#row").Should(tab.DblClick())

Like Click, it accepts [PointerOption]s (b.At/b.Shift/...) after the selector or in place of it.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) DblClick(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("double-click", "be double-clickable", args, b.performDblClick)
}

/*
RightClick() right-clicks (context-clicks) the first element matching selector.

	tab.RightClick("#row")

it polls by default until the element exists, is visible, and is enabled, then right-clicks it (fast mode dispatches mousedown/mouseup/contextmenu events; realistic mode dispatches a real right-button mouse click) - failing the spec if that never happens before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with no selector, tab.RightClick() returns a Gomega matcher:

	Eventually("#row").Should(tab.RightClick())

Like Click, it accepts [PointerOption]s (b.At/b.Shift/...) after the selector or in place of it.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) RightClick(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("right-click", "be right-clickable", args, b.performRightClick)
}

/*
MiddleClick() middle-clicks (auxiliary-clicks) the first element matching selector.

	tab.MiddleClick("#row")

it polls by default until the element exists, is visible, and is enabled, then middle-clicks it (fast mode dispatches mousedown/mouseup/auxclick events; realistic mode dispatches a real middle-button mouse click) - failing the spec if that never happens before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with no selector, tab.MiddleClick() returns a Gomega matcher:

	Eventually("#row").Should(tab.MiddleClick())

Like Click, it accepts [PointerOption]s (b.At/b.Shift/...) after the selector or in place of it.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) MiddleClick(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("middle-click", "be middle-clickable", args, b.performMiddleClick)
}

/*
Tap() taps (touches) the first element matching selector.

	tab.Tap("#row")

it polls by default until the element exists, is visible, and is enabled, then taps it (fast mode dispatches synthetic touch and pointer events plus a click; realistic mode dispatches a real CDP touch) - failing the spec if that never happens before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with no selector, tab.Tap() returns a Gomega matcher:

	Eventually("#row").Should(tab.Tap())

It accepts a b.At(x, y) [PointerOption] to tap at an offset; keyboard modifiers don't apply to touch and are ignored.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Tap(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("tap", "be tappable", args, b.performTap)
}

/*
DragTo() has two modes of operation:

When invoked with a source and a target selector:

	tab.DragTo("#card", "#column")

it drags source's center onto target's center with a pointer-based drag sequence (pointerdown/pointermove/pointerup plus the matching mouse events; realistic mode dispatches the drag with real CDP mouse input).  It is meant for pointer-based drag-and-drop libraries (@dnd-kit and the like); it does NOT drive native HTML5 draggable - for that, drop to chromedp via tab.Context.  It polls by default, retrying the whole find-source/find-target/drag operation until both endpoints are present and source is visible - failing the spec if that never happens before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with just a target, tab.DragTo() returns a Gomega matcher whose subject is the source.  This lets you poll until both source and target are present and the drag can be performed - folding the wait into the action so you don't have to assert both endpoints exist first:

	Eventually("#card").Should(tab.DragTo("#column"))

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) DragTo(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) >= 2 {
		// Immediate form: poll the WHOLE find-source/find-target/drag operation.  The matcher's
		// subject is the source and it re-resolves the target on every attempt (performDrag
		// re-encodes and re-finds target in JS each call), so a late-arriving target is waited on too.
		target := args[1]
		matcher := gcustom.MakeMatcher(func(source any) (bool, error) {
			return b.performDrag(source, target)
		}).WithMessage("be draggable to the target")
		b.pollOrImmediate(args[0], matcher)
		return nil
	}
	target := args[0]
	b.guardBareMatcher("DragTo")
	return gcustom.MakeMatcher(func(source any) (bool, error) {
		return b.performDrag(source, target)
	}).WithMessage("be draggable to the target")
}

// performDrag is the fast/realistic fork shared by DragTo's immediate and matcher forms.
func (b *Biloba) performDrag(source, target any) (bool, error) {
	if b.realistic {
		return b.realisticDragTo(source, target)
	}
	encodedTarget, err := encodeSelector(target)
	if err != nil {
		return false, err
	}
	return b.runBilobaHandler("dragTo", source, encodedTarget).MatcherResult()
}

/*
ScrollWheel() scrolls the mouse wheel over the first element matching selector.

	tab.ScrollWheel("#scroll-box", 0, 200)   // scrolls down 200px (positive deltaY is down, positive deltaX is right)

it polls by default until the element is present and visible, then dispatches a wheel event at the element's center and scrolls the nearest scrollable ancestor (realistic mode dispatches a real CDP wheel event that scrolls via genuine trusted input) - failing the spec if that never happens before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with just the deltas (no selector), tab.ScrollWheel() returns a Gomega matcher so you can poll until an element is present to scroll:

	Eventually("#scroll-box").Should(tab.ScrollWheel(0, 200))

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) ScrollWheel(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	switch len(args) {
	case 3:
		dx, okX := asFloat64(args[1])
		dy, okY := asFloat64(args[2])
		if !okX || !okY {
			b.gt.Fatalf("ScrollWheel requires numeric deltaX and deltaY")
			return nil
		}
		matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.performScrollWheel(selector, dx, dy)
		}).WithMessage("be scrollable with the mouse wheel")
		b.pollOrImmediate(args[0], matcher)
		return nil
	case 2:
		dx, okX := asFloat64(args[0])
		dy, okY := asFloat64(args[1])
		if !okX || !okY {
			b.gt.Fatalf("ScrollWheel requires numeric deltaX and deltaY")
			return nil
		}
		b.guardBareMatcher("ScrollWheel")
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.performScrollWheel(selector, dx, dy)
		}).WithMessage("be scrollable with the mouse wheel")
	default:
		b.gt.Fatalf("ScrollWheel requires a deltaX and deltaY (preceded by a selector when used immediately)")
		return nil
	}
}

// performScrollWheel is the fast/realistic fork shared by ScrollWheel's immediate and matcher forms.
func (b *Biloba) performScrollWheel(selector any, deltaX, deltaY float64) (bool, error) {
	if b.realistic {
		if err := b.realisticScrollWheel(selector, deltaX, deltaY); err != nil {
			return false, err
		}
		return true, nil
	}
	return b.runBilobaHandler("scrollWheel", selector, deltaX, deltaY).MatcherResult()
}

/*
Focus() focuses the first element matching selector.

When invoked with a selector, tab.Focus("input.search") polls by default until the element exists, is visible, and is enabled, then focuses it - failing the spec if that never happens before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with no arguments, tab.Focus() returns a Gomega matcher so you can poll until an element is focusable:

	Eventually("input.search").Should(tab.Focus())

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Focus(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("focus", selector).MatcherResult()
	}).WithMessage("be focusable")
	if len(args) > 0 {
		b.pollOrImmediate(args[0], matcher)
		return nil
	}
	b.guardBareMatcher("Focus")
	return matcher
}

/*
Blur() blurs (removes focus from) the first element matching selector.

This is useful when you want to explicitly trigger a blur handler - for example an input that commits its value or validates onBlur.  Note that [Biloba.SetValue] does not blur text inputs, so reach for Blur() when you need that behavior:

	tab.SetValue("input.name", "New Name")
	tab.Blur("input.name") // fires the onBlur commit handler

Note that a blur event only fires if the element is actually focused; [Biloba.SetValue] leaves the text input it sets focused, so the example above works.

When invoked with a selector, tab.Blur("input.search") polls by default until the element is present, then blurs it - failing the spec if it never appears before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with no arguments, tab.Blur() returns a Gomega matcher so you can poll until an element is present to blur:

	Eventually("input.search").Should(tab.Blur())

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Blur(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("blur", selector).MatcherResult()
	}).WithMessage("be blurrable")
	if len(args) > 0 {
		b.pollOrImmediate(args[0], matcher)
		return nil
	}
	b.guardBareMatcher("Blur")
	return matcher
}

/*
Hover() dispatches the pointer/mouse events associated with hovering (pointerover, mouseover, pointerenter, mouseenter, mousemove) at the first element matching selector.

Like all of Biloba's interactions this is a pragmatic simulation, not a real pointer: it fires synthetic events synchronously and atomically in the browser.  That means it triggers JavaScript hover handlers (e.g. a menu that opens on mouseenter) but does not activate CSS :hover styling - for that you'll need to drop down to chromedp's input domain.

When invoked with a selector, tab.Hover(".menu") polls by default until the element is present and visible, then hovers it - failing the spec if that never happens before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with no arguments, tab.Hover() returns a Gomega matcher so you can poll until an element is hoverable:

	Eventually(".menu").Should(tab.Hover())

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Hover(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("hover", "be hoverable", args, b.performHover)
}

/*
ScrollIntoView() scrolls the first element matching selector into view (via the element's scrollIntoView()).

When invoked with a selector, tab.ScrollIntoView("#footer") polls by default until the element is present, then scrolls it into view - failing the spec if it never appears before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with no arguments, tab.ScrollIntoView() returns a Gomega matcher so you can poll until an element is present to scroll to:

	Eventually("#footer").Should(tab.ScrollIntoView())

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) ScrollIntoView(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("scrollIntoView", selector).MatcherResult()
	}).WithMessage("be scrollable into view")
	if len(args) > 0 {
		b.pollOrImmediate(args[0], matcher)
		return nil
	}
	b.guardBareMatcher("ScrollIntoView")
	return matcher
}

/*
SelectText() selects all of the text inside the first element matching selector - the equivalent of dragging the cursor across it - and produces a genuine window.getSelection() range, then dispatches a mouseup so selection-driven UIs (a "highlight → menu" toolbar, an annotation layer) react.

When invoked with a selector, tab.SelectText("#passage") polls by default until the element is present and visible, then selects its text - failing the spec if that never happens before the timeout.  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with no arguments, tab.SelectText() returns a Gomega matcher so you can poll until an element is present to select:

	Eventually("#passage").Should(tab.SelectText())

To select just a substring of the element's text - useful for text-anchoring UIs where a word repeats - pass the substring, and optionally a [Biloba.Occurrence] to pick which appearance (1-based, defaults to the first):

	tab.SelectText("#passage", "chloroplast")                   // selects the 1st "chloroplast"
	tab.SelectText("#passage", "chloroplast", tab.Occurrence(2)) // selects the 2nd "chloroplast"

The substring form also has a matcher variant - note it requires an explicit tab.Occurrence so it is unambiguous with the select-all immediate form above:

	Eventually("#passage").Should(tab.SelectText("chloroplast", tab.Occurrence(2)))

To assert on what's selected, read the selection back with tab.EvaluateTo: Eventually("window.getSelection().toString()").Should(tab.EvaluateTo("the highlighted words")).  Clear it with tab.ClearSelection.

Read https://onsi.github.io/biloba/#selecting-text to learn more about selecting text
*/
func (b *Biloba) SelectText(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	var positional []any
	cfg := selectTextConfig{occurrence: 1}
	for _, arg := range args {
		if opt, ok := arg.(SelectTextOption); ok {
			opt(&cfg)
		} else {
			positional = append(positional, arg)
		}
	}
	hasOpts := len(positional) != len(args)

	switch {
	case len(positional) == 0 && !hasOpts:
		// matcher form, select all of the element's text; selector supplied by Eventually
		b.guardBareMatcher("SelectText")
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("selectText", selector).MatcherResult()
		}).WithMessage("be selectable")
	case len(positional) == 1 && !hasOpts:
		// immediate form, select all of the element's text
		matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("selectText", selector).MatcherResult()
		}).WithMessage("be selectable")
		b.pollOrImmediate(positional[0], matcher)
		return nil
	case len(positional) == 1 && hasOpts:
		// matcher form, select an occurrence of substring; selector supplied by Eventually
		substring := positional[0]
		b.guardBareMatcher("SelectText")
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("selectOccurrence", selector, substring, cfg.occurrence).MatcherResult()
		}).WithMessage("be selectable")
	case len(positional) == 2:
		// immediate form, select an occurrence of substring
		substring := positional[1]
		matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("selectOccurrence", selector, substring, cfg.occurrence).MatcherResult()
		}).WithMessage("be selectable")
		b.pollOrImmediate(positional[0], matcher)
		return nil
	default:
		b.gt.Fatalf("SelectText: unsupported arguments.  Use SelectText(selector), SelectText(selector, substring), or SelectText(selector, substring, tab.Occurrence(n)) to act immediately; or SelectText() and SelectText(substring, tab.Occurrence(n)) to return a matcher.")
		return nil
	}
}

type selectTextConfig struct {
	occurrence int
}

/*
SelectTextOption configures a [Biloba.SelectText] call.  Build one with [Biloba.Occurrence].
*/
type SelectTextOption func(*selectTextConfig)

/*
Occurrence(n) tells [Biloba.SelectText] which appearance of the substring to select (1-based) when a word repeats within the element:

	tab.SelectText("#passage", "chloroplast", tab.Occurrence(2)) // selects the 2nd "chloroplast"

Read https://onsi.github.io/biloba/#selecting-text to learn more about selecting text
*/
func (b *Biloba) Occurrence(n int) SelectTextOption {
	return func(c *selectTextConfig) { c.occurrence = n }
}

/*
SelectRange() selects a sub-range of the text inside the first element matching selector, by character offset, across the element's text nodes.  Like SelectText it produces a genuine window.getSelection() range and dispatches a mouseup.

The offsets count characters into the element's text content (start inclusive, end exclusive):

	tab.SelectRange("#passage", 5, 12) // selects characters 5..11

When invoked with a selector, start, and end it polls by default until the element is present and visible, then selects the range - failing the spec if that never happens before the timeout (or if [start, end] is out of bounds).  Opt out with [Biloba.Immediate], or tune the wait with [Biloba.WithTimeout]/[Biloba.WithPolling]/[Biloba.WithContext].

When invoked with just start and end, tab.SelectRange(start, end) returns a Gomega matcher whose subject is the selector:

	Eventually("#passage").Should(tab.SelectRange(5, 12))

Read https://onsi.github.io/biloba/#selecting-text to learn more about selecting text
*/
func (b *Biloba) SelectRange(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) == 3 {
		start, end := args[1], args[2]
		matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("selectRange", selector, start, end).MatcherResult()
		}).WithMessage("be selectable")
		b.pollOrImmediate(args[0], matcher)
		return nil
	}
	if len(args) != 2 {
		b.gt.Fatalf("SelectRange requires either (selector, start, end) to act immediately or (start, end) to return a matcher")
		return nil
	}
	start, end := args[0], args[1]
	b.guardBareMatcher("SelectRange")
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("selectRange", selector, start, end).MatcherResult()
	}).WithMessage("be selectable")
}

/*
ClearSelection() clears any active text selection on the tab (window.getSelection().removeAllRanges()):

	tab.ClearSelection()

It is the counterpart to SelectText/SelectRange and never fails for "nothing selected".

Read https://onsi.github.io/biloba/#selecting-text to learn more about selecting text
*/
func (b *Biloba) ClearSelection() {
	b.gt.Helper()
	b.guardConfig("ClearSelection")
	if _, err := b.RunErr("window.getSelection().removeAllRanges()"); err != nil {
		b.gt.Fatalf("Failed to clear selection:\n%s", err.Error())
	}
}

/*
InvokeOn() takes a selector, a method name, and optional arguments.  It will find the first element matching selector and invoke method on that option, passing in any arguments provided.  That is:

	tab.InvokeOn("input.login", "scrollIntoView")
	tab.InvokeOn(".notice", "setAttribute", "data-age", "17")

are equivalent to the javascript:

	document.querySelector("input.login")["scrollIntoView"]()
	document.querySelector(".notice")["setAttribute"]("data-age", "17")

InvokeOn polls by default: it waits until an element matching selector is present, then invokes method on it.  If the method is undefined on the element, or it throws, the error is surfaced when the poll times out (under [Biloba.Immediate] it fails fast on the first attempt instead).  Anything returned by method is returned by InvokeOn with type any:

	Expect(tab.InvokeOn(".notice", "getAttribute", "data-age")).To(Equal("17"))

Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#invoking-javascript-on-and-with-selected-elements to learn more about invoking javascript on/with DOM elements
*/
func (b *Biloba) InvokeOn(selector any, methodName string, args ...any) any {
	b.gt.Helper()
	finalArgs := []any{methodName}
	finalArgs = append(finalArgs, args...)
	var result any
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		r := b.runBilobaHandler("invokeOnP", sel, finalArgs...)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		result = r.Result
		return true, nil
	}).WithMessage(fmt.Sprintf("respond to %q", methodName))
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
InvokeOnEachImmediately() invokes the passed-in method, passing in the args if any, on all elements matching selector.  It returns a []any slice containing any return values from each invocation.

All invocations receive the same arguments.

See [Biloba.InvokeOn] for more details

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#invoking-javascript-on-and-with-selected-elements to learn more about invoking javascript on/with DOM elements
*/
func (b *Biloba) InvokeOnEachImmediately(selector string, methodName string, args ...any) []any {
	b.gt.Helper()
	b.guardConfig("InvokeOnEachImmediately")
	finalArgs := []any{methodName}
	finalArgs = append(finalArgs, args...)
	r := b.runBilobaHandler("invokeOnEach", selector, finalArgs...)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to invoke \"%s\" on each:\n%s", methodName, r.Error())
		return nil
	}
	return r.ResultAnySlice()
}

/*
InvokeWith() finds the first element matching selector then invokes the passed-in function, passing in the element and any additional args provided.  It returns anything returned by the function as type any.

callableScript must be a snippet of javascript that evaluates to a callable function.  For example:

	appendLi := `(el, text) => {
		let li = document.createElement('li')
		li.innerText = text
		el.appendChild(li);
	}`
	b.InvokeWith("ul", appendLi, "Another Item") //runs on the first <ul>
	b.InvokeWithEachImmediately("ul", appendLi, "Another Item For All") //runs on all <ul>s

InvokeWith polls by default: it waits until an element matching selector is present, then invokes the function.  If the script throws, the error is surfaced when the poll times out (under [Biloba.Immediate] it fails fast on the first attempt instead).  Configure the wait with WithTimeout/WithPolling/WithContext, or opt into act-once/fail-fast with Immediate().

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#invoking-javascript-on-and-with-selected-elements to learn more about invoking javascript on/with DOM elements
*/
func (b *Biloba) InvokeWith(selector any, callableScript string, args ...any) any {
	b.gt.Helper()
	finalArgs := []any{callableScript}
	finalArgs = append(finalArgs, args...)
	var result any
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		r := b.runBilobaHandler("invokeWithP", sel, finalArgs...)
		if r.Error() != nil {
			return false, r.Error()
		}
		if !r.Success {
			return false, nil
		}
		result = r.Result
		return true, nil
	}).WithMessage("be invokable")
	b.pollOrImmediate(selector, matcher)
	return result
}

/*
InvokeWithEachImmediately() finds all elements matching selector then invokes the passed-in function on each element, passing in the element and any additional args provided.  It collects the return values for each invocation and returns them as an []any slice.

See [Biloba.InvokeWith] for more details

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#invoking-javascript-on-and-with-selected-elements to learn more about invoking javascript on/with DOM elements
*/
func (b *Biloba) InvokeWithEachImmediately(selector string, callableScript string, args ...any) []any {
	b.gt.Helper()
	b.guardConfig("InvokeWithEachImmediately")
	finalArgs := []any{callableScript}
	finalArgs = append(finalArgs, args...)
	r := b.runBilobaHandler("invokeWithEach", selector, finalArgs...)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to InvokeWithEachImmediately:\n%s\n\n%s", callableScript, r.Error())
		return nil
	}
	return r.ResultAnySlice()
}

func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func matcherOrEqual(expected any) types.GomegaMatcher {
	var matcher types.GomegaMatcher
	switch v := expected.(type) {
	case types.GomegaMatcher:
		matcher = v
	default:
		if v == nil {
			matcher = gomega.BeNil()
		} else {
			matcher = gomega.Equal(v)
		}
	}
	return matcher
}
func nilSafeSlice(expected []any) []any {
	safeExpected := make([]any, len(expected))
	for i, v := range expected {
		if v == nil {
			safeExpected[i] = gomega.BeNil()
		} else {
			safeExpected[i] = v
		}
	}
	return safeExpected
}

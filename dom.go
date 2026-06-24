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

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) HasElement(selector any) bool {
	b.gt.Helper()
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

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) Count(selector any) int {
	b.gt.Helper()
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

/*
EachBeVisible() is a Gomega matcher that passes if every element returned by selector is visible.  It passes vacuously when no elements match.

Use it like this:

	Expect("div.comment").To(tab.EachBeVisible())
	Eventually("div.comment").Should(tab.EachBeVisible())

visibility is determined by non-zero offsetWidth and offsetHeight

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) EachBeVisible() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("eachIsVisible", selector).MatcherResult()
	}).WithMessage("each be visible")
}

/*
EachBeEnabled() is a Gomega matcher that passes if every element returned by selector is not disabled.  It passes vacuously when no elements match.

Use it like this:

	Expect("input").To(tab.EachBeEnabled())
	Eventually("button").Should(tab.EachBeEnabled())

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) EachBeEnabled() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("eachIsEnabled", selector).MatcherResult()
	}).WithMessage("each be enabled")
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
InnerText(selector) returns the innerText of the first element matching selector

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) InnerText(selector any) string {
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
InnerTextForEach(selector) returns a slice []string of innerText for each element matching selector

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) InnerTextForEach(selector any) []string {
	b.gt.Helper()
	return toStringSlice(b.GetPropertyForEach(selector, "innerText"))
}

/*
EachHaveInnerText(expected) is a Gomega matcher that passes if the []string slice of innerTexts for all matching elements returned by selector matches expected.  expected can be a []string, but you'll probably want to use a Gomega matcher

Use it like this:

	Eventually("div.comment").Should(tab.EachHaveInnerText(ContainElement("new comment")))
	//equivalent to, but tidier than
	Eventually(tab.InnerTextForEach).WithArgument("div.comment").Should(ContainElement("new comment"))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DO
M
*/
func (b *Biloba) EachHaveInnerText(args ...any) types.GomegaMatcher {
	if len(args) == 0 {
		args = []any{gomega.BeEmpty()}
	}
	return b.EachHaveProperty("innerText", args...)
}

/*
TextContent(selector) returns the textContent of the first element matching selector.

Unlike [Biloba.InnerText], textContent is computed straight from the DOM tree and does not depend on layout - which makes it reliable in headless Chrome for content that has just been added or changed (innerText can return a stale or partial value before a paint pass).  Note that textContent includes the text of hidden elements and of <script>/<style> children, and does not reflect CSS text-transform; reach for InnerText when you specifically need the rendered, visible text.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) TextContent(selector any) string {
	b.gt.Helper()
	return toString(b.GetProperty(selector, "textContent"))
}

/*
HaveTextContent(expected) is a Gomega matcher that passes if the first element returned by selector has textContent matching expected.  expected can be a string, or a Gomega matcher.

Prefer HaveTextContent over [Biloba.HaveInnerText] when asserting on dynamic content: textContent is layout-independent and so does not flake in headless Chrome the way innerText can.  See [Biloba.TextContent] for the semantic differences between the two.

Use it like this:

	Eventually("div.comment").Should(tab.HaveTextContent("hello world"))
	Eventually("div.comment").Should(tab.HaveTextContent(ContainSubstring("world")))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) HaveTextContent(expected any) types.GomegaMatcher {
	return b.HaveProperty("textContent", expected)
}

/*
TextContentForEach(selector) returns a slice []string of textContent for each element matching selector.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) TextContentForEach(selector any) []string {
	b.gt.Helper()
	return toStringSlice(b.GetPropertyForEach(selector, "textContent"))
}

/*
EachHaveTextContent(expected) is a Gomega matcher that passes if the []string slice of textContents for all matching elements returned by selector matches expected.  expected can be a []string, but you'll probably want to use a Gomega matcher.

Use it like this:

	Eventually("div.comment").Should(tab.EachHaveTextContent(ContainElement("new comment")))
	//equivalent to, but tidier than
	Eventually(tab.TextContentForEach).WithArgument("div.comment").Should(ContainElement("new comment"))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) EachHaveTextContent(args ...any) types.GomegaMatcher {
	if len(args) == 0 {
		args = []any{gomega.BeEmpty()}
	}
	return b.EachHaveProperty("textContent", args...)
}

/*
GetProperty(selector, property) returns the named javascript property from the first element matching selector

GetProperty will fail if no element is found.  It returns nil if property is not defined on the element.  Otherwise it returns the value of the property as type any - you'll need to do type assertions yourself.  Or just use a Gomega matcher to handle the types for you.

Dot-delimited properties are also support.  e.g.

	tab.GetProperty("div.comment", "dataset.poster")

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) GetProperty(selector any, property string) any {
	b.gt.Helper()
	r := b.runBilobaHandler("getProperty", selector, property)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get property \"%s\":\n%s", property, r.Error())
	}
	return r.Result
}

/*
GetPropertyForEach(selector, property) returns the requested property for all elements matching selector.  It returns []any and follows the rules of [Biloba.GetProperty].  If no elements are found an empty slice is returned.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) GetPropertyForEach(selector any, property string) []any {
	b.gt.Helper()
	r := b.runBilobaHandler("getPropertyForEach", selector, property)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get property \"%s\" for each:\n%s", property, r.Error())
	}
	return r.ResultAnySlice()
}

/*
GetAttribute(selector, name) returns the named HTML attribute of the first element matching selector.  It returns the raw attribute string, or nil when the attribute is not present.

GetAttribute is the immediate sibling of the [Biloba.HaveAttribute] matcher - reach for it when you want an attribute value in a Go variable for control-flow rather than to assert on.  Unlike [Biloba.GetProperty], it reads the raw markup attribute (e.g. the literal href="/about") rather than the resolved DOM property (the absolute URL).

	tab.GetAttribute("#link", "href") // "/about"

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties and attributes
*/
func (b *Biloba) GetAttribute(selector any, name string) any {
	b.gt.Helper()
	r := b.runBilobaHandler("getAttribute", selector, name)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get attribute \"%s\":\n%s", name, r.Error())
	}
	return r.Result
}

/*
GetAttributeForEach(selector, name) returns the named HTML attribute for all elements matching selector.  It returns []any and follows the rules of [Biloba.GetAttribute] - nil entries stand in for elements that lack the attribute.  If no elements are found an empty slice is returned.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties and attributes
*/
func (b *Biloba) GetAttributeForEach(selector any, name string) []any {
	b.gt.Helper()
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
EachHaveProperty() is a Gomega matcher with two modes of operation:

When invoked with only one argument, it passes only if all elements matching selector have the requested javascript property defined on them:

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
			return r.Success, nil
		}).WithTemplate("Expected each {{.Actual}} {{.To}} each have property \"{{.Data.Property}}\"", data)
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
			return matcher.Match(data["Result"])
		}).WithTemplate("EachHaveProperty \"{{.Data.Property}}\" for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
	}
}

/*
SetProperty() has two modes of operation:

When invoked with a selector and two arguments:

	tab.SetProperty(selector, property, value)

it immediately sets the specified property on the first element matching selector to value.  If no element is found, tab.SetProperty fails the spec.  property must have type string but value can be any type

When invoked with just two arguments, tab.SetProperty returns a Gomega matcher that will only succeed once an element is found and its property set:

	Eventually("div.comment").Should(tab.SetProperty("dataset.poster", "George"))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) SetProperty(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) == 3 {
		r := b.runBilobaHandler("setProperty", args[0], args[1], args[2])
		if r.Error() != nil {
			b.gt.Fatalf("Failed to set property \"%s\":\n%s", args[1], r.Error())
		}
		return nil
	} else {
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("setProperty", selector, args[0], args[1]).MatcherResult()
		}).WithMessage("be property-settable")
	}
}

/*
SetPropertyForEach() sets the specified property to the specified value on all DOM elements matching selector. It does nothing if no elements match.

Like the rest of the *Each family it acts immediately and has no matcher form - it does not poll.  Gate it on the matches being present first (e.g. Eventually(selector).Should(b.HaveCount(n))) when they appear asynchronously.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) SetPropertyForEach(selector any, property string, value any) {
	b.gt.Helper()
	r := b.runBilobaHandler("setPropertyForEach", selector, property, value)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to set property \"%s\" for each:\n%s", property, r.Error())
	}
}

/*
GetProperties() returns a [Properties] struct containing multiple properties from the first DOM element selected by selector.  If no DOM element matches, GetProperties() fails.  If any of the requested properties don't exist - those properties will be set to nil.

	p := GetProperties(".notice", "tagName", "classList", "dataset", "disabled")
	p.GetString("tagName") //"DIV"
	p.GetStringSlice("classList") //[]string{"notice", "highlight"}
	p.GetBool("disabled") //false

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) GetProperties(selector any, properties ...string) Properties {
	b.gt.Helper()
	if len(properties) == 0 {
		b.gt.Fatalf("GetProperties requires at least one property to fetch")
		return nil
	}
	r := b.runBilobaHandler("getProperties", selector, properties)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get properties %s:\n%s", strings.Join(properties, ", "), r.Error())
		return nil
	}
	return newProperties(r.Result)
}

/*
GetPropertiesForEach() returns a [SliceOfProperties] - i.e. a slice of [Properties] - from each DOM element selected by selector.  If no DOM element matches, GetPropertiesForEach() returns an empty SliceOfProperties.  If any of the requested properties don't exist - those individual properties will be set to nil.

	p := GetPropertiesForEach(".notice", "tagName", "classList", "dataset", "disabled")
	p.GetString("tagName") //[]string{"DIV", "DIV", "DIV"}
	p.GetStringSlice("classList") //[][]string{{"notice", "highlight"}, {"notice", "gray"}, {"notice"}}
	p.GetBool("disabled") //[]bool{false, true, false}

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#properties to learn more about working with properties
*/
func (b *Biloba) GetPropertiesForEach(selector any, properties ...string) SliceOfProperties {
	b.gt.Helper()
	if len(properties) == 0 {
		b.gt.Fatalf("GetPropertiesForEach requires at least one property to fetch")
		return nil
	}
	r := b.runBilobaHandler("getPropertiesForEach", selector, properties)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get properties %s for each:\n%s", strings.Join(properties, ", "), r.Error())
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

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#form-elements to learn more about working with form elements
*/
func (b *Biloba) GetValue(selector any) any {
	b.gt.Helper()
	r := b.runBilobaHandler("getValue", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get value:\n%s", r.Error())
	}
	return r.Result
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

it immediately sets the value on the first element matching selector.  If no element is found, tab.SetValue fails the spec.  The element must be visible and enabled.

When invoked with just one argument, tab.SetValue returns a Gomega matcher that will only succeed once an element is found, is visible, and is enabled and its value gets set:

	Eventually("input[type='checkbox']").Should(tab.SetValue(true))

the types you provide `SetValue` will depend on the type of input you are addressing.  See [Biloba.GetValue] for examples.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#form-elements to learn more about working with form elements
*/
func (b *Biloba) SetValue(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) == 2 {
		if b.realistic {
			ok, err := b.realisticSetValue(args[0], args[1])
			if err != nil {
				b.gt.Fatalf("Failed to set value:\n%s", err.Error())
			} else if !ok {
				b.gt.Fatalf("Failed to set value: element is not settable (not visible, enabled, in view, or unobscured)")
			}
			return nil
		}
		r := b.runBilobaHandler("setValue", args[0], args[1])
		if r.Error() != nil {
			b.gt.Fatalf("Failed to set value:\n%s", r.Error())
		}
		return nil
	}
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
EachHaveClass returns a Gomega matcher to assert that every element matching selector has the expected class.  It passes vacuously when no elements match.

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
			return true, nil // vacuously true when no elements match
		}
		return matcher.Match(data["Result"])
	}).WithTemplate("EachHaveClass for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
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

it immediately clicks the first element matching selector.  It fails if no element is found, or if the element is hidden or disabled.

When invoked with no selector, tab.Click() returns a Gomega matcher.  This allows you to poll until an element is clickable (exists, is visible, and is enabled):

	Eventually("#submit").Should(tab.Click())

Both forms accept [PointerOption]s after the selector (or in place of it, for the matcher) to target an offset and/or hold keyboard modifiers:

	tab.Click("#canvas", b.At(30, 40), b.Shift())
	Eventually("#canvas").Should(tab.Click(b.At(30, 40), b.Shift()))

A plain tab.Click(selector) dispatches the maximally-faithful native element.click().  Passing any option instead dispatches synthetic mousedown/mouseup/click MouseEvents carrying the coordinates and modifier flags (realistic mode always uses real CDP input) - a deliberate fidelity-for-control tradeoff.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) Click(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("click", "element is not clickable (it is disabled, off-screen, or obscured by another element)", "be clickable", args, b.performClick)
}

/*
ClickEach() clicks on every DOM element matching selector that is visible and enabled.

If no elements match, nothing happens.

Like the rest of the *Each family it acts immediately and has no matcher form - it does not poll.  Gate it on the matches being present first (e.g. Eventually(selector).Should(b.HaveCount(n))) when they appear asynchronously.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) ClickEach(selector any) {
	b.gt.Helper()
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

it immediately double-clicks (fast mode fires two click events plus a dblclick event; realistic mode dispatches a real double mouse click).  It fails if no element is found, or if the element is hidden or disabled.

When invoked with no selector, tab.DblClick() returns a Gomega matcher:

	Eventually("#row").Should(tab.DblClick())

Like Click, it accepts [PointerOption]s (b.At/b.Shift/...) after the selector or in place of it.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) DblClick(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("double-click", "element is not clickable (it is disabled, off-screen, or obscured by another element)", "be double-clickable", args, b.performDblClick)
}

/*
RightClick() right-clicks (context-clicks) the first element matching selector.

	tab.RightClick("#row")

it immediately right-clicks (fast mode dispatches mousedown/mouseup/contextmenu events; realistic mode dispatches a real right-button mouse click).  It fails if no element is found, or if the element is hidden or disabled.

When invoked with no selector, tab.RightClick() returns a Gomega matcher:

	Eventually("#row").Should(tab.RightClick())

Like Click, it accepts [PointerOption]s (b.At/b.Shift/...) after the selector or in place of it.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) RightClick(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("right-click", "element is not clickable (it is disabled, off-screen, or obscured by another element)", "be right-clickable", args, b.performRightClick)
}

/*
MiddleClick() middle-clicks (auxiliary-clicks) the first element matching selector.

	tab.MiddleClick("#row")

it immediately middle-clicks (fast mode dispatches mousedown/mouseup/auxclick events; realistic mode dispatches a real middle-button mouse click).  It fails if no element is found, or if the element is hidden or disabled.

When invoked with no selector, tab.MiddleClick() returns a Gomega matcher:

	Eventually("#row").Should(tab.MiddleClick())

Like Click, it accepts [PointerOption]s (b.At/b.Shift/...) after the selector or in place of it.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) MiddleClick(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("middle-click", "element is not clickable (it is disabled, off-screen, or obscured by another element)", "be middle-clickable", args, b.performMiddleClick)
}

/*
Tap() taps (touches) the first element matching selector.

	tab.Tap("#row")

it immediately taps (fast mode dispatches synthetic touch and pointer events plus a click; realistic mode dispatches a real CDP touch).  It fails if no element is found, or if the element is hidden or disabled.

When invoked with no selector, tab.Tap() returns a Gomega matcher:

	Eventually("#row").Should(tab.Tap())

It accepts a b.At(x, y) [PointerOption] to tap at an offset; keyboard modifiers don't apply to touch and are ignored.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Tap(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("tap", "element is not tappable (it is disabled, off-screen, or obscured by another element)", "be tappable", args, b.performTap)
}

/*
DragTo() has two modes of operation:

When invoked with a source and a target selector:

	tab.DragTo("#card", "#column")

it immediately drags source's center onto target's center with a pointer-based drag sequence (pointerdown/pointermove/pointerup plus the matching mouse events; realistic mode dispatches the drag with real CDP mouse input).  It is meant for pointer-based drag-and-drop libraries (@dnd-kit and the like); it does NOT drive native HTML5 draggable - for that, drop to chromedp via tab.Context.  It fails if either element is not found, or if source is hidden.

When invoked with just a target, tab.DragTo() returns a Gomega matcher whose subject is the source.  This lets you poll until both source and target are present and the drag can be performed - folding the wait into the action so you don't have to assert both endpoints exist first:

	Eventually("#card").Should(tab.DragTo("#column"))

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) DragTo(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) >= 2 {
		ok, err := b.performDrag(args[0], args[1])
		if err != nil {
			b.gt.Fatalf("Failed to drag:\n%s", err.Error())
		} else if !ok {
			b.gt.Fatalf("Failed to drag: source or target is not actionable (it is disabled, off-screen, or obscured by another element)")
		}
		return nil
	}
	target := args[0]
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

it immediately dispatches a wheel event at the element's center and scrolls the nearest scrollable ancestor (realistic mode dispatches a real CDP wheel event that scrolls via genuine trusted input).  It fails if the element is not found or is hidden.

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
		success, err := b.performScrollWheel(args[0], dx, dy)
		if err != nil {
			b.gt.Fatalf("Failed to scroll wheel:\n%s", err.Error())
		} else if !success {
			b.gt.Fatalf("Failed to scroll wheel: element is not visible")
		}
		return nil
	case 2:
		dx, okX := asFloat64(args[0])
		dy, okY := asFloat64(args[1])
		if !okX || !okY {
			b.gt.Fatalf("ScrollWheel requires numeric deltaX and deltaY")
			return nil
		}
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

When invoked with a selector, tab.Focus("input.search") acts immediately and fails the spec if no element is found, or if the element is hidden or disabled.

When invoked with no arguments, tab.Focus() returns a Gomega matcher so you can poll until an element is focusable:

	Eventually("input.search").Should(tab.Focus())

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Focus(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) > 0 {
		r := b.runBilobaHandler("focus", args[0])
		if r.Error() != nil {
			b.gt.Fatalf("Failed to focus:\n%s", r.Error())
		}
		return nil
	}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("focus", selector).MatcherResult()
	}).WithMessage("be focusable")
}

/*
Blur() blurs (removes focus from) the first element matching selector.

This is useful when you want to explicitly trigger a blur handler - for example an input that commits its value or validates onBlur.  Note that [Biloba.SetValue] does not blur text inputs, so reach for Blur() when you need that behavior:

	tab.SetValue("input.name", "New Name")
	tab.Blur("input.name") // fires the onBlur commit handler

Note that a blur event only fires if the element is actually focused; [Biloba.SetValue] leaves the text input it sets focused, so the example above works.

When invoked with a selector, tab.Blur("input.search") acts immediately and fails the spec if no element is found.

When invoked with no arguments, tab.Blur() returns a Gomega matcher so you can poll until an element is present to blur:

	Eventually("input.search").Should(tab.Blur())

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Blur(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) > 0 {
		r := b.runBilobaHandler("blur", args[0])
		if r.Error() != nil {
			b.gt.Fatalf("Failed to blur:\n%s", r.Error())
		}
		return nil
	}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("blur", selector).MatcherResult()
	}).WithMessage("be blurrable")
}

/*
Hover() dispatches the pointer/mouse events associated with hovering (pointerover, mouseover, pointerenter, mouseenter, mousemove) at the first element matching selector.

Like all of Biloba's interactions this is a pragmatic simulation, not a real pointer: it fires synthetic events synchronously and atomically in the browser.  That means it triggers JavaScript hover handlers (e.g. a menu that opens on mouseenter) but does not activate CSS :hover styling - for that you'll need to drop down to chromedp's input domain.

When invoked with a selector, tab.Hover(".menu") acts immediately and fails the spec if no element is found, or if the element is hidden.

When invoked with no arguments, tab.Hover() returns a Gomega matcher so you can poll until an element is hoverable:

	Eventually(".menu").Should(tab.Hover())

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Hover(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	return b.pointerInteraction("hover", "element is off-screen", "be hoverable", args, b.performHover)
}

/*
ScrollIntoView() scrolls the first element matching selector into view (via the element's scrollIntoView()).

When invoked with a selector, tab.ScrollIntoView("#footer") acts immediately and fails the spec if no element is found.

When invoked with no arguments, tab.ScrollIntoView() returns a Gomega matcher so you can poll until an element is present to scroll to:

	Eventually("#footer").Should(tab.ScrollIntoView())

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) ScrollIntoView(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) > 0 {
		r := b.runBilobaHandler("scrollIntoView", args[0])
		if r.Error() != nil {
			b.gt.Fatalf("Failed to scroll into view:\n%s", r.Error())
		}
		return nil
	}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("scrollIntoView", selector).MatcherResult()
	}).WithMessage("be scrollable into view")
}

/*
SelectText() selects all of the text inside the first element matching selector - the equivalent of dragging the cursor across it - and produces a genuine window.getSelection() range, then dispatches a mouseup so selection-driven UIs (a "highlight → menu" toolbar, an annotation layer) react.

When invoked with a selector, tab.SelectText("#passage") acts immediately and fails the spec if no element is found, or if the element is hidden.

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
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("selectText", selector).MatcherResult()
		}).WithMessage("be selectable")
	case len(positional) == 1 && !hasOpts:
		// immediate form, select all of the element's text
		r := b.runBilobaHandler("selectText", positional[0])
		if r.Error() != nil {
			b.gt.Fatalf("Failed to select text:\n%s", r.Error())
		}
		return nil
	case len(positional) == 1 && hasOpts:
		// matcher form, select an occurrence of substring; selector supplied by Eventually
		substring := positional[0]
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("selectOccurrence", selector, substring, cfg.occurrence).MatcherResult()
		}).WithMessage("be selectable")
	case len(positional) == 2:
		// immediate form, select an occurrence of substring
		r := b.runBilobaHandler("selectOccurrence", positional[0], positional[1], cfg.occurrence)
		if r.Error() != nil {
			b.gt.Fatalf("Failed to select text:\n%s", r.Error())
		}
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

When invoked with a selector, start, and end it acts immediately and fails the spec if no element is found, if the element is hidden, or if [start, end] is out of bounds.

When invoked with just start and end, tab.SelectRange(start, end) returns a Gomega matcher whose subject is the selector:

	Eventually("#passage").Should(tab.SelectRange(5, 12))

Read https://onsi.github.io/biloba/#selecting-text to learn more about selecting text
*/
func (b *Biloba) SelectRange(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) == 3 {
		r := b.runBilobaHandler("selectRange", args[0], args[1], args[2])
		if r.Error() != nil {
			b.gt.Fatalf("Failed to select range:\n%s", r.Error())
		}
		return nil
	}
	if len(args) != 2 {
		b.gt.Fatalf("SelectRange requires either (selector, start, end) to act immediately or (start, end) to return a matcher")
		return nil
	}
	start, end := args[0], args[1]
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

InvokeOn() fails if no element is found, or if the method is not defined on the element. Anything returned by method is returned by InvokeOn with type any:

	Expect(document.InvokeOn("getAttribute", "data-age")).To(Equal("17"))

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#invoking-javascript-on-and-with-selected-elements to learn more about invoking javascript on/with DOM elements
*/
func (b *Biloba) InvokeOn(selector string, methodName string, args ...any) any {
	b.gt.Helper()
	finalArgs := []any{methodName}
	finalArgs = append(finalArgs, args...)
	r := b.runBilobaHandler("invokeOn", selector, finalArgs...)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to invoke \"%s\":\n%s", methodName, r.Error())
		return nil
	}
	return r.Result
}

/*
InvokeOnEach() invokes the passed-in method, passing in the args if any, on all elements matching selector.  It returns a []any slice containing any return values from each invocation.

All invocations receive the same arguments.

See [Biloba.InvokeOn] for more details

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#invoking-javascript-on-and-with-selected-elements to learn more about invoking javascript on/with DOM elements
*/
func (b *Biloba) InvokeOnEach(selector string, methodName string, args ...any) []any {
	b.gt.Helper()
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
	b.InvokeWithEach("ul", appendLi, "Another Item For All") //runs on all <ul>s

# InvokeWith fails if no element is found

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#invoking-javascript-on-and-with-selected-elements to learn more about invoking javascript on/with DOM elements
*/
func (b *Biloba) InvokeWith(selector string, callableScript string, args ...any) any {
	b.gt.Helper()
	finalArgs := []any{callableScript}
	finalArgs = append(finalArgs, args...)
	r := b.runBilobaHandler("invokeWith", selector, finalArgs...)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to InvokeWith:\n%s", r.Error())
		return nil
	}
	return r.Result
}

/*
InvokeWithEach() finds all elements matching selector then invokes the passed-in function on each element, passing in the element and any additional args provided.  It collects the return values for each invocation and returns them as an []any slice.

See [Biloba.InvokeWith] for more details

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
Read https://onsi.github.io/biloba/#invoking-javascript-on-and-with-selected-elements to learn more about invoking javascript on/with DOM elements
*/
func (b *Biloba) InvokeWithEach(selector string, callableScript string, args ...any) []any {
	b.gt.Helper()
	finalArgs := []any{callableScript}
	finalArgs = append(finalArgs, args...)
	r := b.runBilobaHandler("invokeWithEach", selector, finalArgs...)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to InvokeWithEach:\n%s", callableScript, r.Error())
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

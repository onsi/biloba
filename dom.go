package biloba

import (
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
	return fmt.Errorf(r.Err)
}
func (r *bilobaJSResponse) MatcherResult() (bool, error) { return r.Success, r.Error() }
func (r *bilobaJSResponse) ResultString() string         { return toString(r.Result) }
func (r *bilobaJSResponse) ResultInt() int               { return toInt(r.Result) }
func (r *bilobaJSResponse) ResultBool() bool             { return toBool(r.Result) }
func (r *bilobaJSResponse) ResultStringSlice() []string  { return toStringSlice(r.Result) }
func (r *bilobaJSResponse) ResultAnySlice() []any        { return toAnySlice(r.Result) }

func (b *Biloba) runBilobaHandler(name string, selector any, args ...any) *bilobaJSResponse {
	b.ensureBiloba()
	result := &bilobaJSResponse{}
	parameters := []any{}
	switch x := selector.(type) {
	case XPath:
		parameters = append(parameters, "x"+string(x))
	case string:
		if x[0] == '/' {
			parameters = append(parameters, "x"+x)
		} else {
			parameters = append(parameters, "s"+x)
		}
	default:
		result.Err = fmt.Sprintf("invalid selector type %T", x)
		return result
	}
	parameters = append(parameters, args...)
	_, err := b.RunErr(b.JSFunc("_biloba."+name).Invoke(parameters...), result)
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
		r := b.runBilobaHandler("setValue", args[0], args[1])
		if r.Error() != nil {
			b.gt.Fatalf("Failed to set value:\n%s", r.Error())
		}
		return nil
	} else {
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("setValue", selector, args[0]).MatcherResult()
		}).WithMessage("be value-settable")
	}
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
Click() has two modes of operation:

When invoked with a selector:

	tab.Click("#submit")

it immediately clicks the first element matching selector.  It fails if no element is found, or if the element is hidden or disabled.

When invoked with no arguments, tab.Click() returns a Gomega matcher.  This allows you to poll until an element is clickable (exists, is visible, and is enabled):

	Eventually("#submit").Should(tab.Click())

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) Click(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) > 0 {
		r := b.runBilobaHandler("click", args[0])
		if r.Error() != nil {
			b.gt.Fatalf("Failed to click:\n%s", r.Error())
		}
		return nil
	} else {
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("click", selector).MatcherResult()
		}).WithMessage("be clickable")
	}
}

/*
ClickEach() clicks on every DOM element matching selector that is visible and enabled.

If no elements match, nothing happens.

Read https://onsi.github.io/biloba/#working-with-the-dom to learn more about selectors and handling the DOM
*/
func (b *Biloba) ClickEach(selector any) {
	b.gt.Helper()
	r := b.runBilobaHandler("clickEach", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to click each:\n%s", r.Error())
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

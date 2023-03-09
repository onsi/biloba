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

func (b *Biloba) HasElement(selector any) bool {
	b.gt.Helper()
	r := b.runBilobaHandler("exists", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to check if element exists:\n%s", r.Error())
	}
	return r.Success
}

func (b *Biloba) Exist() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("exists", selector).MatcherResult()
	}).WithMessage("exist")
}

func (b *Biloba) Count(selector any) int {
	b.gt.Helper()
	r := b.runBilobaHandler("count", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to count elements:\n%s", r.Error())
	}
	return r.ResultInt()
}

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

func (b *Biloba) BeVisible() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("isVisible", selector).MatcherResult()
	}).WithMessage("be visible")
}

func (b *Biloba) BeEnabled() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("isEnabled", selector).MatcherResult()
	}).WithMessage("be enabled")
}

func (b *Biloba) InnerText(selector any) string {
	b.gt.Helper()
	return toString(b.GetProperty(selector, "innerText"))
}

func (b *Biloba) HaveInnerText(expected any) types.GomegaMatcher {
	return b.HaveProperty("innerText", expected)
}

func (b *Biloba) InnerTextForEach(selector any) []string {
	b.gt.Helper()
	return toStringSlice(b.GetPropertyForEach(selector, "innerText"))
}

func (b *Biloba) EachHaveInnerText(args ...any) types.GomegaMatcher {
	if len(args) == 0 {
		args = []any{gomega.BeEmpty()}
	}
	return b.EachHaveProperty("innerText", args...)
}

func (b *Biloba) GetProperty(selector any, property string) any {
	b.gt.Helper()
	r := b.runBilobaHandler("getProperty", selector, property)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get property \"%s\":\n%s", property, r.Error())
	}
	return r.Result
}

func (b *Biloba) GetPropertyForEach(selector any, property string) []any {
	b.gt.Helper()
	r := b.runBilobaHandler("getPropertyForEach", selector, property)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get property \"%s\" for each:\n%s", property, r.Error())
	}
	return r.ResultAnySlice()
}

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

func (b *Biloba) EachHaveProperty(property string, args ...any) types.GomegaMatcher {
	var data = map[string]any{}
	data["Property"] = property
	if len(args) == 0 {
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			r := b.runBilobaHandler("eachHasProperty", selector, property)
			if r.Error() != nil {
				return false, r.Error()
			}
			return r.Success, nil
		}).WithTemplate("Expected each {{.Actual}} {{.To}} each have property \"{{.Data.Property}}\"", data)
	} else {
		var matcher types.GomegaMatcher
		if x, ok := args[0].(types.GomegaMatcher); ok && len(args) == 1 {
			matcher = x
		} else {
			matcher = gomega.HaveExactElements(nilSafeSlice(args)...)
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

func (b *Biloba) SetPropertyForEach(selector any, property string, value any) {
	b.gt.Helper()
	r := b.runBilobaHandler("setPropertyForEach", selector, property, value)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to set property \"%s\" for each:\n%s", property, r.Error())
	}
}

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

func (b *Biloba) GetValue(selector any) any {
	b.gt.Helper()
	r := b.runBilobaHandler("getValue", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get value:\n%s", r.Error())
	}
	return r.Result
}

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
func toString(input any) string {
	if input == nil {
		return ""
	}
	return input.(string)
}
func toBool(input any) bool {
	if input == nil {
		return false
	}
	return input.(bool)
}
func toInt(input any) int {
	if input == nil {
		return 0
	}
	return int(input.(float64))
}
func toFloat64(input any) float64 {
	if input == nil {
		return 0
	}
	return input.(float64)
}
func toAnySlice(input any) []any {
	if input == nil {
		return []any{}
	}
	return input.([]any)
}
func toStringSlice(input any) []string {
	if input == nil {
		return []string{}
	}
	vs := input.([]any)
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = toString(v)
	}
	return out
}

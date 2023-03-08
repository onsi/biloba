package biloba

import (
	"fmt"

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
func (r *bilobaJSResponse) ResultString() string {
	if r.Result == nil {
		return ""
	}
	return r.Result.(string)
}
func (r *bilobaJSResponse) ResultBool() bool {
	if r.Result == nil {
		return false
	}
	return r.Result.(bool)
}
func (r *bilobaJSResponse) ResultStringSlice() []string {
	if r.Result == nil {
		return []string{}
	}
	out := []string{}
	for _, el := range r.Result.([]any) {
		out = append(out, el.(string))
	}
	return out
}

func (r *bilobaJSResponse) ResultAnySlice() []any {
	if r.Result == nil {
		return []any{}
	}
	return r.Result.([]any)
}

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
	r := b.runBilobaHandler("getInnerText", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get inner text:\n%s", r.Error())
	}
	return r.ResultString()
}

func (b *Biloba) HaveInnerText(expected interface{}) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("getInnerText", selector)
		if r.Error() != nil {
			return false, r.Error()
		}
		data["Result"] = r.ResultString()
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveInnerText for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

func (b *Biloba) InnerTexts(selector any) []string {
	b.gt.Helper()
	r := b.runBilobaHandler("getInnerTexts", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get inner texts:\n%s", r.Error())
	}
	return r.ResultStringSlice()
}

func (b *Biloba) HaveInnerTexts(args ...any) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher types.GomegaMatcher
	if len(args) == 0 {
		matcher = gomega.BeEmpty()
	} else if x, ok := args[0].(types.GomegaMatcher); ok && len(args) == 1 {
		matcher = x
	} else {
		matcher = gomega.HaveExactElements(nilSafeSlice(args)...)
	}
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("getInnerTexts", selector)
		if r.Error() != nil {
			return false, r.Error()
		}
		data["Result"] = r.ResultStringSlice()
		return matcher.Match(data["Result"])
	}).WithTemplate("HaveInnerText for {{.Actual}}:\n{{if .Failure}}{{.Data.Matcher.FailureMessage .Data.Result}}{{else}}{{.Data.Matcher.NegatedFailureMessage .Data.Result}}{{end}}", data)
}

func (b *Biloba) GetProperty(selector any, property string) any {
	b.gt.Helper()
	r := b.runBilobaHandler("getProperty", selector, property)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get property \"%s\":\n%s", property, r.Error())
	}
	return r.Result
}

func (b *Biloba) GetPropertyFromEach(selector any, property string) []any {
	b.gt.Helper()
	r := b.runBilobaHandler("getPropertyFromEach", selector, property)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get property \"%s\" from each:\n%s", property, r.Error())
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
		}).WithTemplate("Expected all {{.Actual}} {{.To}} each have property \"{{.Data.Property}}\"", data)
	} else {
		var matcher types.GomegaMatcher
		if x, ok := args[0].(types.GomegaMatcher); ok && len(args) == 1 {
			matcher = x
		} else {
			matcher = gomega.HaveExactElements(nilSafeSlice(args)...)
		}

		data["Matcher"] = matcher
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			r := b.runBilobaHandler("getPropertyFromEach", selector, property)
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
		b.gt.Fatalf("Failed to set property \"%s\" for all:\n%s", property, r.Error())
	}
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

func (b *Biloba) HaveClass(expected interface{}) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = gomega.ContainElement(expected)
	data["Matcher"] = matcher
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		r := b.runBilobaHandler("getClassList", selector)
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

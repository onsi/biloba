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
	Result  any    `json: "result"`
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

func (b *Biloba) runBilobaHandler(name string, selector any, args ...any) *bilobaJSResponse {
	b.ensureBiloba()
	result := &bilobaJSResponse{}
	parameters := []string{}
	switch x := selector.(type) {
	case XPath:
		parameters = append(parameters, `"x`+string(x)+`"`)
	case string:
		if x[0] == '/' {
			parameters = append(parameters, `"x`+x+`"`)
		} else {
			parameters = append(parameters, `"s`+x+`"`)
		}
	default:
		result.Err = fmt.Sprintf("invalid selector type %T", x)
		return result
	}
	for _, arg := range args {
		switch x := arg.(type) {
		case string:
			parameters = append(parameters, `"`+x+`"`)
		case float64:
			parameters = append(parameters, fmt.Sprintf("%f", x))
		case int:
			parameters = append(parameters, fmt.Sprintf("%d", x))
		case bool:
			if x {
				parameters = append(parameters, "true")
			} else {
				parameters = append(parameters, "false")
			}
		}
	}
	_, err := b.RunErr("_biloba."+name+"("+strings.Join(parameters, ", ")+")", result)
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

func (b *Biloba) GetProperty(selector any, property string) any {
	b.gt.Helper()
	r := b.runBilobaHandler("getProperty", selector, property)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get property %s:\n%s", property, r.Error())
	}
	return r.Result
}

func (b *Biloba) HaveProperty(property string, expected interface{}) types.GomegaMatcher {
	var data = map[string]any{}
	var matcher = matcherOrEqual(expected)
	data["Property"] = property
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

func (b *Biloba) IsChecked(selector any) bool {
	b.gt.Helper()
	r := b.runBilobaHandler("isChecked", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to determine if checked:\n%s", r.Error())
	}
	return r.Success
}

func (b *Biloba) BeChecked() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("isChecked", selector).MatcherResult()
	}).WithMessage("be checked")
}

func (b *Biloba) SetChecked(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) == 2 {
		r := b.runBilobaHandler("setChecked", args[0], args[1])
		if r.Error() != nil {
			b.gt.Fatalf("Failed to set checked:\n%s", r.Error())
		}
		return nil
	} else {
		return gcustom.MakeMatcher(func(selector any) (bool, error) {
			return b.runBilobaHandler("setChecked", selector, args[0]).MatcherResult()
		}).WithMessage("be checkable")
	}
}

func (b *Biloba) GetValue(selector any) string {
	b.gt.Helper()
	r := b.runBilobaHandler("getValue", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to get value:\n%s", r.Error())
	}
	return r.ResultString()
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
		data["Result"] = r.ResultString()
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

func matcherOrEqual(expected interface{}) types.GomegaMatcher {
	var matcher types.GomegaMatcher
	switch v := expected.(type) {
	case types.GomegaMatcher:
		matcher = v
	default:
		matcher = gomega.Equal(v)
	}
	return matcher
}

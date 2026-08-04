package biloba

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
)

/*
ValueMatcher is the concrete type returned by every Biloba matcher that *reads a value off the page*
(HaveAttribute, HaveProperty, HaveInnerText, HaveCount, EvaluateTo, HaveBoundingBox, ...).  It is a
plain Gomega matcher - use it exactly as before - with one extra affordance: [ValueMatcher.Capture]
hands you the value the matcher just observed.

Matchers that read no value (Exist, BeVisible, the relational geometry matchers) and the
actions-as-matchers (Click, SetValue, ...) keep returning a bare types.GomegaMatcher, so .Capture()
does not compile on them.

Read https://onsi.github.io/biloba/#capturing-values-from-matchers to learn more about capturing values
*/
type ValueMatcher struct {
	matcher  types.GomegaMatcher
	observed func() any
	target   any
}

// capturable wraps matcher so callers can chain .Capture(&x).  observed is consulted only when a
// capture target has been set and matcher has just succeeded - it returns whatever the matcher read
// off the page on that attempt.
func capturable(matcher types.GomegaMatcher, observed func() any) *ValueMatcher {
	return &ValueMatcher{matcher: matcher, observed: observed}
}

// capturableResult is the common case: the matcher already stashes what it observed in data["Result"]
// so its failure template can render it.  Capture reads the same slot.
func capturableResult(matcher types.GomegaMatcher, data map[string]any) *ValueMatcher {
	return capturable(matcher, func() any { return data["Result"] })
}

/*
Capture(&target) records the value this matcher observes into target as a side effect of a successful
match, so you can poll for a condition and keep the value that satisfied it - in one read:

	var blockID string
	Eventually(".figure-frame").Should(b.HaveAttribute("data-block-id", Not(BeEmpty())).Capture(&blockID))

	var log []FoldEntry
	Eventually(`window.__foldLog`).Should(b.EvaluateTo(ContainElement(HaveKeyWithValue("fidelity", "text"))).Capture(&log))

Note the asymmetry in that second example: the matcher sees the raw JSON-decoded value (a []any of
map[string]any, so match on it with HaveKeyWithValue rather than HaveField) while Capture is what
hands you the typed []FoldEntry.

This is the fix for gate-then-re-read: asserting with a matcher and *then* calling a getter for the
value is two reads of a page that may have changed in between (the classic TOCTOU shape - a repaint
between the two leaves you holding a value nothing ever asserted on).  Capture gives you the value
from the winning read itself.

Capture writes only when the matcher matches.  Under ShouldNot/NotTo the assertion passes precisely
when the matcher did NOT match, so nothing is captured - capture the value with a separate Should if
you need it.  While polling, target is overwritten on every successful attempt; when the assertion
finally passes it holds the value from the attempt that passed.

Capture returns a NEW matcher and leaves the receiver alone, so a matcher held in a variable can be
reused with different targets without the second Capture stealing the first's:

	m := b.HaveAttribute("data-id", Not(BeEmpty()))
	Eventually("#a").Should(m.Capture(&idA))
	Eventually("#b").Should(m.Capture(&idB))

target must be a non-nil pointer.  Biloba decodes into it the way encoding/json does, so a JavaScript
number lands in the Go type you asked for (an *int gets 3, not float64(3)) and a JavaScript object
lands in your struct - including a struct that names only the fields you care about, which is how
HaveJSONAttribute captures are usually written.  A genuine type mismatch - a string into an *int, 3.5
into an *int - fails the assertion immediately (it will never come true, so Capture does not wait out
the timeout).

The one thing Capture will not do is bridge a Biloba struct into a different Go struct: a [Box]
captured into a *ScrollOffset (or into a hand-rolled struct naming a few of Box's fields) is an
immediate, explanatory failure rather than a silent half-fill.  Capture the type the matcher observes -
or an *any - and read the fields you want off that.

Read https://onsi.github.io/biloba/#capturing-values-from-matchers to learn more about capturing values
*/
func (v *ValueMatcher) Capture(target any) *ValueMatcher {
	captured := *v
	captured.target = target
	return &captured
}

func (v *ValueMatcher) Match(actual any) (bool, error) {
	success, err := v.matcher.Match(actual)
	if err != nil || !success || v.target == nil {
		return success, err
	}
	if err := decodeCapture(v.observed(), v.target); err != nil {
		return false, gomega.StopTrying(err.Error())
	}
	return true, nil
}

func (v *ValueMatcher) FailureMessage(actual any) string {
	return v.matcher.FailureMessage(actual)
}

func (v *ValueMatcher) NegatedFailureMessage(actual any) string {
	return v.matcher.NegatedFailureMessage(actual)
}

// decodeCapture writes observed into the pointer target.  It assigns directly when the types already
// line up (the typed getters hand back a Box, a Cookie, a []string) and otherwise round-trips through
// JSON - which is what makes the float64 gotcha go away: a JS number observed as float64(3) decodes
// into an *int as 3.  A mismatch JSON cannot honestly bridge (a string into an *int, 3.5 into an
// *int) comes back as an error, and Match turns that into a StopTrying: it is a bug in the spec, not
// a not-yet condition, so waiting out the timeout would only delay the report.  One shape never
// reaches JSON at all - see rejectStructToStruct.
func decodeCapture(observed any, target any) error {
	value := reflect.ValueOf(target)
	if target == nil || value.Kind() != reflect.Pointer || value.IsNil() {
		return fmt.Errorf("Capture requires a non-nil pointer to decode into, got %T", target)
	}
	elem := value.Elem()
	if !elem.CanSet() {
		return fmt.Errorf("Capture requires a settable pointer to decode into, got %T", target)
	}

	if observed == nil {
		elem.Set(reflect.Zero(elem.Type()))
		return nil
	}

	if observedValue := reflect.ValueOf(observed); observedValue.Type().AssignableTo(elem.Type()) {
		elem.Set(observedValue)
		return nil
	}

	if err := rejectStructToStruct(observed, elem); err != nil {
		return err
	}

	encoded, err := json.Marshal(observed)
	if err != nil {
		return fmt.Errorf("Capture could not encode the observed value:\n%s\n%s", format.Object(observed, 1), err.Error())
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		return fmt.Errorf("Capture cannot decode the observed value into %T:\n%s\n%s", target, format.Object(observed, 1), err.Error())
	}
	return nil
}

// rejectStructToStruct refuses the JSON bridge when we are decoding one concrete Go struct into a
// DIFFERENT concrete Go struct.  The typed getters hand back Box, ScrollOffset, BoxDelta, Cookie -
// every CORRECT capture of one of those took the direct-assign path above, so arriving here means the
// spec named the wrong struct, and JSON would answer that mistake with a half-filled value: these
// structs share field names and carry no json tags, so a Box decodes into a *ScrollOffset filling
// Top/Left and zeroing the rest.  DisallowUnknownFields only catches the EXTRA-field direction, and
// BoxDelta's fields are a strict subset of Box's, so it cannot catch this at all.  We refuse outright
// and say what to do instead - it costs nothing real, since a correct capture never gets here.
//
// This is deliberately narrow: decoding a JS OBJECT (observed as a map) into a struct that names a
// subset of its keys is a legitimate, common capture - it is the documented HaveJSONAttribute case -
// and takes the JSON path below untouched.
func rejectStructToStruct(observed any, elem reflect.Value) error {
	if reflect.ValueOf(observed).Kind() != reflect.Struct || elem.Kind() != reflect.Struct {
		return nil
	}
	return fmt.Errorf("Capture cannot decode the observed %T into a *%s.\nThese are different Go struct types - Biloba will not map one onto the other, as they share field names and would decode into a silently half-filled value.\nCapture into a *%T (or an *any) and read the fields you want off that.", observed, elem.Type(), observed)
}

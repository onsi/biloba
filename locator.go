package biloba

import (
	"encoding/json"
	"fmt"
	"strings"
)

// TestIDAttribute is the HTML attribute [Biloba.ByTestID] matches against.  It defaults to
// "data-testid" (Playwright's default); set it once (e.g. in a SynchronizedBeforeSuite) if your
// app uses a different convention such as "data-test" or "data-qa".
var TestIDAttribute = "data-testid"

/*
Locator is a semantic selector that matches elements the way a user (or the accessibility tree) perceives them - by accessible role, accessible name, visible text, form label, placeholder, alt text, title, or test id - rather than by CSS/XPath structure.  Like a CSS string or an [XPath], a Locator flows through every Biloba DOM method and matcher (and through realistic mode):

	b.Click(b.ByRole("button").WithName("Save"))
	Eventually(b.ByText("Welcome back")).Should(b.BeVisible())
	b.SetValue(b.ByLabel("Email"), "jane@example.com")

Locators are resilient to DOM churn the way structural selectors are not, and they nudge you toward testing what the user perceives.  Build them with the By* constructors ([Biloba.ByRole], [Biloba.ByText], [Biloba.ByLabel], [Biloba.ByPlaceholder], [Biloba.ByAltText], [Biloba.ByTitle], [Biloba.ByTestID]).

Locators compose.  Filter and combine them with [Locator.WithName], [Locator.ContainingText], [Locator.Containing], [Locator.Within] (scoping), [Locator.And]/[Locator.Or] (set combination - which accept any CSS/XPath/Locator selector), [Locator.Nth]/[Locator.First]/[Locator.Last] (ordinal), [Locator.Level] (heading level), and the ARIA-state filters ([Locator.Checked], [Locator.Disabled], [Locator.Expanded], [Locator.Pressed], [Locator.Selected]).

Coverage is pragmatic, not the full ARIA spec: explicit role="" plus the common implicit roles, and accessible names from aria-labelledby/aria-label/<label>/alt/placeholder/value/text/figcaption/caption/title.  Locators pierce open shadow roots (closed roots and cross-origin frames are skipped).  For the long tail of structural queries, drop to [Biloba.XPath] or CSS :has().

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
type Locator struct {
	by string // leaf: role|text|label|placeholder|alttext|title|testid ; combinator: and|or

	role string // by=="role"

	value     string // search string for text/label/placeholder/alttext/title/testid
	valueMode string // "exact" | "contains"
	attr      string // by=="testid": the attribute name to match

	name     string // role accessible-name filter
	nameSet  bool
	nameMode string // "exact" | "contains"

	operands []any // by=="and"/"or": the selectors to intersect / union (CSS/XPath/Locator)

	within    any // scope selector (CSS/XPath/Locator)
	withinSet bool

	filters []locatorFilter // containsText / contains, applied in order

	level    int // heading level
	levelSet bool

	states []string // ARIA-state filters: checked/disabled/expanded/pressed/selected

	nth    int // ordinal index; -1 sentinel means "last"
	nthSet bool
}

// locatorFilter is a post-match predicate: a visible-text test ("containsText") or a
// has-a-descendant-matching-selector test ("contains"). negate flips the sense.
type locatorFilter struct {
	kind     string // "containsText" | "contains"
	value    string // containsText
	mode     string // containsText: "exact" | "contains"
	selector any    // contains: the descendant selector
	negate   bool
}

// ---- leaf constructors -------------------------------------------------------------------------

/*
ByRole(role) returns a [Locator] that matches elements with the given accessible role (e.g. "button", "link", "heading", "checkbox", "textbox").  Chain [Locator.WithName] (accessible name), [Locator.Level] (heading level), or an ARIA-state filter to narrow further:

	b.Click(b.ByRole("button").WithName("Save"))
	Eventually(b.ByRole("heading").Level(2).WithNameContains("Getting Started")).Should(b.BeVisible())

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByRole(role string) Locator {
	return Locator{by: "role", role: role}
}

/*
ByText(text) returns a [Locator] that matches the smallest element whose visible text equals text exactly.

	b.Click(b.ByText("Sign in"))

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByText(text string) Locator {
	return Locator{by: "text", value: text, valueMode: "exact"}
}

// ByTextContains(text) is like [Biloba.ByText] but matches the smallest element whose visible text contains text.
func (b *Biloba) ByTextContains(text string) Locator {
	return Locator{by: "text", value: text, valueMode: "contains"}
}

/*
ByLabel(text) returns a [Locator] that matches the form control whose accessible label equals text exactly (via <label>, aria-label, or aria-labelledby):

	b.SetValue(b.ByLabel("Email"), "jane@example.com")

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByLabel(text string) Locator {
	return Locator{by: "label", value: text, valueMode: "exact"}
}

// ByLabelContains(text) is like [Biloba.ByLabel] but matches a form control whose accessible label contains text.
func (b *Biloba) ByLabelContains(text string) Locator {
	return Locator{by: "label", value: text, valueMode: "contains"}
}

/*
ByPlaceholder(text) returns a [Locator] that matches the input or textarea whose placeholder equals text exactly:

	b.SetValue(b.ByPlaceholder("Search"), "biloba")

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByPlaceholder(text string) Locator {
	return Locator{by: "placeholder", value: text, valueMode: "exact"}
}

// ByPlaceholderContains(text) is like [Biloba.ByPlaceholder] but matches a placeholder that contains text.
func (b *Biloba) ByPlaceholderContains(text string) Locator {
	return Locator{by: "placeholder", value: text, valueMode: "contains"}
}

/*
ByAltText(text) returns a [Locator] that matches the element (e.g. an <img>) whose alt text equals text exactly:

	Eventually(b.ByAltText("Company logo")).Should(b.BeVisible())

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByAltText(text string) Locator {
	return Locator{by: "alttext", value: text, valueMode: "exact"}
}

// ByAltTextContains(text) is like [Biloba.ByAltText] but matches alt text that contains text.
func (b *Biloba) ByAltTextContains(text string) Locator {
	return Locator{by: "alttext", value: text, valueMode: "contains"}
}

/*
ByTitle(text) returns a [Locator] that matches the element whose title attribute equals text exactly:

	Eventually(b.ByTitle("Close")).Should(b.Click())

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByTitle(text string) Locator {
	return Locator{by: "title", value: text, valueMode: "exact"}
}

// ByTitleContains(text) is like [Biloba.ByTitle] but matches a title that contains text.
func (b *Biloba) ByTitleContains(text string) Locator {
	return Locator{by: "title", value: text, valueMode: "contains"}
}

/*
ByTestID(id) returns a [Locator] that matches the element whose test-id attribute equals id exactly.  The attribute name is [TestIDAttribute] ("data-testid" by default):

	b.Click(b.ByTestID("submit-button"))

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByTestID(id string) Locator {
	return Locator{by: "testid", value: id, valueMode: "exact", attr: TestIDAttribute}
}

// ---- role refinements --------------------------------------------------------------------------

// WithName(name) narrows a role [Locator] to elements whose accessible name equals name exactly.
func (l Locator) WithName(name string) Locator {
	l.name, l.nameSet, l.nameMode = name, true, "exact"
	return l
}

// WithNameContains(name) narrows a role [Locator] to elements whose accessible name contains name.
func (l Locator) WithNameContains(name string) Locator {
	l.name, l.nameSet, l.nameMode = name, true, "contains"
	return l
}

/*
Level(n) narrows a role="heading" [Locator] to headings at the given level (an aria-level attribute, or the digit of an <h1>-<h6> tag):

	Eventually(b.ByRole("heading").Level(2).WithName("Getting Started")).Should(b.BeVisible())

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (l Locator) Level(n int) Locator {
	l.level, l.levelSet = n, true
	return l
}

// Checked() narrows the [Locator] to elements that are checked (the checked property or aria-checked="true").
func (l Locator) Checked() Locator { return l.addState("checked") }

// Disabled() narrows the [Locator] to elements that are disabled (the disabled property or aria-disabled="true").
func (l Locator) Disabled() Locator { return l.addState("disabled") }

// Expanded() narrows the [Locator] to elements with aria-expanded="true".
func (l Locator) Expanded() Locator { return l.addState("expanded") }

// Pressed() narrows the [Locator] to elements with aria-pressed="true".
func (l Locator) Pressed() Locator { return l.addState("pressed") }

// Selected() narrows the [Locator] to elements that are selected (the selected property or aria-selected="true").
func (l Locator) Selected() Locator { return l.addState("selected") }

// ---- text / structural filters ----------------------------------------------------------------

/*
ContainingText(text) narrows the [Locator] to elements whose visible text contains text - useful for picking a container by some text inside it (Playwright's filter({hasText})):

	b.Click(b.ByRole("listitem").ContainingText("Product 2"))

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (l Locator) ContainingText(text string) Locator {
	return l.addFilter(locatorFilter{kind: "containsText", value: text, mode: "contains"})
}

// NotContainingText(text) narrows the [Locator] to elements whose visible text does NOT contain text.
func (l Locator) NotContainingText(text string) Locator {
	return l.addFilter(locatorFilter{kind: "containsText", value: text, mode: "contains", negate: true})
}

/*
Containing(selector) narrows the [Locator] to elements that have a descendant matching selector (any CSS/XPath/Locator) - Playwright's filter({has}):

	b.Click(b.ByRole("listitem").Containing(b.ByRole("button").WithName("Delete")))

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (l Locator) Containing(selector any) Locator {
	return l.addFilter(locatorFilter{kind: "contains", selector: selector})
}

// NotContaining(selector) narrows the [Locator] to elements that do NOT have a descendant matching selector.
func (l Locator) NotContaining(selector any) Locator {
	return l.addFilter(locatorFilter{kind: "contains", selector: selector, negate: true})
}

// ---- set combination ---------------------------------------------------------------------------

/*
And(selector) returns a [Locator] matching elements that match BOTH this locator and selector (any CSS/XPath/Locator) - the set intersection (Playwright's and()):

	b.Click(b.ByRole("button").And(".primary"))

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (l Locator) And(selector any) Locator {
	return Locator{by: "and", operands: []any{l, selector}}
}

/*
Or(selector) returns a [Locator] matching elements that match EITHER this locator or selector (any CSS/XPath/Locator) - the set union in document order (Playwright's or()):

	Eventually(b.ByRole("button").WithName("Save").Or(b.ByRole("button").WithName("Submit"))).Should(b.Click())

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (l Locator) Or(selector any) Locator {
	return Locator{by: "or", operands: []any{l, selector}}
}

// ---- scoping & ordinal -------------------------------------------------------------------------

/*
Within(scope) restricts the [Locator] to elements that are descendants of an element matching scope (any CSS/XPath/Locator):

	b.Click(b.ByRole("button").WithName("Delete").Within("#dialog"))

If nothing matches scope the locator matches nothing.

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (l Locator) Within(scope any) Locator {
	l.within, l.withinSet = scope, true
	return l
}

/*
Nth(i) narrows the [Locator] to the single element at index i (0-based) among its matches.  Out-of-range indices match nothing.

	b.Click(b.ByRole("listitem").Nth(2))

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (l Locator) Nth(i int) Locator {
	l.nth, l.nthSet = i, true
	return l
}

// First() narrows the [Locator] to its first match (equivalent to Nth(0)).
func (l Locator) First() Locator {
	return l.Nth(0)
}

// Last() narrows the [Locator] to its final match.
func (l Locator) Last() Locator {
	l.nth, l.nthSet = -1, true
	return l
}

// ---- internals ---------------------------------------------------------------------------------

func (l Locator) addFilter(f locatorFilter) Locator {
	nf := make([]locatorFilter, len(l.filters), len(l.filters)+1)
	copy(nf, l.filters)
	l.filters = append(nf, f)
	return l
}

func (l Locator) addState(s string) Locator {
	ns := make([]string, len(l.states), len(l.states)+1)
	copy(ns, l.states)
	l.states = append(ns, s)
	return l
}

// encode renders the Locator as the "a"-prefixed JSON payload that biloba.js's locate() consumes.
func (l Locator) encode() (string, error) {
	payload, err := l.payload()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "a" + string(data), nil
}

func (l Locator) payload() (map[string]any, error) {
	p := map[string]any{"by": l.by}
	switch l.by {
	case "role":
		p["role"] = l.role
		if l.nameSet {
			p["nameSet"], p["name"], p["nameMode"] = true, l.name, l.nameMode
		}
	case "text", "label", "placeholder", "alttext", "title":
		p["value"], p["valueMode"] = l.value, l.valueMode
	case "testid":
		p["value"], p["attr"] = l.value, l.attr
	case "and", "or":
		ops := make([]string, len(l.operands))
		for i, o := range l.operands {
			enc, err := encodeSelector(o)
			if err != nil {
				return nil, err
			}
			ops[i] = enc
		}
		p["operands"] = ops
	}
	if l.withinSet {
		enc, err := encodeSelector(l.within)
		if err != nil {
			return nil, err
		}
		p["within"] = enc
	}
	if len(l.filters) > 0 {
		fs := make([]map[string]any, len(l.filters))
		for i, f := range l.filters {
			fm := map[string]any{"kind": f.kind, "negate": f.negate}
			switch f.kind {
			case "containsText":
				fm["value"], fm["mode"] = f.value, f.mode
			case "contains":
				enc, err := encodeSelector(f.selector)
				if err != nil {
					return nil, err
				}
				fm["selector"] = enc
			}
			fs[i] = fm
		}
		p["filters"] = fs
	}
	if l.levelSet {
		p["level"] = l.level
	}
	if len(l.states) > 0 {
		p["states"] = l.states
	}
	if l.nthSet {
		p["nthSet"], p["nth"] = true, l.nth
	}
	return p, nil
}

// String renders a human-readable description of the Locator (used in failure annotations).
func (l Locator) String() string {
	op := func(mode string) string {
		if mode == "contains" {
			return "~"
		}
		return "="
	}
	var base string
	switch l.by {
	case "role":
		base = "role=" + l.role
		if l.nameSet {
			base += fmt.Sprintf(" name%s%q", op(l.nameMode), l.name)
		}
	case "text", "label", "placeholder", "alttext", "title":
		base = fmt.Sprintf("%s%s%q", l.by, op(l.valueMode), l.value)
	case "testid":
		base = fmt.Sprintf("testid=%q", l.value)
	case "and", "or":
		parts := make([]string, len(l.operands))
		for i, o := range l.operands {
			parts[i] = describeSelector(o)
		}
		base = "(" + strings.Join(parts, " "+l.by+" ") + ")"
	default:
		base = "locator"
	}
	if l.levelSet {
		base += fmt.Sprintf(" level=%d", l.level)
	}
	for _, s := range l.states {
		base += " " + s
	}
	for _, f := range l.filters {
		not := ""
		if f.negate {
			not = "!"
		}
		if f.kind == "containsText" {
			base += fmt.Sprintf(" %scontainsText%q", not, f.value)
		} else {
			base += fmt.Sprintf(" %scontaining(%s)", not, describeSelector(f.selector))
		}
	}
	if l.withinSet {
		base += fmt.Sprintf(" within(%s)", describeSelector(l.within))
	}
	if l.nthSet {
		if l.nth == -1 {
			base += " [last]"
		} else {
			base += fmt.Sprintf(" [nth=%d]", l.nth)
		}
	}
	return base
}

// describeSelector renders any selector (CSS/XPath/Locator) for failure annotations.
func describeSelector(selector any) string {
	switch x := selector.(type) {
	case Locator:
		return x.String()
	case XPath:
		return string(x)
	case string:
		return x
	default:
		return fmt.Sprintf("%v", x)
	}
}

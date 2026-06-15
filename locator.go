package biloba

import (
	"encoding/json"
	"fmt"
)

/*
Locator is a semantic selector that matches elements by their accessible role, accessible name, visible text, or associated form label - rather than by CSS/XPath structure.  Like a CSS string or an [XPath], a Locator flows through every Biloba DOM method and matcher (and through realistic mode), so you can:

	b.Click(b.ByRole("button").WithName("Save"))
	Eventually(b.ByText("Welcome back")).Should(b.BeVisible())
	b.SetValue(b.ByLabel("Email"), "jane@example.com")

Locators are resilient to DOM churn the way structural selectors are not, and they nudge you toward testing what the user perceives.  Build them with [Biloba.ByRole], [Biloba.ByText]/[Biloba.ByTextContains], and [Biloba.ByLabel]/[Biloba.ByLabelContains].

Coverage is pragmatic, not the full ARIA spec: explicit role="" plus the common implicit roles, and accessible names from aria-labelledby/aria-label/<label>/alt/value/text/title.  Document-level only (does not pierce shadow roots yet).  For the long tail of structural queries, drop to [Biloba.XPath] or CSS :has().

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
type Locator struct {
	by       string
	role     string
	name     string
	nameSet  bool
	nameMode string // "exact" | "contains"
	text     string
	textMode string // "exact" | "contains"
}

/*
ByRole(role) returns a [Locator] that matches elements with the given accessible role (e.g. "button", "link", "heading", "checkbox", "textbox").  Chain [Locator.WithName] or [Locator.WithNameContains] to also match the accessible name:

	b.Click(b.ByRole("button").WithName("Save"))
	Eventually(b.ByRole("heading").WithNameContains("Getting Started")).Should(b.BeVisible())

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByRole(role string) Locator {
	return Locator{by: "role", role: role}
}

/*
WithName(name) narrows a role [Locator] to elements whose accessible name equals name exactly.

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (l Locator) WithName(name string) Locator {
	l.name, l.nameSet, l.nameMode = name, true, "exact"
	return l
}

/*
WithNameContains(name) narrows a role [Locator] to elements whose accessible name contains name.

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (l Locator) WithNameContains(name string) Locator {
	l.name, l.nameSet, l.nameMode = name, true, "contains"
	return l
}

/*
ByText(text) returns a [Locator] that matches the smallest element whose visible text equals text exactly.  ByText is the modern replacement for [Biloba.WithText].

	b.Click(b.ByText("Sign in"))

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByText(text string) Locator {
	return Locator{by: "text", text: text, textMode: "exact"}
}

/*
ByTextContains(text) returns a [Locator] that matches the smallest element whose visible text contains text.

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByTextContains(text string) Locator {
	return Locator{by: "text", text: text, textMode: "contains"}
}

/*
ByLabel(text) returns a [Locator] that matches the form control whose accessible label equals text exactly (via <label>, aria-label, or aria-labelledby):

	b.SetValue(b.ByLabel("Email"), "jane@example.com")

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByLabel(text string) Locator {
	return Locator{by: "label", text: text, textMode: "exact"}
}

/*
ByLabelContains(text) returns a [Locator] that matches the form control whose accessible label contains text.

Read https://onsi.github.io/biloba/#selecting-dom-elements to learn more about selectors.
*/
func (b *Biloba) ByLabelContains(text string) Locator {
	return Locator{by: "label", text: text, textMode: "contains"}
}

// encode renders the Locator as the "a"-prefixed JSON payload that biloba.js's locate() consumes.
func (l Locator) encode() (string, error) {
	payload := map[string]any{"by": l.by}
	switch l.by {
	case "role":
		payload["role"] = l.role
		if l.nameSet {
			payload["nameSet"] = true
			payload["name"] = l.name
			payload["nameMode"] = l.nameMode
		}
	case "text", "label":
		payload["text"] = l.text
		payload["textMode"] = l.textMode
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return "a" + string(data), nil
}

// String renders a human-readable description of the Locator (used in failure annotations).
func (l Locator) String() string {
	switch l.by {
	case "role":
		if l.nameSet {
			op := "="
			if l.nameMode == "contains" {
				op = "~"
			}
			return fmt.Sprintf("role=%s name%s%q", l.role, op, l.name)
		}
		return "role=" + l.role
	case "text":
		if l.textMode == "contains" {
			return fmt.Sprintf("text~%q", l.text)
		}
		return fmt.Sprintf("text=%q", l.text)
	case "label":
		if l.textMode == "contains" {
			return fmt.Sprintf("label~%q", l.text)
		}
		return fmt.Sprintf("label=%q", l.text)
	}
	return "locator"
}

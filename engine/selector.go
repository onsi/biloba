package engine

import (
	"encoding/json"
	"fmt"
)

// MatchMode controls whether semantic locator text is matched exactly or by substring.
type MatchMode string

const (
	Exact    MatchMode = "exact"
	Contains MatchMode = "contains"
)

// Selector is the runner-neutral locator representation consumed by Biloba's browser script.
type Selector struct {
	kind  string
	value string
	role  string
	name  string
	mode  MatchMode
	first bool
}

func CSS(value string) Selector { return Selector{kind: "css", value: value} }

func TestID(value string) Selector { return Selector{kind: "testid", value: value} }

func Text(value string, mode MatchMode) Selector {
	return Selector{kind: "text", value: value, mode: normalizedMode(mode)}
}

func Role(role, name string, mode MatchMode) Selector {
	return Selector{kind: "role", role: role, name: name, mode: normalizedMode(mode)}
}

func (s Selector) First() Selector {
	s.first = true
	return s
}

func (s Selector) Encoded() string {
	if s.kind == "css" && !s.first {
		return "s" + s.value
	}
	payload := map[string]any{"by": s.kind}
	switch s.kind {
	case "css":
		payload["value"] = s.value
	case "testid":
		payload["value"], payload["attr"] = s.value, "data-testid"
	case "text":
		payload["value"], payload["valueMode"] = s.value, normalizedMode(s.mode)
	case "role":
		payload["role"] = s.role
		if s.name != "" {
			payload["nameSet"], payload["name"], payload["nameMode"] = true, s.name, normalizedMode(s.mode)
		}
	}
	if s.first {
		payload["nthSet"], payload["nth"] = true, 0
	}
	encoded, _ := json.Marshal(payload)
	return "a" + string(encoded)
}

func (s Selector) Description() string {
	var description string
	switch s.kind {
	case "css":
		description = fmt.Sprintf("locator(%q)", s.value)
	case "testid":
		description = fmt.Sprintf("getByTestId(%q)", s.value)
	case "text":
		description = fmt.Sprintf("getByText(%q, %s)", s.value, normalizedMode(s.mode))
	case "role":
		description = fmt.Sprintf("getByRole(%q", s.role)
		if s.name != "" {
			description += fmt.Sprintf(", name=%q, %s", s.name, normalizedMode(s.mode))
		}
		description += ")"
	default:
		description = "document"
	}
	if s.first {
		description += ".first()"
	}
	return description
}

func normalizedMode(mode MatchMode) MatchMode {
	if mode == Contains {
		return Contains
	}
	return Exact
}

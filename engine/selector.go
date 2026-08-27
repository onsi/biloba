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

	operands []Selector
	within   *Selector
	filters  []selectorFilter
	level    int
	levelSet bool
	states   []string
	nth      int
	nthSet   bool
}

type selectorFilter struct {
	kind     string
	value    string
	mode     MatchMode
	selector *Selector
	negate   bool
}

func CSS(value string) Selector { return Selector{kind: "css", value: value} }

func XPath(value string) Selector { return Selector{kind: "xpath", value: value} }

func TestID(value string) Selector { return Selector{kind: "testid", value: value} }

func Text(value string, mode MatchMode) Selector {
	return Selector{kind: "text", value: value, mode: normalizedMode(mode)}
}

func Role(role, name string, mode MatchMode) Selector {
	return Selector{kind: "role", role: role, name: name, mode: normalizedMode(mode)}
}

func (s Selector) First() Selector {
	return s.Nth(0)
}

func (s Selector) Last() Selector { s.nth, s.nthSet = -1, true; return s }

func (s Selector) Nth(index int) Selector { s.nth, s.nthSet = index, true; return s }

func (s Selector) Within(scope Selector) Selector { s.within = selectorPointer(scope); return s }

func (s Selector) NotWithin(scope Selector) Selector {
	return s.addFilter(selectorFilter{kind: "within", selector: selectorPointer(scope), negate: true})
}

func (s Selector) ContainingText(value string) Selector {
	return s.addFilter(selectorFilter{kind: "containsText", value: value, mode: Contains})
}

func (s Selector) NotContainingText(value string) Selector {
	return s.addFilter(selectorFilter{kind: "containsText", value: value, mode: Contains, negate: true})
}

func (s Selector) Containing(selector Selector) Selector {
	return s.addFilter(selectorFilter{kind: "contains", selector: selectorPointer(selector)})
}

func (s Selector) NotContaining(selector Selector) Selector {
	return s.addFilter(selectorFilter{kind: "contains", selector: selectorPointer(selector), negate: true})
}

func (s Selector) And(other Selector) Selector {
	return Selector{kind: "and", operands: []Selector{s, other}}
}

func (s Selector) Or(other Selector) Selector {
	return Selector{kind: "or", operands: []Selector{s, other}}
}

func (s Selector) Level(level int) Selector { s.level, s.levelSet = level, true; return s }

func (s Selector) Checked() Selector  { return s.addState("checked") }
func (s Selector) Disabled() Selector { return s.addState("disabled") }
func (s Selector) Expanded() Selector { return s.addState("expanded") }
func (s Selector) Pressed() Selector  { return s.addState("pressed") }
func (s Selector) Selected() Selector { return s.addState("selected") }

func (s Selector) Encoded() string {
	if s.kind == "css" && s.isUnrefined() {
		return "s" + s.value
	}
	if s.kind == "xpath" && s.isUnrefined() {
		return "x" + s.value
	}
	payload := s.payload()
	encoded, _ := json.Marshal(payload)
	return "a" + string(encoded)
}

func (s Selector) payload() map[string]any {
	payload := map[string]any{"by": s.kind}
	switch s.kind {
	case "css", "xpath":
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
	case "and", "or":
		operands := make([]string, len(s.operands))
		for i, operand := range s.operands {
			operands[i] = operand.Encoded()
		}
		payload["operands"] = operands
	}
	if s.within != nil {
		payload["within"] = s.within.Encoded()
	}
	if len(s.filters) > 0 {
		filters := make([]map[string]any, len(s.filters))
		for i, filter := range s.filters {
			encoded := map[string]any{"kind": filter.kind, "negate": filter.negate}
			if filter.kind == "containsText" {
				encoded["value"], encoded["mode"] = filter.value, normalizedMode(filter.mode)
			} else if filter.selector != nil {
				encoded["selector"] = filter.selector.Encoded()
			}
			filters[i] = encoded
		}
		payload["filters"] = filters
	}
	if s.levelSet {
		payload["level"] = s.level
	}
	if len(s.states) > 0 {
		payload["states"] = s.states
	}
	if s.nthSet {
		payload["nthSet"], payload["nth"] = true, s.nth
	}
	return payload
}

func (s Selector) Description() string {
	var description string
	switch s.kind {
	case "css":
		description = fmt.Sprintf("locator(%q)", s.value)
	case "xpath":
		description = fmt.Sprintf("xpath(%q)", s.value)
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
	for _, filter := range s.filters {
		switch {
		case filter.kind == "containsText" && filter.negate:
			description += fmt.Sprintf(".notContainingText(%q)", filter.value)
		case filter.kind == "containsText":
			description += fmt.Sprintf(".containingText(%q)", filter.value)
		case filter.kind == "contains" && filter.negate:
			description += ".notContaining(" + filter.selector.Description() + ")"
		case filter.kind == "contains":
			description += ".containing(" + filter.selector.Description() + ")"
		case filter.kind == "within" && filter.negate:
			description += ".notWithin(" + filter.selector.Description() + ")"
		}
	}
	if s.within != nil {
		description += ".within(" + s.within.Description() + ")"
	}
	if s.levelSet {
		description += fmt.Sprintf(".level(%d)", s.level)
	}
	for _, state := range s.states {
		description += "." + state + "()"
	}
	if s.nthSet {
		switch s.nth {
		case -1:
			description += ".last()"
		case 0:
			description += ".first()"
		default:
			description += fmt.Sprintf(".nth(%d)", s.nth)
		}
	}
	return description
}

func (s Selector) addFilter(filter selectorFilter) Selector {
	filters := make([]selectorFilter, len(s.filters), len(s.filters)+1)
	copy(filters, s.filters)
	s.filters = append(filters, filter)
	return s
}

func (s Selector) addState(state string) Selector {
	states := make([]string, len(s.states), len(s.states)+1)
	copy(states, s.states)
	s.states = append(states, state)
	return s
}

func (s Selector) isUnrefined() bool {
	return s.within == nil && len(s.filters) == 0 && !s.levelSet && len(s.states) == 0 && !s.nthSet
}

func selectorPointer(selector Selector) *Selector { return &selector }

func normalizedMode(mode MatchMode) MatchMode {
	if mode == Contains {
		return Contains
	}
	return Exact
}

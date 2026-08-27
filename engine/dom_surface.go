package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// TextMode selects the browser text representation returned by a text read.
type TextMode string

const (
	InnerText      TextMode = "innerText"
	TextContent    TextMode = "textContent"
	NormalizedText TextMode = "normalizedText"
)

// NameSpec controls whether an absent attribute or property blocks a read.
type NameSpec struct {
	Name     string
	Required bool
}

func RequiredName(name string) NameSpec { return NameSpec{Name: name, Required: true} }
func OptionalName(name string) NameSpec { return NameSpec{Name: name} }

type allowMissingName struct {
	Name string `json:"__biloba_allow_missing"`
}

// MatchScope selects the first match or every current match for a mutation.
type MatchScope uint8

const (
	FirstMatch MatchScope = iota
	AllMatches
)

// InteractionMode selects atomic JavaScript simulation or trusted CDP input.
type InteractionMode uint8

const (
	Fast InteractionMode = iota
	Realistic
)

type MouseButton uint8

const (
	LeftButton MouseButton = iota
	RightButton
	MiddleButton
)

type Modifier uint8

const (
	ShiftModifier Modifier = 1 << iota
	ControlModifier
	AltModifier
	MetaModifier
)

type Point struct{ X, Y float64 }

// ValueLabel selects an option by its rendered label instead of its raw value.
type ValueLabel struct {
	Label string `json:"__biloba_value_label"`
}

func OptionLabel(label string) ValueLabel { return ValueLabel{Label: label} }

type PointerOptions struct {
	Mode      InteractionMode
	Offset    *Point
	Modifiers Modifier
}

type ClickOptions struct {
	Mode      InteractionMode
	Button    MouseButton
	Count     int
	Offset    *Point
	Modifiers Modifier
}

type ScrollIntoViewOptions struct {
	Container    Selector
	TopOffset    float64
	HasTopOffset bool
}

type KeyboardOptions struct {
	Mode      InteractionMode
	Modifiers Modifier
}

// Selection chooses all text, a one-based substring occurrence, or a flat character range.
type Selection struct {
	Substring  string
	Occurrence int
	Start      int
	End        int
	Range      bool
}

type ElementState string

const (
	StateVisible   ElementState = "visible"
	StateEnabled   ElementState = "enabled"
	StateClickable ElementState = "clickable"
	StateChecked   ElementState = "checked"
	StateFocused   ElementState = "focused"
)

type Box struct {
	Top, Left, Width, Height, Bottom, Right, CenterX, CenterY, ClientWidth, ClientHeight float64
}

type ScrollOffset struct{ Top, Left, MaxTop, MaxLeft float64 }
type Offset struct{ Top, Left float64 }
type BoxPair struct{ Subject, Other Box }
type BoxDelta struct{ Top, Left, Width, Height, Bottom, Right, CenterX, CenterY float64 }

type GeometryRelation string

const (
	Above    GeometryRelation = "above"
	Below    GeometryRelation = "below"
	LeftOf   GeometryRelation = "leftOf"
	RightOf  GeometryRelation = "rightOf"
	Encloses GeometryRelation = "encloses"
	Overlaps GeometryRelation = "overlaps"
)

type DocumentOrder string

const (
	Before       DocumentOrder = "before"
	After        DocumentOrder = "after"
	Same         DocumentOrder = "same"
	Disconnected DocumentOrder = "disconnected"
)

func (s *Session) TextContent(ctx context.Context, selector Selector) (Observation, error) {
	return s.TextByMode(ctx, selector, TextContent)
}

func (s *Session) TextByMode(ctx context.Context, selector Selector, mode TextMode) (Observation, error) {
	property, err := textProperty(mode)
	if err != nil {
		return Observation{}, err
	}
	response, err := s.handler(ctx, "getProperty", selector, property)
	observation := response.observation(response.Result)
	if err == nil && mode == NormalizedText {
		observation.Value = normalizeWhitespace(fmt.Sprint(observation.Value))
	}
	return observation, err
}

func (s *Session) Texts(ctx context.Context, selector Selector, mode TextMode) (Observation, error) {
	property, err := textProperty(mode)
	if err != nil {
		return Observation{}, err
	}
	response, err := s.handler(ctx, "getPropertyForEach", selector, property)
	observation := response.observation(response.Result)
	if err == nil && mode == NormalizedText {
		values, _ := observation.Value.([]any)
		for index := range values {
			values[index] = normalizeWhitespace(fmt.Sprint(values[index]))
		}
	}
	return observation, err
}

func textProperty(mode TextMode) (string, error) {
	switch mode {
	case InnerText:
		return "innerText", nil
	case TextContent, NormalizedText:
		return "textContent", nil
	default:
		return "", invalidArgument("read text", fmt.Sprintf("unsupported text mode %q", mode))
	}
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func (s *Session) Classes(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "getProperty", selector, "classList")
	return response.observation(response.Result), err
}

func (s *Session) ClassesForEach(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "getPropertyForEach", selector, "classList")
	return response.observation(response.Result), err
}

func (s *Session) DistinctAttributeCount(ctx context.Context, selector Selector, attribute string) (Observation, error) {
	if attribute == "" {
		return Observation{}, invalidArgument("count distinct attributes", "attribute name must not be empty")
	}
	response, err := s.handler(ctx, "distinctCountByAttr", selector, attribute)
	return response.observation(intValue(response.Result)), err
}

func (s *Session) Attributes(ctx context.Context, selector Selector, names []NameSpec) (Observation, error) {
	args, err := encodeNameSpecs("read attributes", names)
	if err != nil {
		return Observation{}, err
	}
	response, err := s.handler(ctx, "getAttributesP", selector, args)
	return response.observation(response.Result), err
}

func (s *Session) AttributesForEach(ctx context.Context, selector Selector, names []string) (Observation, error) {
	if err := validateNames("read attributes for each", names); err != nil {
		return Observation{}, err
	}
	response, err := s.handler(ctx, "getAttributesForEach", selector, names)
	return response.observation(response.Result), err
}

func (s *Session) JSONAttribute(ctx context.Context, selector Selector, name string) (Observation, error) {
	if name == "" {
		return Observation{}, invalidArgument("read JSON attribute", "attribute name must not be empty")
	}
	response, err := s.handler(ctx, "getAttributesP", selector, []any{name})
	if err != nil {
		return response.observation(response.Result), err
	}
	attributes, ok := response.Result.(map[string]any)
	if !ok {
		return Observation{}, invalidArgument("read JSON attribute", "browser returned malformed attributes")
	}
	raw, ok := attributes[name].(string)
	if !ok {
		return Observation{}, invalidArgument("read JSON attribute", fmt.Sprintf("attribute %q is not a string", name))
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return Observation{Value: raw, Found: response.Found}, invalidArgument("read JSON attribute", fmt.Sprintf("attribute %q is not valid JSON: %v", name, err))
	}
	return Observation{Value: value, Found: response.Found}, nil
}

func (s *Session) Properties(ctx context.Context, selector Selector, names []NameSpec) (Observation, error) {
	args, err := encodeNameSpecs("read properties", names)
	if err != nil {
		return Observation{}, err
	}
	response, err := s.handler(ctx, "getPropertiesP", selector, args)
	return response.observation(response.Result), err
}

func (s *Session) PropertiesForEach(ctx context.Context, selector Selector, names []string) (Observation, error) {
	if err := validateNames("read properties for each", names); err != nil {
		return Observation{}, err
	}
	response, err := s.handler(ctx, "getPropertiesForEach", selector, names)
	return response.observation(response.Result), err
}

func (s *Session) PropertyForEach(ctx context.Context, selector Selector, name string) (Observation, error) {
	if name == "" {
		return Observation{}, invalidArgument("read property for each", "property name must not be empty")
	}
	response, err := s.handler(ctx, "getPropertyForEach", selector, name)
	return response.observation(response.Result), err
}

func (s *Session) Values(ctx context.Context, selector Selector) (Observation, error) {
	response, err := s.handler(ctx, "getValueForEach", selector)
	return response.observation(response.Result), err
}

func encodeNameSpecs(operation string, names []NameSpec) ([]any, error) {
	if len(names) == 0 {
		return nil, invalidArgument(operation, "at least one name is required")
	}
	args := make([]any, len(names))
	for index, spec := range names {
		if spec.Name == "" {
			return nil, invalidArgument(operation, "names must not be empty")
		}
		if spec.Required {
			args[index] = spec.Name
		} else {
			args[index] = allowMissingName{Name: spec.Name}
		}
	}
	return args, nil
}

func validateNames(operation string, names []string) error {
	if len(names) == 0 {
		return invalidArgument(operation, "at least one name is required")
	}
	for _, name := range names {
		if name == "" {
			return invalidArgument(operation, "names must not be empty")
		}
	}
	return nil
}

func (s *Session) SetProperty(ctx context.Context, selector Selector, path string, value any, scope MatchScope) error {
	if path == "" {
		return invalidArgument("set property", "property path must not be empty")
	}
	handler := "setProperty"
	if scope == AllMatches {
		handler = "setPropertyForEach"
	} else if scope != FirstMatch {
		return invalidArgument("set property", "unsupported match scope")
	}
	_, err := s.handler(ctx, handler, selector, path, value)
	return err
}

func (s *Session) State(ctx context.Context, selector Selector, state ElementState) (Observation, error) {
	var handler string
	switch state {
	case StateVisible:
		handler = "isVisible"
	case StateEnabled:
		handler = "isEnabled"
	case StateClickable:
		handler = "isClickable"
	case StateChecked:
		handler = "isChecked"
	case StateFocused:
		handler = "isFocused"
	default:
		return Observation{}, invalidArgument("read element state", fmt.Sprintf("unsupported element state %q", state))
	}
	response, err := s.handler(ctx, handler, selector)
	if err != nil && response.Found != nil && *response.Found && response.Err == "" {
		return response.observation(false), nil
	}
	return response.observation(response.Success), err
}

func (s *Session) AllState(ctx context.Context, selector Selector, state ElementState) (Observation, error) {
	handler := ""
	switch state {
	case StateVisible:
		handler = "eachIsVisible"
	case StateEnabled:
		handler = "eachIsEnabled"
	default:
		return Observation{}, invalidArgument("read every element state", "only visible and enabled support all-match reads")
	}
	response, err := s.handler(ctx, handler, selector)
	if err != nil && response.Err == "" {
		return response.observation(false), nil
	}
	return response.observation(response.Success), err
}

func (s *Session) Focus(ctx context.Context, selector Selector) error {
	_, err := s.handler(ctx, "focus", selector)
	return err
}
func (s *Session) Blur(ctx context.Context, selector Selector) error {
	_, err := s.handler(ctx, "blur", selector)
	return err
}

func (s *Session) Hover(ctx context.Context, selector Selector, mode InteractionMode) error {
	if mode == Fast {
		_, err := s.handler(ctx, "hover", selector)
		return err
	}
	if mode != Realistic {
		return invalidArgument("hover", "unsupported interaction mode")
	}
	return s.serial(ctx, "realistic hover", func(opCtx context.Context) error {
		point, err := s.stablePointerPoint(opCtx, selector)
		if err != nil {
			return err
		}
		if !point.inViewport {
			return &Error{Code: CodeActionFailed, Operation: "realistic hover", Message: "element is outside the viewport"}
		}
		return MouseMoveContext(opCtx, point.x, point.y)
	})
}

func (s *Session) TypeWith(ctx context.Context, selector Selector, keys string, options KeyboardOptions) error {
	if keys == "" {
		return invalidArgument("type", "keys must not be empty")
	}
	if options.Mode != Fast && options.Mode != Realistic {
		return invalidArgument("type", "unsupported interaction mode")
	}
	return s.serial(ctx, "type", func(opCtx context.Context) error {
		if options.Mode == Realistic {
			if _, err := s.runHandler(opCtx, "scrollIntoView", selector); err != nil {
				return err
			}
		}
		if _, err := s.runHandler(opCtx, "focus", selector); err != nil {
			return err
		}
		return KeyEventContext(opCtx, keys, chromedp.KeyModifiers(cdpModifiers(options.Modifiers)))
	})
}

func (s *Session) SendKeysWith(ctx context.Context, keys string, modifiers Modifier) error {
	if keys == "" {
		return invalidArgument("send keys", "keys must not be empty")
	}
	return s.serial(ctx, "send keys", func(opCtx context.Context) error {
		return KeyEventContext(opCtx, keys, chromedp.KeyModifiers(cdpModifiers(modifiers)))
	})
}

func (s *Session) ClickWith(ctx context.Context, selector Selector, options ClickOptions) error {
	if options.Count == 0 {
		return invalidArgument("click", "click count must be positive")
	}
	if options.Count < 0 || options.Count > 2 {
		return invalidArgument("click", "click count must be one or two")
	}
	if options.Button > MiddleButton {
		return invalidArgument("click", "unsupported mouse button")
	}
	if options.Mode == Fast {
		handler := "click"
		switch {
		case options.Button == LeftButton && options.Count == 2:
			handler = "dblClick"
		case options.Button == RightButton && options.Count == 1:
			handler = "rightClick"
		case options.Button == MiddleButton && options.Count == 1:
			handler = "middleClick"
		case options.Button != LeftButton || options.Count != 1:
			return invalidArgument("click", "unsupported fast click combination")
		}
		_, err := s.handler(ctx, handler, selector, pointerPayload(options.Offset, options.Modifiers))
		return err
	}
	if options.Mode != Realistic {
		return invalidArgument("click", "unsupported interaction mode")
	}
	return s.serial(ctx, "realistic click", func(opCtx context.Context) error {
		point, err := s.resolvePointerTarget(opCtx, selector, options.Offset)
		if err != nil {
			return err
		}
		return MouseClickContext(opCtx, point.x, point.y, cdpButton(options.Button), options.Count, cdpModifiers(options.Modifiers))
	})
}

// ClickEach acts once on every element currently matching selector; it never waits for new matches.
func (s *Session) ClickEach(ctx context.Context, selector Selector, mode InteractionMode) error {
	if mode == Fast {
		_, err := s.handler(ctx, "clickEach", selector)
		return err
	}
	if mode != Realistic {
		return invalidArgument("click each", "unsupported interaction mode")
	}
	return s.serial(ctx, "realistic click each", func(opCtx context.Context) error {
		countResponse, err := s.runHandler(opCtx, "count", selector)
		if err != nil {
			return err
		}
		for index := 0; index < int(numeric(countResponse.Result)); index++ {
			pointResponse, pointErr := s.runHandler(opCtx, "scrollToAndPointAt", selector, index)
			if pointErr != nil {
				return pointErr
			}
			pointMap, ok := pointResponse.Result.(map[string]any)
			if !ok {
				continue
			}
			if clickErr := MouseClickContext(opCtx, numeric(pointMap["x"]), numeric(pointMap["y"]), input.Left, 1, 0); clickErr != nil {
				return clickErr
			}
		}
		return nil
	})
}

func (s *Session) Tap(ctx context.Context, selector Selector, options PointerOptions) error {
	if options.Mode == Fast {
		_, err := s.handler(ctx, "tap", selector, pointerPayload(options.Offset, options.Modifiers))
		return err
	}
	if options.Mode != Realistic {
		return invalidArgument("tap", "unsupported interaction mode")
	}
	return s.serial(ctx, "realistic tap", func(opCtx context.Context) error {
		point, err := s.resolvePointerTarget(opCtx, selector, options.Offset)
		if err != nil {
			return err
		}
		return TouchTapContext(opCtx, point.x, point.y)
	})
}

func (s *Session) resolvePointerTarget(ctx context.Context, selector Selector, offset *Point) (actionPoint, error) {
	if offset == nil {
		return s.actionablePoint(ctx, selector)
	}
	if _, err := s.stablePointerPoint(ctx, selector); err != nil {
		return actionPoint{}, err
	}
	response, err := s.runHandlerAsync(ctx, "scrollToStableCorner", selector)
	if err != nil {
		return actionPoint{}, err
	}
	corner, ok := response.Result.(map[string]any)
	if !ok || corner["translatable"] != true {
		return actionPoint{}, &Error{Code: CodeActionFailed, Operation: "resolve pointer target", Message: "element offset is not actionable"}
	}
	left, leftOK := number(corner["left"])
	top, topOK := number(corner["top"])
	width, widthOK := number(corner["innerWidth"])
	height, heightOK := number(corner["innerHeight"])
	x, y := left+offset.X, top+offset.Y
	if !leftOK || !topOK || !widthOK || !heightOK || x < 0 || y < 0 || x > width || y > height {
		return actionPoint{}, &Error{Code: CodeActionFailed, Operation: "resolve pointer target", Message: "element offset is outside the viewport"}
	}
	return actionPoint{x: x, y: y}, nil
}

type stablePointerPoint struct {
	x, y                          float64
	enabled, inViewport, hittable bool
}

func (s *Session) stablePointerPoint(ctx context.Context, selector Selector) (stablePointerPoint, error) {
	response, err := s.runHandlerAsync(ctx, "scrollToStablePoint", selector)
	if err != nil {
		return stablePointerPoint{}, err
	}
	point, ok := response.Result.(map[string]any)
	if !ok {
		return stablePointerPoint{}, malformed("resolve pointer target", response.Result)
	}
	x, xOK := number(point["x"])
	y, yOK := number(point["y"])
	if !xOK || !yOK {
		return stablePointerPoint{}, malformed("resolve pointer target", response.Result)
	}
	return stablePointerPoint{x: x, y: y, enabled: point["enabled"] == true, inViewport: point["inViewport"] == true, hittable: point["hittable"] == true}, nil
}

func pointerPayload(offset *Point, modifiers Modifier) map[string]any {
	payload := map[string]any{"shift": modifiers&ShiftModifier != 0, "control": modifiers&ControlModifier != 0, "alt": modifiers&AltModifier != 0, "meta": modifiers&MetaModifier != 0}
	if offset != nil {
		payload["hasOffset"], payload["ox"], payload["oy"] = true, offset.X, offset.Y
	}
	return payload
}

func cdpButton(button MouseButton) input.MouseButton {
	switch button {
	case RightButton:
		return input.Right
	case MiddleButton:
		return input.Middle
	default:
		return input.Left
	}
}

func cdpModifiers(modifiers Modifier) input.Modifier {
	var result input.Modifier
	if modifiers&AltModifier != 0 {
		result |= input.ModifierAlt
	}
	if modifiers&ControlModifier != 0 {
		result |= input.ModifierCtrl
	}
	if modifiers&MetaModifier != 0 {
		result |= input.ModifierMeta
	}
	if modifiers&ShiftModifier != 0 {
		result |= input.ModifierShift
	}
	return result
}

func (s *Session) FastDragTo(ctx context.Context, source, target Selector) error {
	_, err := s.handler(ctx, "dragTo", source, target.Encoded())
	return err
}

func (s *Session) DragWith(ctx context.Context, source, target Selector, mode InteractionMode) error {
	if mode == Fast {
		return s.FastDragTo(ctx, source, target)
	}
	if mode == Realistic {
		return s.DragTo(ctx, source, target)
	}
	return invalidArgument("drag", "unsupported interaction mode")
}

func (s *Session) ScrollIntoView(ctx context.Context, selector Selector, options ScrollIntoViewOptions) error {
	payload := map[string]any{"hasOffset": options.HasTopOffset, "offset": options.TopOffset}
	if options.Container.kind != "" {
		payload["container"] = options.Container.Encoded()
	}
	_, err := s.handler(ctx, "scrollIntoViewP", selector, payload)
	return err
}

func (s *Session) ScrollWheel(ctx context.Context, selector Selector, deltaX, deltaY float64, mode InteractionMode) error {
	if mode == Fast {
		_, err := s.handler(ctx, "scrollWheel", selector, deltaX, deltaY)
		return err
	}
	if mode != Realistic {
		return invalidArgument("scroll wheel", "unsupported interaction mode")
	}
	return s.serial(ctx, "realistic scroll wheel", func(opCtx context.Context) error {
		point, err := s.stablePointerPoint(opCtx, selector)
		if err != nil {
			return err
		}
		if !point.inViewport || !point.hittable {
			return &Error{Code: CodeActionFailed, Operation: "realistic scroll wheel", Message: "element is outside the viewport or obscured"}
		}
		return ScrollWheelContext(opCtx, point.x, point.y, deltaX, deltaY)
	})
}

func (s *Session) Select(ctx context.Context, selector Selector, selection Selection) error {
	handler := "selectText"
	args := []any{}
	if selection.Range {
		if selection.Start < 0 || selection.End < selection.Start {
			return invalidArgument("select text", "selection end must be at or after its non-negative start")
		}
		handler, args = "selectRange", []any{selection.Start, selection.End}
	} else if selection.Substring != "" {
		if selection.Occurrence <= 0 {
			return invalidArgument("select text", "occurrence must be positive")
		}
		handler, args = "selectOccurrence", []any{selection.Substring, selection.Occurrence}
	} else if selection.Occurrence != 0 {
		return invalidArgument("select text", "substring is required when occurrence is set")
	}
	_, err := s.handler(ctx, handler, selector, args...)
	return err
}

func (s *Session) ClearSelection(ctx context.Context) error {
	return s.serial(ctx, "clear selection", func(opCtx context.Context) error {
		return EvaluateContext(opCtx, `window.getSelection()?.removeAllRanges()`, false, nil)
	})
}

func (s *Session) InvokeMethod(ctx context.Context, selector Selector, method string, args ...any) (Observation, error) {
	if method == "" {
		return Observation{}, invalidArgument("invoke method", "method name must not be empty")
	}
	response, err := s.handler(ctx, "invokeOnP", selector, append([]any{method}, args...)...)
	return response.observation(response.Result), err
}

func (s *Session) InvokeFunction(ctx context.Context, selector Selector, function string, args ...any) (Observation, error) {
	if function == "" {
		return Observation{}, invalidArgument("invoke function", "function source must not be empty")
	}
	response, err := s.handler(ctx, "invokeWithP", selector, append([]any{function}, args...)...)
	return response.observation(response.Result), err
}

func (s *Session) InvokeMethodForEach(ctx context.Context, selector Selector, method string, args ...any) (Observation, error) {
	if method == "" {
		return Observation{}, invalidArgument("invoke method for each", "method name must not be empty")
	}
	response, err := s.handler(ctx, "invokeOnEach", selector, append([]any{method}, args...)...)
	return response.observation(response.Result), err
}

func (s *Session) InvokeFunctionForEach(ctx context.Context, selector Selector, function string, args ...any) (Observation, error) {
	if function == "" {
		return Observation{}, invalidArgument("invoke function for each", "function source must not be empty")
	}
	response, err := s.handler(ctx, "invokeWithEach", selector, append([]any{function}, args...)...)
	return response.observation(response.Result), err
}

func (s *Session) BoundingBox(ctx context.Context, selector Selector) (Box, error) {
	response, err := s.handler(ctx, "boundingBoxP", selector)
	if err != nil {
		return Box{}, err
	}
	return boxFrom(response.Result)
}

func (s *Session) ScrollOffset(ctx context.Context, selector Selector) (ScrollOffset, error) {
	response, err := s.handler(ctx, "scrollOffsetP", selector)
	if err != nil {
		return ScrollOffset{}, err
	}
	m, ok := response.Result.(map[string]any)
	if !ok {
		return ScrollOffset{}, malformed("read scroll offset", response.Result)
	}
	return ScrollOffset{Top: numeric(m["top"]), Left: numeric(m["left"]), MaxTop: numeric(m["maxTop"]), MaxLeft: numeric(m["maxLeft"])}, nil
}

func (s *Session) OffsetWithin(ctx context.Context, selector, container Selector) (Offset, error) {
	response, err := s.handler(ctx, "offsetWithinP", selector, container.Encoded())
	if err != nil {
		return Offset{}, err
	}
	m, ok := response.Result.(map[string]any)
	if !ok {
		return Offset{}, malformed("read offset", response.Result)
	}
	return Offset{Top: numeric(m["top"]), Left: numeric(m["left"])}, nil
}

func (s *Session) RelativeBoxes(ctx context.Context, selector, other Selector) (BoxPair, error) {
	response, err := s.handler(ctx, "relativeBoxesP", selector, other.Encoded())
	if err != nil {
		return BoxPair{}, err
	}
	m, ok := response.Result.(map[string]any)
	if !ok {
		return BoxPair{}, malformed("read relative boxes", response.Result)
	}
	a, err := boxFrom(m["a"])
	if err != nil {
		return BoxPair{}, err
	}
	b, err := boxFrom(m["b"])
	if err != nil {
		return BoxPair{}, err
	}
	return BoxPair{Subject: a, Other: b}, nil
}

func (s *Session) GeometryRelation(ctx context.Context, selector, other Selector, relation GeometryRelation) (Observation, error) {
	pair, err := s.RelativeBoxes(ctx, selector, other)
	if err != nil {
		return Observation{}, err
	}
	a, b := pair.Subject, pair.Other
	var matched bool
	switch relation {
	case Above:
		matched = a.Bottom <= b.Top
	case Below:
		matched = a.Top >= b.Bottom
	case LeftOf:
		matched = a.Right <= b.Left
	case RightOf:
		matched = a.Left >= b.Right
	case Encloses:
		matched = a.Top <= b.Top && a.Left <= b.Left && a.Bottom >= b.Bottom && a.Right >= b.Right
	case Overlaps:
		matched = a.Left < b.Right && a.Right > b.Left && a.Top < b.Bottom && a.Bottom > b.Top
	default:
		return Observation{}, invalidArgument("read geometry relation", fmt.Sprintf("unsupported geometry relation %q", relation))
	}
	return Observation{Value: matched}, nil
}

func (s *Session) GapBetween(ctx context.Context, selector, other Selector) (BoxDelta, error) {
	pair, err := s.RelativeBoxes(ctx, selector, other)
	if err != nil {
		return BoxDelta{}, err
	}
	a, b := pair.Subject, pair.Other
	return BoxDelta{Top: a.Top - b.Top, Left: a.Left - b.Left, Width: a.Width - b.Width, Height: a.Height - b.Height, Bottom: a.Bottom - b.Bottom, Right: a.Right - b.Right, CenterX: a.CenterX - b.CenterX, CenterY: a.CenterY - b.CenterY}, nil
}

func (s *Session) InViewport(ctx context.Context, selector Selector, fully bool) (Observation, error) {
	response, err := s.handler(ctx, "inViewportP", selector)
	if err != nil {
		return response.observation(response.Result), err
	}
	m, ok := response.Result.(map[string]any)
	if !ok {
		return Observation{}, malformed("read viewport state", response.Result)
	}
	top, left, bottom, right, width, height := numeric(m["top"]), numeric(m["left"]), numeric(m["bottom"]), numeric(m["right"]), numeric(m["vw"]), numeric(m["vh"])
	inside := bottom > 0 && right > 0 && top < height && left < width
	if fully {
		inside = top >= 0 && left >= 0 && bottom <= height && right <= width
	}
	return response.observation(inside), nil
}

func (s *Session) DocumentOrder(ctx context.Context, selector, other Selector) (DocumentOrder, error) {
	response, err := s.handler(ctx, "documentOrderP", selector, other.Encoded())
	if err != nil {
		return "", err
	}
	mask := int(numeric(response.Result))
	switch {
	case mask == 0:
		return Same, nil
	case mask&0x04 != 0:
		return Before, nil
	case mask&0x02 != 0:
		return After, nil
	default:
		return Disconnected, nil
	}
}

func (s *Session) ComputedStyle(ctx context.Context, selector Selector, property string) (Observation, error) {
	if property == "" {
		return Observation{}, invalidArgument("read computed style", "property must not be empty")
	}
	response, err := s.handler(ctx, "getComputedStyleP", selector, property)
	return response.observation(response.Result), err
}

func (s *Session) ComputedStyleNumber(ctx context.Context, selector Selector, property string) (Observation, error) {
	if property == "" {
		return Observation{}, invalidArgument("read computed style", "property must not be empty")
	}
	response, err := s.handler(ctx, "getComputedStyleNumericP", selector, property)
	return response.observation(response.Result), err
}

func (s *Session) NormalizeColor(ctx context.Context, color string) (Observation, error) {
	if color == "" {
		return Observation{}, invalidArgument("normalize color", "color must not be empty")
	}
	response, err := s.globalHandler(ctx, "normalizeColor", color)
	return response.observation(response.Result), err
}

func (s *Session) globalHandler(ctx context.Context, name string, args ...any) (HandlerResponse, error) {
	var response HandlerResponse
	err := s.serial(ctx, name, func(opCtx context.Context) error {
		if err := s.ensureBiloba(opCtx); err != nil {
			return err
		}
		var err error
		response, err = RunHandlerContext(opCtx, name, "", args...)
		if err != nil {
			return err
		}
		if response.Err != "" {
			return &Error{Code: CodeActionFailed, Operation: name, Message: response.Err, Observed: response.Result}
		}
		if !response.Success {
			return &Error{Code: CodeActionFailed, Operation: name, Message: "operation did not succeed", Observed: response.Result}
		}
		return nil
	})
	return response, err
}

func boxFrom(value any) (Box, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return Box{}, malformed("read bounding box", value)
	}
	return Box{Top: numeric(m["top"]), Left: numeric(m["left"]), Width: numeric(m["width"]), Height: numeric(m["height"]), Bottom: numeric(m["bottom"]), Right: numeric(m["right"]), CenterX: numeric(m["centerX"]), CenterY: numeric(m["centerY"]), ClientWidth: numeric(m["clientWidth"]), ClientHeight: numeric(m["clientHeight"])}, nil
}

func numeric(value any) float64 { number, _ := number(value); return number }
func malformed(operation string, value any) error {
	return &Error{Code: CodeJavaScript, Operation: operation, Message: fmt.Sprintf("unexpected browser result: %v", value)}
}
func invalidArgument(operation, message string) error {
	return &Error{Code: CodeInvalidArgument, Operation: operation, Message: message}
}

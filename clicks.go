package biloba

import (
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

// clickModifier is a keyboard modifier held down during a pointer interaction (shift-click,
// ctrl-click, ...) or keyboard input (Shift-Enter, ...).  It is internal: users build modifiers with
// b.Shift/Ctrl/Alt/Meta.  The string values double as the keys the biloba.js pointer handlers read
// (see pointerConfig.encode).
type clickModifier string

const (
	modShift   clickModifier = "shift"
	modControl clickModifier = "control"
	modAlt     clickModifier = "alt"
	modMeta    clickModifier = "meta" // Command on macOS, the Windows key elsewhere
)

// pointerConfig accumulates the optional knobs (offset position, held modifiers) for a pointer
// interaction.  It is built up by applying PointerOptions and consumed by both the fast JS path
// (via encode) and the realistic CDP path (via resolvePointerTarget/modifierMask).
type pointerConfig struct {
	offsetX, offsetY float64
	hasOffset        bool
	modifiers        []clickModifier
}

// encode renders the config into the plain object the biloba.js pointer handlers expect.  An
// empty config encodes to an empty object, which the handlers treat as "no offset, no modifiers"
// and so take the native element.click() fast path.
func (cfg pointerConfig) encode() map[string]any {
	o := map[string]any{}
	if cfg.hasOffset {
		o["hasOffset"], o["ox"], o["oy"] = true, cfg.offsetX, cfg.offsetY
	}
	for _, m := range cfg.modifiers {
		o[string(m)] = true
	}
	return o
}

/*
PointerOption configures a pointer interaction.  Build positional options with [Biloba.At] and
modifier options with [Biloba.Shift], [Biloba.Ctrl], [Biloba.Alt], and [Biloba.Meta], then pass
them after the selector (or, in the matcher form, in place of it):

	b.Click("#canvas", b.At(30, 40), b.Shift())
	Eventually("#canvas").Should(b.Click(b.At(30, 40), b.Shift()))

Options are honored by Click, DblClick, RightClick, MiddleClick, and Tap (Tap ignores keyboard
modifiers, which don't apply to touch).

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
type PointerOption func(*pointerConfig)

/*
Modifier is a held keyboard modifier - Shift, Ctrl, Alt, or Meta - shared by pointer interactions
(shift-click) and keyboard input (Shift-Enter).  Build them with [Biloba.Shift], [Biloba.Ctrl],
[Biloba.Alt], and [Biloba.Meta] and pass them to a pointer method ([Biloba.Click] and friends) or a
keyboard method ([Biloba.Type], [Biloba.SendKeysToWindowImmediately]):

	b.Click("#row", b.Shift())                     // shift-click
	b.Type("textarea", biloba.Keys.Enter, b.Shift()) // Shift-Enter

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
type Modifier struct{ modifier clickModifier }

/*
At(offsetX, offsetY) targets a point measured in CSS pixels from the element's top-left corner
(matching Playwright's position option), instead of the element's center:

	b.Click("#canvas", b.At(30, 40))   // click 30px right and 40px down from the top-left corner

The interaction carries the real coordinates, so apps reading e.clientX/e.offsetX see the point
you targeted.  In fast mode, adding any option makes the click dispatch synthetic MouseEvents at
those coordinates instead of calling element.click() - see [Biloba.Click].

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) At(offsetX, offsetY float64) PointerOption {
	return func(c *pointerConfig) { c.offsetX, c.offsetY, c.hasOffset = offsetX, offsetY, true }
}

/*
Shift() holds the Shift key down during a pointer interaction or keyboard input:

	b.Click("#row", b.Shift())                           // shift-click
	b.Type("textarea", biloba.Keys.Enter, b.Shift()) // Shift-Enter

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Shift() Modifier { return Modifier{modShift} }

/*
Ctrl() holds the Control key down during a pointer interaction (e.g. ctrl-click) or keyboard input.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Ctrl() Modifier { return Modifier{modControl} }

/*
Alt() holds the Alt/Option key down during a pointer interaction (e.g. alt-click) or keyboard input.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Alt() Modifier { return Modifier{modAlt} }

/*
Meta() holds the Meta key down during a pointer interaction (e.g. cmd-click) or keyboard input -
Command on macOS, the Windows key elsewhere.

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Meta() Modifier { return Modifier{modMeta} }

// applyPointerOption applies a positional option (b.At, a PointerOption) or a shared modifier
// (b.Shift/b.Ctrl/..., a Modifier) to cfg, reporting whether a was one of those option types
// (false => it's a selector or an invalid argument).
func applyPointerOption(a any, cfg *pointerConfig) bool {
	switch o := a.(type) {
	case PointerOption:
		o(cfg)
	case Modifier:
		cfg.modifiers = append(cfg.modifiers, o.modifier)
	default:
		return false
	}
	return true
}

// isPointerOption reports whether a is a pointer option (b.At) or a shared modifier (b.Shift/...) -
// i.e. not a selector.  It mirrors applyPointerOption's recognized types without mutating a config.
func isPointerOption(a any) bool {
	switch a.(type) {
	case PointerOption, Modifier:
		return true
	}
	return false
}

// parsePointerArgs splits a pointer method's variadic args into (selector, config, immediate).
// Selectors and options (PointerOption/Modifier) are disjoint types, so the leading argument
// disambiguates the two API forms: a selector first => immediate (the rest are options); an option
// first, or no args at all => matcher form (the selector is supplied later by Eventually/Expect).
func (b *Biloba) parsePointerArgs(verb string, args []any) (selector any, cfg pointerConfig, immediate bool) {
	b.gt.Helper()
	if len(args) == 0 {
		return nil, cfg, false
	}
	start := 0
	if !isPointerOption(args[0]) {
		selector, immediate, start = args[0], true, 1
	}
	for _, a := range args[start:] {
		if !applyPointerOption(a, &cfg) {
			b.gt.Fatalf("Failed to %s: expected a selector or a biloba pointer option (b.At/b.Shift/...), got %T", verb, a)
			return selector, cfg, immediate
		}
	}
	return selector, cfg, immediate
}

// pointerInteraction wires up the poll/immediate x fast/realistic fork that every single-selector
// pointer interaction shares.  act performs the interaction on a resolved selector and returns
// (didIt, err): a non-nil err is a hard failure (missing/hidden element) and a false didIt with a nil
// err is a soft "present but not actionable yet".  Both are retried while polling and both fail in
// Immediate() mode - Gomega's MatcherResult semantics give us that for free.
//
// When a selector is present the call resolves to the poll-by-default action form: it routes through
// pollOrImmediate (poll unless Immediate() is set).  When under-applied it returns the bare matcher
// for the user to wrap in Eventually/Expect - and configuring that call (WithTimeout/...) is a hard
// error, since you configure the Eventually, not the matcher.
func (b *Biloba) pointerInteraction(verb, matcherMessage string, args []any, act func(selector any, cfg pointerConfig) (bool, error)) types.GomegaMatcher {
	b.gt.Helper()
	selector, cfg, immediate := b.parsePointerArgs(verb, args)
	matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
		return act(selector, cfg)
	}).WithMessage(matcherMessage)
	if immediate {
		b.pollOrImmediate(selector, matcher)
		return nil
	}
	b.guardBareMatcher(verb)
	return matcher
}

// performClick / performDblClick / performRightClick / performMiddleClick / performTap are the
// fork points handed to pointerInteraction: each routes to the realistic CDP path when the tab is
// realistic, and otherwise to the fast JS handler, passing the encoded option object along.
func (b *Biloba) performClick(selector any, cfg pointerConfig) (bool, error) {
	if b.realistic {
		return b.realisticClick(selector, cfg)
	}
	return b.runBilobaHandler("click", selector, cfg.encode()).MatcherResult()
}

func (b *Biloba) performDblClick(selector any, cfg pointerConfig) (bool, error) {
	if b.realistic {
		return b.realisticDblClick(selector, cfg)
	}
	return b.runBilobaHandler("dblClick", selector, cfg.encode()).MatcherResult()
}

func (b *Biloba) performRightClick(selector any, cfg pointerConfig) (bool, error) {
	if b.realistic {
		return b.realisticRightClick(selector, cfg)
	}
	return b.runBilobaHandler("rightClick", selector, cfg.encode()).MatcherResult()
}

func (b *Biloba) performMiddleClick(selector any, cfg pointerConfig) (bool, error) {
	if b.realistic {
		return b.realisticMiddleClick(selector, cfg)
	}
	return b.runBilobaHandler("middleClick", selector, cfg.encode()).MatcherResult()
}

func (b *Biloba) performTap(selector any, cfg pointerConfig) (bool, error) {
	if b.realistic {
		return b.realisticTap(selector, cfg)
	}
	return b.runBilobaHandler("tap", selector, cfg.encode()).MatcherResult()
}

// performHover ignores cfg - Hover takes no pointer options (it has no button to position or
// modify) - but it shares the same fast/realistic fork shape, so it rides pointerInteraction too.
func (b *Biloba) performHover(selector any, _ pointerConfig) (bool, error) {
	if b.realistic {
		return b.realisticHover(selector)
	}
	return b.runBilobaHandler("hover", selector).MatcherResult()
}

/*
ClickWhen(selector, guardSelector) is the state-guarded, idempotent click: it clicks the element matching selector at most once WHILE an element matching guardSelector is present, and the poll ends once guardSelector stops matching.  guardSelector expresses "the click is still needed" - typically the same element in the state you want to leave:

	b.ClickWhen(".card", ".card.collapsed")   // open the card only if it booted collapsed; no-op if already open

This is the safe way to force a maybe-already-in-state toggle.  The obvious hand-roll - a check-then-click loop inside Eventually - re-clicks on every poll tick and oscillates: a tick landing between the click and the state swap clicks the toggle right back.  ClickWhen fires the click exactly once per observed "still needed" state and then waits (without re-clicking) for the guard to clear, so a settling class-swap can't be double-toggled.  If the guard never clears after the single click it fails at the timeout.

ClickWhen polls by default (honoring WithTimeout/WithPolling/WithContext, and the realistic fork).  Immediate() clicks once iff the guard currently matches, then asserts the guard cleared - failing fast if it did not.  When the guard never matched to begin with, ClickWhen is an immediate no-op success (the element is already in the desired state).

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) ClickWhen(selector, guardSelector any) {
	b.gt.Helper()
	clicked := false
	guardMatches := func() (bool, error) {
		guard := b.runBilobaHandler("exists", guardSelector)
		return guard.Success, guard.Error()
	}
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		matches, err := guardMatches()
		if err != nil {
			return false, err
		}
		if !matches {
			return true, nil // guard cleared (or never matched) -> desired state reached
		}
		if !clicked {
			didClick, err := b.performClick(sel, pointerConfig{})
			if err != nil {
				return false, err
			}
			if !didClick {
				return false, nil // present but not clickable yet; retry without latching
			}
			clicked = true
			// re-check the guard now: a synchronous toggle clears it in this same evaluation (so
			// Immediate() can succeed), while an async swap leaves it set and we wait below.
			matches, err = guardMatches()
			if err != nil {
				return false, err
			}
			return !matches, nil
		}
		return false, nil // already clicked once; wait for the guard to clear, do NOT re-click
	}).WithMessage("no longer match the guard selector after being clicked")
	b.pollOrImmediate(selector, matcher)
}

package biloba

import (
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

// clickModifier is a keyboard modifier held down during a pointer interaction (shift-click,
// ctrl-click, ...).  It is internal: users build modifier options with b.Shift/Ctrl/Alt/Meta.  The
// string values double as the keys the biloba.js pointer handlers read (see pointerConfig.encode).
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
PointerOption configures a pointer interaction.  Build them with [Biloba.At], [Biloba.Shift],
[Biloba.Ctrl], [Biloba.Alt], and [Biloba.Meta] and pass them after the selector (or, in the
matcher form, in place of it):

	b.Click("#canvas", b.At(30, 40), b.Shift())
	Eventually("#canvas").Should(b.Click(b.At(30, 40), b.Shift()))

Options are honored by Click, DblClick, RightClick, MiddleClick, and Tap (Tap ignores keyboard
modifiers, which don't apply to touch).

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
type PointerOption func(*pointerConfig)

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
Shift() holds the Shift key down during a pointer interaction (e.g. shift-click):

	b.Click("#row", b.Shift())

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Shift() PointerOption {
	return func(c *pointerConfig) { c.modifiers = append(c.modifiers, modShift) }
}

/*
Ctrl() holds the Control key down during a pointer interaction (e.g. ctrl-click).

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Ctrl() PointerOption {
	return func(c *pointerConfig) { c.modifiers = append(c.modifiers, modControl) }
}

/*
Alt() holds the Alt/Option key down during a pointer interaction (e.g. alt-click).

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Alt() PointerOption {
	return func(c *pointerConfig) { c.modifiers = append(c.modifiers, modAlt) }
}

/*
Meta() holds the Meta key down during a pointer interaction - Command on macOS, the Windows key
elsewhere (e.g. cmd-click).

Read https://onsi.github.io/biloba/#interacting-with-elements to learn more about interacting with elements
*/
func (b *Biloba) Meta() PointerOption {
	return func(c *pointerConfig) { c.modifiers = append(c.modifiers, modMeta) }
}

// parsePointerArgs splits a pointer method's variadic args into (selector, config, immediate).
// Selectors and PointerOptions are disjoint types, so the leading argument disambiguates the two
// API forms: a selector first => immediate (the rest are options); an option first, or no args at
// all => matcher form (the selector is supplied later by Eventually/Expect).
func (b *Biloba) parsePointerArgs(verb string, args []any) (selector any, cfg pointerConfig, immediate bool) {
	b.gt.Helper()
	if len(args) == 0 {
		return nil, cfg, false
	}
	start := 0
	if _, isOption := args[0].(PointerOption); !isOption {
		selector, immediate, start = args[0], true, 1
	}
	for _, a := range args[start:] {
		option, ok := a.(PointerOption)
		if !ok {
			b.gt.Fatalf("Failed to %s: expected a selector or a biloba pointer option (b.At/b.Shift/...), got %T", verb, a)
			return selector, cfg, immediate
		}
		option(&cfg)
	}
	return selector, cfg, immediate
}

// pointerInteraction wires up the immediate/matcher x fast/realistic fork that every single-selector
// pointer interaction shares.  act performs the interaction on a resolved selector and returns
// (didIt, err): a non-nil err is a hard failure (missing/hidden element - matchers keep polling on
// it, immediates fail), and a false didIt with a nil err is a soft "present but not actionable yet"
// (matchers poll, immediates fail with notActionable).  The fast path collapses both failure modes
// into err; the realistic path distinguishes them - either way Eventually retries and immediates fail.
func (b *Biloba) pointerInteraction(verb, notActionable, matcherMessage string, args []any, act func(selector any, cfg pointerConfig) (bool, error)) types.GomegaMatcher {
	b.gt.Helper()
	selector, cfg, immediate := b.parsePointerArgs(verb, args)
	if immediate {
		ok, err := act(selector, cfg)
		if err != nil {
			b.gt.Fatalf("Failed to %s:\n%s", verb, err.Error())
		} else if !ok {
			b.gt.Fatalf("Failed to %s: %s", verb, notActionable)
		}
		return nil
	}
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return act(selector, cfg)
	}).WithMessage(matcherMessage)
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

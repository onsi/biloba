---
name: biloba-dom-method
description: How to add a new DOM interaction or matcher to Biloba (a browser-action method like Click/SetValue/HaveProperty). Use when adding or modifying a browser-side primitive that touches biloba.js and the Go wrapper, or when implementing the dual immediate/matcher API. Covers the JS bridge, the gcustom matcher pattern, first-vs-all (Each) variants, tests, and docs.
---

# Adding a DOM method/matcher to Biloba

A DOM interaction in Biloba is split across two layers that move together:

1. **`biloba.js`** — a synchronous, atomic primitive on `window._biloba`.
2. **A Go wrapper** (in `dom.go`, `properties.go`, etc.) that calls the primitive via `runBilobaHandler` and exposes it as either an immediate method, a Gomega matcher, or both.

Read `CLAUDE.md` first for the principles. The whole point of doing the work *inside* one JS snippet is atomicity → no flakiness. Never add a Go-side fetch-then-act flow.

## Step 1 — add the browser-side primitive in `biloba.js`

Primitives are registered as `b.<name> = ...` inside the `if (!window["_biloba"])` block. Use the existing combinators — match the dense, functional style:

- `one(...chain)` — operates on the **first** element matching the selector. Each step in the chain is `(n, ...args) => r(...)`; the chain short-circuits if a step's `success` is falsy. Use this for single-element actions/checks. Existence is validated for you (`one` returns an error if `sel(s)` is null).
- `each(cb)` — operates on **all** matching elements; `cb` receives the node array. Use this for `*ForEach`/`*Each` behavior; returns an empty result when nothing matches rather than erroring.
- Result helpers: `r(success, guardMessage)` for boolean checks (a falsy `success` yields a failure with `guardMessage`), `rErr(msg)` for hard errors, `rRes(value)` to return a value.
- `dispatchInputChange(n)` fires `input`+`change` events — reuse it for anything that mutates form state.

Example shapes already in the file:
```js
b.isVisible = one(n => r(n.offsetWidth > 0 || n.offsetHeight > 0 || n.offsetParent != null, "DOM element is not visible"))
b.click    = one(b.isVisible, b.isEnabled, n => r(n.click()))   // composes guards then acts
b.count    = each(ns => rRes(ns.length))
```

Keep checks atomic and pragmatic (e.g. visibility = non-zero offset, not occlusion testing). That is a deliberate stability tradeoff, not a shortcut to fix.

## Step 2 — add the Go wrapper

The Go side calls `b.runBilobaHandler("<jsName>", selector, args...)`, which returns a `*bilobaJSResponse`. **Biloba polls by default** — the old "Biloba never polls" framing is retired. Internally Biloba builds its matcher form and runs it through Gomega for you (`polling.go`), so the fully-applied form *waits* instead of acting once. Pick the shape that matches the API.

Use the typed result getters when returning a value: `r.ResultString()`, `r.ResultInt()`, `r.ResultBool()`, `r.ResultStringSlice()`, `r.ResultAnySlice()`, or `r.Result` (raw `any`).

**Matcher** (poll-friendly, never fails on its own — returns `(bool, error)`):
```go
func (b *Biloba) BeVisible() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("isVisible", selector).MatcherResult()
	}).WithMessage("be visible")
}
```
For matchers that wrap a sub-matcher or need a rich failure message, stash state in a `data` map and use `.WithTemplate(...)` (see `HaveCount`, `HaveProperty`, `HaveClass`). Use `matcherOrEqual(expected)` to accept either a Gomega matcher or a literal value.

### The canonical poll-by-default wiring (`polling.go`)

`polling.go` provides the three helpers every dual/poll method routes through. Build the gcustom matcher **once**, then fork on whether a selector was supplied:

- `b.pollOrImmediate(selector, matcher) bool` — the fully-applied (immediate-looking) branch. By default it runs `Eventually(selector).Should(matcher)` (honoring any `WithTimeout`/`WithPolling`/`WithContext`); under `b.Immediate()` it runs `Expect(selector).To(matcher)` (act once, fail fast). It binds to `b.gt` via `NewWithT` — never the global fail handler — so the failure-capture harness and `Helper()` offsets keep working.
- `b.guardBareMatcher("Method")` — the under-applied (bare-matcher) branch. You configure the `Eventually`/`Expect`, not the matcher, so this rejects every `WithTimeout`/`WithPolling`/`WithContext`/`Immediate` knob with a hard error.
- `b.guardConfig("Method", allowed...)` — for the non-polling buckets (snapshots, waiting commands, one-shot mutations). See the four-bucket model below.

**Dual immediate/matcher** — dispatch on argument count using `args ...any`. Fully-applied POLLS via `pollOrImmediate`; under-applied returns the bare matcher *and* guards it. This is the shape every Cat-1 action shares (`clicks.go pointerInteraction` is the reference impl):
```go
func (b *Biloba) pointerInteraction(verb, matcherMessage string, args []any, act func(...) (bool, error)) types.GomegaMatcher {
	b.gt.Helper()
	selector, cfg, immediate := b.parsePointerArgs(verb, args)
	matcher := gcustom.MakeMatcher(func(selector any) (bool, error) {
		return act(selector, cfg)
	}).WithMessage(matcherMessage)
	if immediate {
		b.pollOrImmediate(selector, matcher)   // fully-applied: POLLS (or Expects under Immediate())
		return nil
	}
	b.guardBareMatcher(verb)                   // under-applied: return bare matcher, reject config knobs
	return matcher
}
```
`SetValue`/`SetProperty`/`DragTo`/`ScrollWheel`/`Focus`/`Blur` etc. follow the same fork directly in `dom.go`. `HaveProperty(property, expected ...any)` (existence-only vs value-matching) is a pure matcher and stays unchanged.

### Value-extracting getters poll-until-found (`Get*`)

A `Get*` getter (`GetProperty`, `GetAttribute`, `GetValue`, `GetInnerText`, `GetTextContent`, `GetProperties`, `GetAttributes`, `InvokeOn`/`InvokeWith`) returns a value but still **polls until the element is found**. Build an **unexported** matcher (not part of the public matcher API) that captures the value into a closure variable, then drive it through `pollOrImmediate` and return the captured value (pattern from `dom.go GetProperty`):
```go
func (b *Biloba) GetProperty(selector any, property any) any {
	b.gt.Helper()
	name := nameOf(property)
	var result any
	matcher := gcustom.MakeMatcher(func(sel any) (bool, error) {
		r := b.runBilobaHandler("getPropertiesP", sel, []any{property})
		if r.Error() != nil {
			return false, r.Error()   // genuine JS error
		}
		if !r.Success {
			return false, nil          // not ready yet → retry
		}
		result = newProperties(r.Result).Get(name)
		return true, nil
	}).WithMessage(fmt.Sprintf("have property %q", name))
	b.pollOrImmediate(selector, matcher)
	return result
}
```
The get-handler is a **single atomic JS op** that returns `found + value` in one round-trip (no `Exist`-then-`get` race). "Success" means "element found"; the value may legitimately be `nil`.

**Two-axis polling (`GetProperty`/`GetProperties`/`GetAttribute`/`GetAttributes`).** These poll until the element is present **and every named property/attribute is defined**. The name params widen `string`/`...string` → `any`/`...any` so each can be a bare `string` or an `AllowMissing`. `b.AllowMissing("name")` exempts one name from the "defined" axis — it comes back `nil` instead of blocking the poll. **Sharp edge:** a name the element type simply can't have (e.g. `disabled` on a `<div>`) would otherwise block the poll forever; it *must* be wrapped in `AllowMissing`. `GetValue`/`GetInnerText`/`GetTextContent` have no "defined" axis (empty string / unselected radio `""` is a valid value) — element-present only, no `AllowMissing`.

### Atomic JS handler / MatcherResult semantics

Keep the not-found/ready/error distinction inside one round-trip so polling stays clean:
- `(false, nil)` = **not ready** → `Eventually` retries.
- `(false, err)` = **genuine JS error** → Gomega does NOT abort the poll; it retries and surfaces the error inside the "Timed out after…" message at the deadline. True fail-fast on a real error happens only under `Immediate()` (which uses `Expect` = single evaluation). Do **not** special-case errors to abort the poll — that re-introduces the flake.

### The four-bucket model and `guardConfig`

Not every method polls. `guardConfig(name, allowed...)` enforces which config knobs (`knobTimeout`/`knobPolling`/`knobContext`/`knobImmediate`) a method accepts:

| Bucket | Examples | Allowed knobs |
|---|---|---|
| **Polling** (Cat 1 actions, Cat 2 `Get*`) | `Click`, `SetValue`, `GetProperty` | all four — skip the guard, route through `pollOrImmediate` |
| **Waiting command** (Cat 5a) | `Navigate`, `Capture*Screenshot*` | `guardConfig(name, knobTimeout, knobContext)` — keep own default deadline; `WithPolling`/`Immediate` hard-error |
| **Snapshot** (Cat 3 `Current*ForEach`, `HasElement`/`Count`) | `CurrentPropertyForEach` | `guardConfig(name)` — no knobs |
| **One-shot mutation** (Cat 5b, `Run`/`RunAsync`) | `SetCookie`, `Run` | `guardConfig(name)` — no knobs |

Snapshot/one-shot methods call `b.guardConfig("Name")` (no knobs) right after `b.gt.Helper()`; waiting commands pass `knobTimeout, knobContext`. A bare-matcher method (Cat 6) or the under-applied form of a dual method uses `guardBareMatcher` instead.

**Naming conventions (poll-by-default).**
- `Get*` (singular) → **polls** until the one element/value you asked about is present.
- `Current*ForEach` → **snapshot** plural getter; no poll, `nil` per missing entry, empty slice when nothing matches (`CurrentPropertyForEach`, `CurrentAttributesForEach`, `CurrentValueForEach`, `CurrentInnerTextForEach`, `CurrentTextContentForEach`). Blessed wait pattern: `Eventually(sel).Should(b.HaveCount(n))` *then* read.
- `*Immediately` → **snapshot** plural action; acts on the current set, no-op on zero, no poll (`ClickEachImmediately`, `SetPropertyForEachImmediately`, `InvokeOnEachImmediately`, `InvokeWithEachImmediately`, `SendKeysToWindowImmediately`). The double-suffix length is an intentional "know what you're doing" smell.

**First-vs-all naming.** If you add a first-element polling getter `GetFoo`, consider its snapshot `CurrentFooForEach` (returns a slice, empty when nothing matches) and/or `EachHaveFoo` matcher counterparts, mirroring `GetProperty`/`CurrentPropertyForEach` and `HaveProperty`/`EachHaveProperty`. The name tells the user which it is.

**`*Each` matchers fail on empty.** `EachBeVisible`/`EachBeEnabled`/`EachHaveClass`/`EachHaveProperty`/`EachHaveInnerText`/`EachHaveTextContent` mean "**≥1 match AND all matches satisfy**." Zero matches **fails** (a vacuous pass is a silent false-positive — exactly the footgun class poll-by-default exists to kill). The `each(cb)` JS combinator still returns empty on no match; the fail-on-empty lives in the Go matcher (return `false` with an "at least one element" message).

**Options (offsets & modifiers) are a distinct type, not a named verb.** Trailing pointer/keyboard options — `b.At(x, y)` and the modifiers `b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` (defined in `clicks.go`) — are a separate `any`-typed argument the method peels off, *not* a `ClickAt`/`ShiftClick` method. A method that accepts them takes `args ...any` and splits options from the selector (see `applyPointerOption` in `clicks.go`, and `splitModifiers` for the keyboard side). The modifiers are deliberately shared across both pointer (`Click`/`Tap`/...) and keyboard (`Type`/`SendKeysToWindowImmediately`, in `keyboard.go`) interactions — if you add a new interaction that should honor them, reuse those helpers rather than inventing a parallel option set. (Keyboard methods drop to chromedp's input domain rather than `runBilobaHandler`, since synthetic JS key events can't type into the page.)

**Godoc.** Add a terse comment to every exported symbol, ending with a `Read https://onsi.github.io/biloba/#... ` link to the relevant docs section, matching the existing comments.

## Step 3 — test it

Tests are Ginkgo specs. Add specs to the matching `*_test.go` file (e.g. `dom_test.go`). Use the `biloba-testing` skill for the harness details (`gt`, `ExpectFailures`, fixtures, and the poll-by-default assertion idioms). Cover at minimum:
- the happy path (the fully-applied form, and the matcher form polled with `Eventually`),
- the not-found / timeout path: because the fully-applied form now polls, assert it with a short `b.WithTimeout(...)` and `ExpectFailures(ContainSubstring("Timed out after"))` rather than an exact immediate-fatal message; add an `Immediate()` spec when you want the old fail-fast message,
- failure-message text where you wrote a custom template (reuse the matcher instance that was already `Match`-ed — see the testing skill's `FailureMessage` gotcha).

If you need new DOM to exercise, add or extend a fixture in `./fixtures/*.html`.

Run with:
```
ginkgo -r -p -randomize-all
```

## Step 4 — docs, skills, and changelog

- Update the narrative docs in `docs/index.md` (this is the source of truth for usage; godoc only links to it). User-facing behavior also needs a terse godoc comment ending in a `Read https://onsi.github.io/biloba/#...` link.
- **Update these plugin skills when you change behavior.** They are part of the project's surface and go stale silently — e.g. when keyboard modifiers shipped, the skills weren't updated. If your change adds/alters a method family, an option, or a convention, reflect it here in the same PR. When in doubt, update the skill.
- Stage a brief entry in `CHANGELOG-TMP.md`. **Never release:** do not bump `BILOBA_VERSION`, do not edit `CHANGELOG.md`, do not tag. Onsi releases via `shipit`, which folds `CHANGELOG-TMP.md` into `CHANGELOG.md` and bumps the version (see `CLAUDE.md`).

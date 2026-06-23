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

The Go side calls `b.runBilobaHandler("<jsName>", selector, args...)`, which returns a `*bilobaJSResponse`. Pick the shape that matches the API:

**Immediate method** (acts now, fails the spec on error). Always call `b.gt.Helper()` first:
```go
func (b *Biloba) ClickEach(selector any) {
	b.gt.Helper()
	r := b.runBilobaHandler("clickEach", selector)
	if r.Error() != nil {
		b.gt.Fatalf("Failed to click each:\n%s", r.Error())
	}
}
```
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

**Dual immediate/matcher** — dispatch on argument count using `args ...any`. This is the key convention (see `CLAUDE.md`). Fully-applied → immediate; under-applied → matcher:
```go
func (b *Biloba) Click(args ...any) types.GomegaMatcher {
	b.gt.Helper()
	if len(args) > 0 {            // immediate: b.Click("#go")
		r := b.runBilobaHandler("click", args[0])
		if r.Error() != nil {
			b.gt.Fatalf("Failed to click:\n%s", r.Error())
		}
		return nil
	}                              // matcher: Eventually("#go").Should(b.Click())
	return gcustom.MakeMatcher(func(selector any) (bool, error) {
		return b.runBilobaHandler("click", selector).MatcherResult()
	}).WithMessage("be clickable")
}
```
`SetValue(args ...any)` (immediate at 2 args, matcher at 1) and `HaveProperty(property, expected ...any)` (existence-only vs value-matching) are the other templates to copy.

**First-vs-all naming.** If you add a first-element method `Foo`, consider its `FooForEach` (returns a slice, empty when nothing matches) and/or `EachHaveFoo` matcher counterparts, mirroring `GetProperty`/`GetPropertyForEach`, `GetAttribute`/`GetAttributeForEach`, and `HaveProperty`/`EachHaveProperty`. The name tells the user which it is.

**Options (offsets & modifiers) are a distinct type, not a named verb.** Trailing pointer/keyboard options — `b.At(x, y)` and the modifiers `b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` (defined in `clicks.go`) — are a separate `any`-typed argument the method peels off, *not* a `ClickAt`/`ShiftClick` method. A method that accepts them takes `args ...any` and splits options from the selector (see `applyPointerOption` in `clicks.go`, and `splitModifiers` for the keyboard side). The modifiers are deliberately shared across both pointer (`Click`/`Tap`/...) and keyboard (`Type`/`SendKeys`, in `keyboard.go`) interactions — if you add a new interaction that should honor them, reuse those helpers rather than inventing a parallel option set. (Keyboard methods drop to chromedp's input domain rather than `runBilobaHandler`, since synthetic JS key events can't type into the page.)

**Godoc.** Add a terse comment to every exported symbol, ending with a `Read https://onsi.github.io/biloba/#... ` link to the relevant docs section, matching the existing comments.

## Step 3 — test it

Tests are Ginkgo specs. Add specs to the matching `*_test.go` file (e.g. `dom_test.go`). Use the `biloba-testing` skill for the harness details (`gt`, `ExpectFailures`, fixtures). Cover at minimum:
- the happy path (immediate and, if applicable, the matcher form polled with `Eventually`),
- the not-found / error path via `ExpectFailures(...)` or by inspecting `matcher.Match(...)`'s returned `err`,
- failure-message text where you wrote a custom template.

If you need new DOM to exercise, add or extend a fixture in `./fixtures/*.html`.

Run with:
```
ginkgo -r -p -randomize-all
```

## Step 4 — docs, skills, and changelog

- Update the narrative docs in `docs/index.md` (this is the source of truth for usage; godoc only links to it). User-facing behavior also needs a terse godoc comment ending in a `Read https://onsi.github.io/biloba/#...` link.
- **Update these plugin skills when you change behavior.** They are part of the project's surface and go stale silently — e.g. when keyboard modifiers shipped, the skills weren't updated. If your change adds/alters a method family, an option, or a convention, reflect it here in the same PR. When in doubt, update the skill.
- Stage a brief entry in `CHANGELOG-TMP.md`. **Never release:** do not bump `BILOBA_VERSION`, do not edit `CHANGELOG.md`, do not tag. Onsi releases via `shipit`, which folds `CHANGELOG-TMP.md` into `CHANGELOG.md` and bumps the version (see `CLAUDE.md`).

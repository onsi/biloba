---
name: biloba-dom-method
description: How to add a new DOM interaction or matcher to Biloba (a browser-action method like Click/SetValue/HaveProperty). Use when adding or modifying a browser-side primitive that touches biloba.js and the Go wrapper, or when implementing the dual immediate/matcher API. Covers the JS bridge, the gcustom matcher pattern (including a matcher whose failure message has a side effect, which embeds CustomGomegaMatcher and overrides FailureMessage), capturable value matchers (*ValueMatcher/.Capture) and the getters' optional decode pointer, missing-element-is-an-error vs silent-retry, when to return gomega.StopTrying, first-vs-all (Each) variants, tests, and docs.
---

# Adding a DOM method/matcher to Biloba

A DOM interaction in Biloba is split across two layers that move together:

1. **`biloba.js`** — a synchronous, atomic primitive on `window._biloba`.
2. **A Go wrapper** (in `dom.go`, `properties.go`, etc.) that calls the primitive via `runBilobaHandler` and exposes it as either an immediate method, a Gomega matcher, or both.

Read `CLAUDE.md` first for the principles. The whole point of doing the work *inside* one JS snippet is atomicity → no flakiness. Never add a Go-side fetch-then-act flow.

## Step 1 — add the browser-side primitive in `biloba.js`

Primitives are registered as `b.<name> = ...` inside the `if (!window["_biloba"])` block. Use the existing combinators — match the dense, functional style:

- `one(...chain)` — operates on the **first** element matching the selector. Each step in the chain is `(n, ...args) => r(...)`; the chain short-circuits if a step's `success` is falsy. Use this for single-element actions/checks. Existence is validated for you (`one` returns an error if `sel(s)` is null).
- `poll(...chain)` — the same shape as `one`, but a missing element returns a bare `{success:false}` (retry) instead of an error. Use it for a getter whose *matcher* counterpart should treat "not there yet" as an ordinary non-match. Prefer `one` when the handler also backs a matcher people will negate (see "missing → error" below) — the geometry probes moved from `poll` to `one` for exactly that reason.
- `each(cb)` — operates on **all** matching elements; `cb` receives the node array. Use this for `*ForEach`/`*Each` behavior; returns an empty result when nothing matches rather than erroring.

`one`/`poll` both stamp a **`found`** boolean onto every response (via `withFound`) reporting whether the selector resolved on that attempt, independent of whether the operation then succeeded. The Go side folds it into the poll-trajectory recorder so a failure can tell "matched, then stopped matching" (a replaced node) from "never matched". You get this for free by using the combinators — don't hand-roll a handler that skips them. Nothing branches on `found`; it is purely diagnostic.
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

The Go side calls `b.runBilobaHandler("<jsName>", selector, args...)`, which returns a `*bilobaJSResponse`. **Biloba polls by default**. Internally Biloba builds its matcher form and runs it through Gomega for you (`polling.go`), so the fully-applied form *waits* instead of acting once. Pick the shape that matches the API.

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

**A failure message with a SIDE EFFECT can't be a template.** A template renders state; it cannot *produce* state. When rendering the message has to do work — `HaveScreenshot` writes the `.actual.png` and `.diff.png` artifacts as it explains the comparison — the matcher stops being a plain `gcustom.MakeMatcher(...)` value and becomes a struct that **embeds `gcustom.CustomGomegaMatcher`** (so `Match` and the negated form come for free) and **overrides `FailureMessage`** (see `screenshotMatcher` in `visual.go`):

```go
type screenshotMatcher struct {
	gcustom.CustomGomegaMatcher
	// …state the failing attempt left behind…
}

func (m *screenshotMatcher) match(actual any) (bool, error) { /* …stash the failing comparison… */ }
func (m *screenshotMatcher) FailureMessage(actual any) string { /* …write artifacts, then render… */ }

m := &screenshotMatcher{...}
m.CustomGomegaMatcher = gcustom.MakeMatcher(m.match)
```

The reason to bother is that `FailureMessage` runs **once**, at the deadline, while `Match` runs on every poll attempt — so the disk write happens for the one comparison that actually failed rather than for each of the dozens that merely hadn't settled yet. Keep the side effect best-effort and quiet (a failing spec should report the assertion, not a disk error) and stash whatever the message needs on the struct during `Match`.

**Decide what a missing element means for your matcher.** `(false, nil)` is "not ready, retry" — which is right under `Eventually`, but it also means `ShouldNot(yourMatcher)` **passes vacuously** against a page that never rendered the element at all. When the matcher's whole point is that the element is there and has some quality, make a genuine not-found an **error** — from the JS side (`one(...)`, or a hand-rolled `sel()` + `notFound(...)` for a two-selector probe, as the geometry handlers do) or from the Go side (`notFoundSelectorError(selector)`, which is how the existence-only `HaveProperty` restores the error its `poll(...)`-backed handler swallows). Make it a deliberate choice, not a default.

### Value-bearing matchers return `*ValueMatcher` (`capture.go`)

A matcher that **reads a value off the page** returns `*ValueMatcher` rather than a bare `types.GomegaMatcher`, so callers can chain `.Capture(&x)` and keep the value from the winning read. This exists to kill gate-then-re-read: asserting with a matcher and *then* calling a getter is two reads of a page that may have changed in between. Wrap the gcustom matcher on the way out:

```go
func (b *Biloba) HaveProperty(property string, expected ...any) *ValueMatcher {
	data := map[string]any{}
	// ...
	return capturableResult(gcustom.MakeMatcher(func(selector any) (bool, error) {
		// ...
		data["Result"] = r.Result       // the same slot the failure template renders
		return matcher.Match(data["Result"])
	}).WithTemplate(..., data), data)
}
```

- `capturableResult(matcher, data)` is the common case: the matcher already stashes what it observed in `data["Result"]` so its template can render it, and `Capture` reads that same slot. **Stash the observed value in `data["Result"]` and the matcher is capturable for free.**
- `capturable(matcher, observed func() any)` is for a matcher whose value doesn't live in `data` — e.g. the existence-only `HaveLocalStorageItem` has no sub-matcher (and so no `Result` for its template) and closes over a local instead (`storage.go`).

**Capturable vs. bare is part of the API — the type is the documentation.** `.Capture` must not compile on a matcher with nothing to hand back. Keep returning `types.GomegaMatcher` for:
- matchers that read no value — `Exist`, `BeVisible`, `BeEnabled`, `BeClickable`, `BeFocused`, `BeChecked`, `BeInViewport`, and the relational geometry matchers (`BeAbove`/`Encloses`/`Overlaps`/`BePrecededBy`/…);
- the actions-as-matchers (`Click`, `SetValue`, `Type`, `DragTo`, `SelectText`, …) — including the under-applied form of every dual method.

Sharp edges the existing code already handles — copy them:
- **An existence-only form should capture too — teach the handler to return the value it already had in hand.** A "is it there?" matcher usually *has* to touch the value to answer, so make the round-trip carry it back and `.Capture` works uniformly across both forms of the same matcher. All three do this now: `HaveProperty(name)` (no expected) calls `getPropertiesP` rather than the boolean `hasProperty`, keeping presence-not-truthiness semantics while making the value available; `HaveAttribute(name)` likewise; and `eachHasProperty` returns `{count, values}` — `count` drives the fail-on-empty message, `values` is the very same `[]any` the value-matching form produces — so `EachHaveProperty(name)` (and the no-arg `EachHaveInnerText()`/`EachHaveTextContent()` that route through it) hand back the whole slice. **A `*ValueMatcher` whose `Capture` leaves the target at zero is a bug, not a signature convenience** — if you find yourself about to document that a form "returns a ValueMatcher for consistency but captures nothing," go make the JS return the value instead.
- **`Capture` writes only on a successful match**, so `ShouldNot`/`NotTo` captures nothing; while polling, the target is overwritten on each successful attempt.
- **`Capture` mutates the receiver and returns it**, so a matcher stashed in a variable and reused keeps writing into the *first* target — matchers are built per-assertion and that's the assumption. `CookieMatcher.withField` copies instead (its `.With*` refinements return a new matcher carrying the same `captureTarget`), so the two capture APIs differ on refinement even though both set the target in place. If you add a builder-style matcher, pick one and document it.
- **Decoding is `decodeCapture`**: direct assign when the types already line up (the typed results — `Box`, `ScrollOffset`, `BoxDelta`, `Cookie`, `[]string`), otherwise a JSON round-trip, which is what makes a JS number land in an `*int` as `3` rather than `float64(3)`. A mismatch JSON can't honestly bridge returns `gomega.StopTrying` — a wrong target type is a spec bug, not a not-yet condition, so it must not wait out the timeout. Decoding one concrete Go struct into a *different* concrete Go struct is tightened with `DisallowUnknownFields` (`structToStruct`), because those structs share field names, carry no json tags, and would otherwise half-fill in silence.
- **`null`/`undefined` zeroes the target, on purpose, with one guaranteed exception.** A JS `null`/`undefined` decodes to the Go zero value for any target — except a `**T` (pointer-to-pointer), which `decodeCapture` leaves `nil` instead of allocating a zeroed `*T`. That's what lets a caller decode into `**T` to tell "absent" apart from "present but empty" (`var key *string; b.GetAttribute(sel, b.AllowMissing("data-key"), &key)`). This is guaranteed behavior, not an accident of the JSON round-trip — keep it working for every target type if you touch `decodeCapture`.
- **A query matcher with its own builder chain grows its own `Capture`.** `CookieMatcher` (`cookie_matchers.go`) carries `.WithValue/.WithPath/.WithSecure/…`, so it can't be a `*ValueMatcher`; it defines `Capture(&cookie)` that composes with the refinements in either order and calls the same `decodeCapture`. Follow that shape for a new builder-style matcher rather than forcing it into `ValueMatcher`.

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

**Optional trailing decode pointer.** A getter whose static type is `any`/`[]any` takes one optional trailing pointer so the caller doesn't have to type-assert: `b.GetProperty("#row", "offsetWidth", &n)`. The pattern is `args ...any` → `target, ok := b.decodeTarget("GetProperty", args)` (hard-errors on more than one arg, or a nil/non-pointer) → `b.decodeResult("GetProperty", target, result)` *after* a successful read. `decodeResult` routes through the same `decodeCapture` as `ValueMatcher.Capture`, so the coercion rules are identical, and the value is still returned as before. It's wired into `GetProperty`/`GetAttribute`/`GetValue` and their `CurrentPropertyForEach`/`CurrentAttributeForEach`/`CurrentValueForEach` snapshots. A getter that already returns a concrete type (`GetBoundingBox` → `Box`, `GetInnerText` → `string`) doesn't need one.

**Readiness can be more than "element found."** A getter may keep polling (`{success:false}` with no error) until the value is *usable*, not just present — the handler adds its own gate on top of finding the element. The two-axis property getters gate on "property defined"; the **geometry getters** (`geometry.go` / `boundingBoxP`/`scrollOffsetP`/`offsetWithinP` in `biloba.js`) gate on a *non-degenerate layout box* (`width`/`height` > 0), so a getter never reads a zero box mid-layout.

**Split the two cases deliberately: missing → error, present-but-not-ready → silent retry.** The geometry handlers are the reference. They're built on `one(...)` (or hand-rolled `sel()` + `notFound(...)` for the two-selector probes) rather than `poll(...)`, so a **missing** element returns an error — which under `Eventually` is still just a failed attempt, but keeps `ShouldNot(b.BeInViewport())` from passing instantly against an element that never rendered. The **degenerate box** stays `{success:false}` with no error, so the positive direction polls through late layout. Same rule elsewhere: `isChecked` errors when the node has no `checked` property at all (the "I selected the label, not the input" bug) instead of coercing `undefined` to `false`. Whenever you write a `poll(...)`-backed handler, ask what a negated assertion on it would do against an empty page. **And say what the negation should be instead:** an error is never a satisfied assertion in *either* direction, so making missing-is-an-error also breaks the legitimate wait-for-teardown spec (`Eventually(sel).ShouldNot(b.BeInViewport())` on a toast that gets removed). Whenever you flip a matcher this way, the docs/skills/changelog entry must name the replacement idiom — assert disappearance with the matchers that are *about* existence, `ShouldNot(b.Exist())` / `Should(b.HaveCount(0))` — not just the vacuous-pass it fixed. They follow this exact `Get*` pattern plus a `Have*`-matcher counterpart (one decodes the struct into a closure var; the other runs a caller-supplied sub-matcher against the decoded struct/value), and they decode the JS map into a typed struct (`Box`/`ScrollOffset`/`BoxDelta`) via a small `newBox`/`newScrollOffset`/`newBoxDelta` à la `newProperties`. **Element-to-element** probes (`relativeBoxesP`, `documentOrderP`) take a *second* selector — Go encodes it with `encodeSelector` and passes the encoded string so the JS `sel()` resolves it directly, and both elements are read in ONE eval so the relation is judged at a single layout instant (the whole point — splitting into two getters loses the atomic frame). They back pure relational matchers (`BeAbove`/`Encloses`/`Overlaps`/`BePrecededBy`/…) plus the `GetGapBetween`/`HaveGapBetween` dual; `BeInViewport` is a single-selector matcher reading the rect against `window.innerWidth/Height`.

**Recording for the poll-trajectory artifact (`probe_trajectory.go`).** Polling getters and `b.Run`/`b.RunAsync` call `b.recordProbe(probeKey(method, sel), value)` once they have a value, so a failing `Eventually` over that read can attach its `(elapsed, value)` trajectory. It's a no-op unless `BilobaConfigPollTrajectory` is on (default on) and costs ~nanoseconds. If you add a new polling getter, call `recordProbe` at the success point (see `geometry.go`).

**Two-axis polling (`GetProperty`/`GetProperties`/`GetAttribute`/`GetAttributes`).** These poll until the element is present **and every named property/attribute is defined**. The name params widen `string`/`...string` → `any`/`...any` so each can be a bare `string` or an `AllowMissing`. `b.AllowMissing("name")` exempts one name from the "defined" axis — it comes back `nil` instead of blocking the poll. **Sharp edge:** a name the element type simply can't have (e.g. `disabled` on a `<div>`) would otherwise block the poll forever; it *must* be wrapped in `AllowMissing`. `GetValue`/`GetInnerText`/`GetTextContent` have no "defined" axis (empty string / unselected radio `""` is a valid value) — element-present only, no `AllowMissing`.

That sharp edge is now **self-explaining**, and a new two-axis getter must keep it that way. The JS handler's not-ready return carries a diagnostic instead of a value — `{success:false, result:{element, undefined:[names]}}` — and the Go side folds it in with `noteUndefinedAxis` + `undefinedAxisTemplate` (`dom.go`), swapping `.WithMessage` for `.WithTemplate`. The timeout then says the element was present the whole time, that it was the *property* that never appeared, and names the exact `b.AllowMissing("...")` call to copy. `{success:false}` still means "retry" — nothing branches on the diagnostic.

**A polling getter doesn't have to be about an element.** `GetLocation`/`GetTitle` (`navigation.go`) and `GetJSValue` (`javascript.go`) are `Get*` getters that poll the *tab*: build the gcustom matcher over `*Biloba` (or over the expression string) and hand `b` itself to `pollOrImmediate` as the actual. The `Get` prefix tracks "polls and returns a value", not "takes a selector". Crucially, these read through CDP, and **a transient CDP error must be a retryable miss** — returning `(false, err)` — never a `Fatalf`. A `Fatalf` inside a poll is a Ginkgo `Fail`, which *panics out* of the surrounding `Eventually` instead of counting as a failed attempt; that bug is what made `Eventually(b.Location)` unpollable. Any internal caller that needs a single-shot read gets an unguarded `location()`/`title()`-style substrate (cf. `Run`/`run`) so snapshot and failure-rendering paths never poll or fail.

### Atomic JS handler / MatcherResult semantics

Keep the not-found/ready/error distinction inside one round-trip so polling stays clean:
- `(false, nil)` = **not ready** → `Eventually` retries.
- `(false, err)` = **genuine JS error** → Gomega does NOT abort the poll; it retries and surfaces the error inside the "Timed out after…" message at the deadline. True fail-fast on a real error happens only under `Immediate()` (which uses `Expect` = single evaluation). Do **not** special-case errors to abort the poll — that re-introduces the flake.
- `(false, gomega.StopTrying(msg))` = **a condition that can never come true** → stop the poll now and report `msg`. Reserve it for exactly that: a state no amount of waiting can change. The reference cases are `HaveScreenshot`'s **missing baseline file** (the file will not appear because the spec kept asking) and an undecodable PNG, alongside `decodeCapture`'s wrong-target-type. Waiting out a full timeout to say "there is no baseline" would bury the one instruction the user needs. A page that hasn't settled yet is *never* one of these — keep it `(false, nil)`.

### State a method mutates must live in `tabState`

`b.Realistic()`, `b.WithTimeout(d)`, `b.Immediate()`, `b.ViewportOnly()` and friends are **shallow copies** of the `Biloba` struct (`nb := *b; nb.flag = true`). So anything a method reads or writes on the tab has to be reachable through a copy:

- **Maps and pointers are fine** — the copy shares them.
- **Slices and bools declared on `Biloba` are NOT.** An `append` through a copy updates the copy's slice header and nothing else, and a read through a copy sees the list frozen at the moment the copy was made. Both directions fail silently.

So a method that registers or accumulates per-tab state puts that state in **`tabState`** (`biloba.go`), which every view shares by pointer. Its membership rule is "what `Prepare()` resets" — `newTabState()` is both the fresh-tab state and the reset, so a field added there can't be forgotten by `Prepare`. `Prepare` resets *through* the pointer (`*b.state = ...`), never by swapping in a new one, or the views would keep the old.

Fields that stay on `Biloba` itself: identity (`Context`, `targetID`, `root`), suite config (the failure-artifact knobs, `downloadDir`), and the view flags themselves — which is the whole point, since those are *supposed* to differ per view.

`go vet` will not catch a mistake here: its copylocks check is what would otherwise flag `nb := *b`, and `lock` is a `*sync.Mutex` so it stays quiet. The test is a spec that registers through a held view (`rb := b.Realistic()`) and asserts the bare tab sees it — see "registering through a view of the tab" in `network_test.go` and `dialog_handling_test.go`.

### The four-bucket model and `guardConfig`

Not every method polls. `guardConfig(name, allowed...)` enforces which config knobs (`knobTimeout`/`knobPolling`/`knobContext`/`knobImmediate`) a method accepts:

| Bucket | Examples | Allowed knobs |
|---|---|---|
| **Polling** (Cat 1 actions, Cat 2 `Get*`) | `Click`, `SetValue`, `GetProperty` | all four — skip the guard, route through `pollOrImmediate` |
| **Waiting command** (Cat 5a) | `Navigate`, `Capture*Screenshot*` | `guardConfig(name, knobTimeout, knobContext)` — keep own default deadline; `WithPolling`/`Immediate` hard-error |
| **Snapshot** (Cat 3 `Current*ForEach`, `HasElement`/`Count`) | `CurrentPropertyForEach` | `guardConfig(name)` — no knobs |
| **One-shot mutation** (Cat 5b, `Run`/`RunAsync`) | `SetCookie`, `Run` | `guardConfig(name)` — no knobs |

Snapshot/one-shot methods call `b.guardConfig("Name")` (no knobs) right after `b.gt.Helper()`; waiting commands pass `knobTimeout, knobContext`. A bare-matcher method (Cat 6) or the under-applied form of a dual method uses `guardBareMatcher` instead.

**Clone-with-a-flag views are a separate axis from the knobs.** `b.Realistic()`, `b.ViewportOnly()`, and the poll-config views all return a shallow copy of the tab with one field set (`rb := *b; rb.flag = true; return &rb`) — `guardConfig` governs the *knobs*, not these. Two rules when you add one:

- Name it so it can't be mistaken for a getter on the same subject. `ViewportOnly()` rather than `Viewport()`, because `WindowSize()` already returns dimensions and a `Viewport()` returning a `*Biloba` would read as its sibling.
- **A view that a method cannot honour must hard-error, not be silently ignored.** `b.ViewportOnly().CaptureScreenshotOf(sel)` fails via a small `guardViewportOnly(name)` guard; the `HaveScreenshot` path returns `StopTrying` (no amount of polling makes the flag mean something). Silently dropping the flag hands the caller a picture of the whole element with nothing to say their request was discarded — the same vacuity class as a baseline that compares nothing.

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
- failure-message text where you wrote a custom template (reuse the matcher instance that was already `Match`-ed — see the testing skill's `FailureMessage` gotcha),
- for a `*ValueMatcher`: a `.Capture(&x)` spec (the captured value equals what the page held), a `ShouldNot` spec (target untouched), and a wrong-target-type spec (fails fast rather than timing out) — mirror the existing capture specs in `dom_test.go`/`geometry_test.go`.

If you need new DOM to exercise, add or extend a fixture in `./fixtures/*.html`.

Run with:
```
ginkgo -r -p -randomize-all
```

## Step 4 — docs, skills, and changelog

- Update the narrative docs in `docs/index.md` (this is the source of truth for usage; godoc only links to it). User-facing behavior also needs a terse godoc comment ending in a `Read https://onsi.github.io/biloba/#...` link.
- **Update these plugin skills when you change behavior.** They are part of the project's surface and go stale silently — e.g. when keyboard modifiers shipped, the skills weren't updated. If your change adds/alters a method family, an option, or a convention, reflect it here in the same PR. When in doubt, update the skill.
- Stage a brief entry in `CHANGELOG-TMP.md`. **Never release:** do not bump `BILOBA_VERSION`, do not edit `CHANGELOG.md`, do not tag. Onsi releases via `shipit`, which folds `CHANGELOG-TMP.md` into `CHANGELOG.md` and bumps the version (see `CLAUDE.md`).

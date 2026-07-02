---
name: flaky-specs
description: Diagnose and prevent flaky Biloba specs — tests that pass locally but fail in CI, fail intermittently under `ginkgo -p`, or fail "somewhere else" than the line that's actually wrong. Biloba polls by default, so the headline rule is "don't reach for b.Immediate()"; the residual smells — single-shot `b.Run(expr,&x)` reads, the non-polling SendKeysToWindowImmediately/`*Immediately` verbs, AllowMissing for absent-on-type properties, optimistic-UI/server-reconciliation traps, and async-settling geometry/layout/document-order reads. Use when a browser spec is flaky, nondeterministic, order-dependent, or load-sensitive, or when reviewing a suite for latent races.
---

# Avoiding & fixing flaky Biloba specs

**Biloba polls by default — so the actions and getters you write (`b.Click(sel)`, `b.GetProperty(...)`, `b.SetValue(...)`) already wait for the element to be ready and act exactly once. The whole poll-by-default design exists to kill the immediate-mode flake footgun.** That removes the single biggest historical flake source. What's left is the handful of things that *don't* poll, plus reads you take in your own Go/JS:

> **Never assert on a value you read exactly once.** A browser is a pile of async settles — a WS frame, a layout/measure pass, an rAF-scheduled DOM injection, an optimistic→authoritative reconciliation. Any single read can land *before* the thing you care about settles. Poll it instead.

The two reflexes: **don't reach for `b.Immediate()`** (it opts back into the old act-once race), and **wrap your own `b.Run` reads in `Eventually`** (Biloba can't poll a value you extract by hand). The smells below are the recurring shapes; each has a polling fix. For the failure-artifact side (reading outlines/screenshots once a spec *has* failed) see `biloba:debug-failures`; for the authoring baseline see `biloba:write-tests`.

## Smell 1 — the single-shot `b.Run` read  (the #1 flake source)

```go
var centered bool
b.Run(`(() => { /* measure geometry / read a store / check doc order */ })()`, &centered)
Expect(centered).To(BeTrue())   // races whatever the expr measured — flakes the instant it settles late
```

`b.Run(expr, &x)` immediately followed by `Expect(x)` reads the world once, at one instant. If anything the expression touches settles asynchronously, it flakes. **Poll the expression instead** — `b.Run` is a plain `func(string, ...any) any`, so it drops straight into `Eventually`:

```go
Eventually(b.Run).WithArguments(`document.querySelector("#card").getBoundingClientRect().top`).
    Should(BeNumerically("~", 40, 1))            // numbers decode to float64 — BeNumerically, not Equal
Eventually(b.Run).WithArguments(`(() => isCentered())()`).Should(BeTrue())          // bool
Eventually(b.Run).WithArguments(`document.title`).Should(Equal("Ready"))            // string
```

No wrapper closure is needed for a scalar/bool/string expression. Remember JSON-decoded numbers are **`float64`** — assert with `BeNumerically`, never `Equal(intLiteral)`.

**For the geometry subclass, prefer the native getters over `b.Run` entirely.** `getBoundingClientRect`/`scrollTop`/offset reads are the most common `b.Run` blobs *and* the most race-prone — so Biloba provides pollable geometry getters that fold layout-readiness in (they wait until the element is present **and** has a non-degenerate box). Reach for these first; drop to `Eventually(b.Run)` only for geometry they don't cover:

```go
Eventually(".hero .sec").Should(b.HaveBoundingBox(HaveField("Top", BeNumerically("<", 120))))
Eventually(".hero .sec").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("<", 120))) // "scrolled near the top"
Eventually(".scroller").Should(b.HaveScrollOffset(HaveField("Top", BeNumerically("==", 0))))
box := b.GetBoundingBox("#card")  // getter form: polls until laid out, returns Box{Top,Left,Width,Height,Bottom,Right,CenterX,CenterY}
```

**Interpolated / multi-line scripts.** `WithArguments` needs a pre-built string, so for an `fmt.Sprintf`-interpolated or multi-line expr, build the string first or wrap a one-line closure that returns the value:

```go
expr := fmt.Sprintf(`document.querySelector(%q).scrollTop`, sel)
Eventually(b.Run).WithArguments(expr).Should(BeNumerically(">", 0))

// or poll a closure when you need Go-side glue around the read:
Eventually(func() float64 {
    var top float64
    b.Run(fmt.Sprintf(`document.querySelector(%q).getBoundingClientRect().top`, sel), &top)
    return top
}).Should(BeNumerically("~", 40, 1))
```

**Grep your own suite for the anti-pattern.** `rg 'b\.Run\(.*&(\w+)\)' -A3 | rg 'Expect\(|Ω\('` finds the *single-line* reads — but the worst offenders are **multi-line**: `b.Run(\`(() => { …several lines… })()\`, &x)` puts the `, &x)` decode-target far from `b.Run(`, so it slips that regex entirely (in practice these — SVG-geometry, document-order reads — are about *half* the findings). Scan in two stages instead: first list every decode target wherever it lands —

```
rg ', &(\w+)\)' -n          # every "…, &x)" — incl. the orphan close-line of a multi-line script
```

— then for each captured var, check whether an `Expect(x)`/`Ω(x)` follows within a few lines (that's the single-shot read). The decode target, not the `b.Run(` token, is the reliable anchor.

**A Biloba matcher poll retries *through a remount* — only your own `b.Run` reads need null-guards.** A common hand-roll defends against a node that gets torn down and re-created (a portal migration, a list re-key) with `document.querySelector(sel)?.dataset.side ?? ''` and a comment claiming a Biloba getter "hard-fails across the remount." Under poll-by-default that's legacy folklore: `Eventually(sel).Should(b.HaveProperty("dataset.side", "left"))` **re-resolves `sel` from scratch every tick**, so it simply retries through the gap — no cached node, nothing to null-guard, no special handling. The null-guard is only needed in *your own* `b.Run`/`Eventually(b.Run)` closure, because there you hold a node reference that a remount invalidates. So: reach for the matcher (`HaveProperty`/`HaveAttribute`/`GetProperty`) and delete the guard; keep the guard only inside a raw `b.Run` read you couldn't express as a matcher.

## Smell 2 — reaching for `b.Immediate()` and reintroducing the race

`b.Click(sel)`, `b.Tap(sel)`, `b.SelectText(sel)`, `b.SetValue(sel, …)` — every fully-applied action now **polls until the element is ready, acts exactly once, then stops.** This is the default, and it is what keeps these flake-free: a `b.Click("#go")` written right after a re-render, a list load, or a card injection simply *waits* for the element instead of racing it. **Write the plain fully-applied form and move on.**

```go
b.Click("#go")                              // polls until clickable, clicks once, stops — then…
Eventually(out).Should(b.HaveClass("open")) // …assert the observable outcome
```

The flake comes back only if you **opt out** with `b.Immediate()`. `b.Immediate().Click(sel)` acts once and fails fast — fire it a frame too early and it no-ops or hits a stale element, and (the cruel part) **the spec doesn't fail at the interaction**: it fails later, at the assertion that depended on it — a downstream `Eventually(...class…)` that times out, or a `null is not an object` from the app's own handler — with nothing pointing back. **So the anti-flake rule is simple: don't reach for `b.Immediate()`.** There is almost never a reason to; the default already does the right thing. (If the default wait is too short, tune it — `b.WithTimeout(d).Click(sel)` — don't drop to `Immediate`.)

The **matcher form** (`Eventually(sel).Should(b.Click())`) is still available when you want to own the poll — a custom `Consistently`, composing with `.And()`, or driving a non-default `Eventually`. It has the same single-shot-and-stop semantics (it dispatches once on first success, never re-fires, so it's safe even on a **toggle**). Selector-only verbs become `Eventually(sel).Should(b.Verb())`; verbs with trailing args move the selector into `Eventually` and keep the rest in the matcher (`b.SetValue(sel, v)` → `Eventually(sel).Should(b.SetValue(v))`; `b.ScrollWheel(sel, dx, dy)` → `Eventually(sel).Should(b.ScrollWheel(dx, dy))`). But for the common case you don't need it — the fully-applied form already polls.

**The one sanctioned use of `Immediate()`: "set-and-confirm-it-stuck".** When you act on an *optimistic* field that can silently revert (Smell 3), put an **immediate** action *and* its confirmation inside one `Eventually(func(g Gomega){...})` closure — the closure is the poll, so the action re-fires each iteration until the value is observed to have stuck. Use `b.Immediate()` here so the inner action acts once per iteration (a default polling `SetValue` would run its *own* nested poll inside each closure pass):

```go
Eventually(func(g Gomega) {
    b.Immediate().SetValue("#qty", 3)           // act once per iteration — re-runs each poll…
    g.Expect("#qty").To(b.HaveValue("3"))       // …until the value actually sticks
}).Should(Succeed())
```

This is the rare case where reaching for `Immediate()` is correct and deliberate: you want to keep re-asserting against a value that may reconcile away, and the *outer* `Eventually` is the poll. (Everywhere else, prefer the plain poll-by-default form — Smell 2.)

> **Name the nested-double-poll smell.** Inside an `Eventually(func(g Gomega){...})` closure the `.Immediate()` is *load-bearing*, not optional. Writing the plain **polling** `b.SetValue("#qty", 3)` there (without `.Immediate()`) still works — so it slips review — but it runs `SetValue`'s *own* nested poll on every iteration of the outer poll: a poll inside a poll. It's wasteful and it muddies failure output (the inner poll's timeout, not your assertion's). The rule: **an action inside a polling closure must be `b.Immediate()`.** If you're not inside a closure, don't use a closure *or* `Immediate()` — just write the fully-applied `b.SetValue("#qty", 3)` and let it poll once.

**State-guarded toggles: use `b.ClickWhen`, never a hand-rolled check-then-click.** The specific trap: an element that may boot in one of two states (a card open-or-collapsed, a disclosure) and you must ensure it ends open. The *obvious* hand-roll —

```go
Eventually(func() bool {                       // WRONG — re-clicks every tick, oscillates
    if b.HasElement(".card.collapsed") { b.Immediate().Click(".card") }
    return !b.HasElement(".card.collapsed")
}).Should(BeTrue())
```

— re-clicks on **every** poll tick, so a tick that lands between the click and the class swap toggles the card right back shut. This is the exact oscillation poll-by-default exists to prevent, reintroduced by hand. Use the primitive built for it, which clicks **at most once** while the guard matches and then waits (without re-clicking) for it to clear:

```go
b.ClickWhen(".card", ".card.collapsed")   // open iff collapsed; no-op if already open; no double-toggle
```

**The poll-by-default action does not check occlusion — keep an explicit `BeClickable` gate when an overlay may cover the target.** `b.Click(sel)` polls on visible + enabled, but a fast click is `element.click()`; it does **not** verify the element is the topmost thing at its center, so it will happily "click" through a modal/overlay sitting on top. When occlusion is possible, gate with `Eventually(sel).Should(b.BeClickable())` (which adds the topmost-at-center check) before acting, or use `b.Realistic()` (which refuses to click through an overlay). Poll-by-default alone won't catch it.

**Two interactions can't poll — gate them by hand.** `b.SendKeysToWindowImmediately(...)` (focus-free; routes to the focused element, else `document`/window — only *you* know what should be focused) and the `*Immediately` plural verbs (`ClickEachImmediately`, `SetPropertyForEachImmediately`) act now with no readiness to fold in, so they carry the classic race. Gate explicitly — for `SendKeysToWindowImmediately`, on *focus*:

```go
Eventually("input.search").Should(b.BeFocused())   // gate on focus…
b.SendKeysToWindowImmediately(biloba.Keys.Enter)   // …then send once
```

(To send keys into a specific element, prefer `b.Type(sel, ...)` — it focuses first and *polls*, so it needs no hand-gate.)

## Smell 3 — optimistic UI + server reconciliation (the DOM lies)

If your app renders **optimistically** and then a server frame (WS/poll) reconciles to the authoritative state, the DOM you see right after an action is the *pre-confirmation* copy. It can momentarily revert or reorder ("1-frame blink"), and under load can even settle stably-wrong (two async write paths racing). Asserting on the DOM right after the action catches the blink — and **`Eventually` on the DOM cannot save you, because the DOM *is* the optimistic copy** it keeps re-reading.

**Fix: wait on a *server-authoritative* signal, not on anything visible.** Either (a) poll an endpoint your app exposes (`Eventually(func() string { …GET /sessions/{id}… }).Should(Equal("confirmed"))`), or (b) ensure the action is durably acknowledged before the next step. This is not a Biloba bug — but Biloba specs are uniquely exposed to it, so name it when a "waited and still flaky" mystery appears.

## Smell 4 — async-settling geometry / layout / document-order reads

`getBoundingClientRect`, `scrollHeight`/`clientHeight` overflow checks, computed `display`/`getComputedStyle`, and `compareDocumentPosition` of rAF-injected nodes all settle **after the element exists**. A spec that gates on "element exists" and *then* reads geometry races the *measure* — a distinct category from "is it there yet." The element being present does not mean it's been laid out.

Nearly all of these reads now have a native, layout-aware Biloba expression — reach for it before `b.Run`. Box/scroll/offset reads use `b.GetBoundingBox`/`b.GetScrollOffset`/`b.GetOffsetTopWithin` and their `Have*` matchers (all wait for a non-degenerate box) — and `Box` carries both the **border-box** (`Width`/`Height`, scrollbar gutter included) and the **client box** (`ClientWidth`/`ClientHeight`, scrollbar-excluded content area) so "content width of this scroll container" needs no `b.Run`. Relational layout uses the **pairwise** matchers `b.BeAbove`/`BeBelow`/`BeLeftOf`/`BeRightOf`/`Encloses`/`Overlaps` and the `b.GetGapBetween`/`HaveGapBetween` delta getter (both elements read in one atomic frame). On-screen-ness uses `b.BeInViewport()` (partial overlap; `b.BeInViewport(b.Fully())` for the whole box on screen); document order uses `b.BePrecededBy`/`b.BeFollowedBy` (**read the subject first**: `Eventually(X).Should(b.BeFollowedBy(Y))` ⇔ X precedes Y); computed style uses `b.GetComputedStyle`/`HaveComputedStyle` (resolves custom properties). Drop to `Eventually(b.Run)` only for the genuinely specialized reads these don't cover (per-line `getClientRects` wrap detection, SVG path-point geometry, atomic act-then-measure):

```go
Eventually("#card").Should(b.HaveBoundingBox(HaveField("Height", BeNumerically("<=", 0.8*viewportH))))
Eventually("#tab").Should(b.BeAbove("#tile"))                // relational — one atomic two-box probe
Eventually("#note").Should(b.BeInViewport(b.Fully()))       // wholly on screen, not merely laid out
hex := b.GetComputedStyle(".rail", "--stage")               // resolved value (custom properties too)
```

**The inverse case — a geometry poll that times out *consistently* (not intermittently).** Under load this looks identical to "needs a bigger timeout," but it usually means the **product** computed a position once and never reconciled — not a slow test. The DOM you're polling is real, but if the page never re-runs the computation `Eventually` can't save you: the value is *stably wrong*, so it sits above threshold for the whole deadline. The fix is product-side (rAF-settle until the value holds, plus a bounded `ResizeObserver` to catch growth-above-the-target after the rAF loop exits), **not** a wider timeout. This mirrors the optimistic-UI trap (Smell 3): `Eventually` on the DOM can't save you when the DOM *is* the optimistic copy — same shape, different axis. The **poll trajectory** Biloba attaches on failure (see `biloba:debug-failures`) is the tell: a flat line = product bug, a monotone approach = latency, a dip-then-rebound = a late reflow.

## Smell 5 — a two-axis getter polling forever on a property the element type doesn't have

The value-getters `GetProperty`/`GetProperties`/`GetAttribute`/`GetAttributes` poll on **two axes**: until the element is present **and** every named property/attribute is *defined*. That's the desired behavior for something that fills in asynchronously (`dataset.poster` populated by a late render). But it bites when the name simply *doesn't exist on that element type* — `b.GetProperty("div.card", "disabled")` (a `<div>` has no `disabled`, so `"disabled" in div` is false) **polls until timeout**, then fails, even though the element was there all along. Same for an attribute that legitimately may be absent.

**Fix: wrap the name in `b.AllowMissing(...)`** — it exempts that name from the "defined" axis, so an absent value comes back as `nil` and never blocks the poll:

```go
b.GetProperty("div.card", b.AllowMissing("disabled"))           // nil instead of a timeout
b.GetProperties("#user", "dataset.firstName", b.AllowMissing("dataset.middleName"))
```

(`GetValue`/`GetInnerText`/`GetTextContent` have no "defined" axis — empty string / unselected-radio `""` is a valid value — so they poll on presence only and never need `AllowMissing`.)

A flip-side, *anti*-flake improvement to know about: the `Each*` matchers (`EachBeVisible`/`EachBeEnabled`/`EachHaveClass`/`EachHaveInnerText`/`EachHaveProperty`/…) now **fail on zero matches** ("≥1 match AND all satisfy") rather than passing vacuously. So `Eventually(sel).Should(b.EachBeVisible())` correctly *waits for the elements to appear* instead of passing instantly against an empty set — a former silent false-positive is now a real poll. To assert that nothing matches, use `Eventually(sel).Should(b.HaveCount(0))` or `ShouldNot(b.Exist())` (the old no-arg `EachHaveInnerText()`/`EachHaveTextContent()` "is empty" forms are gone).

## Fast interactions act in place — they don't scroll or move focus

A useful non-flake fact, since the opposite is easy to assume: **fast-track `b.Click`/`b.Tap` do *not* `scrollIntoView` and do *not* move focus** — a plain fast click is `element.click()` after a visibility check, nothing more. So a fast click never moves the page out from under a scroll/layout assertion; if a scroll position changes around a click, the cause is app-side (a click handler) — don't blame Biloba. Scroll-into-view comes only from **`b.Realistic()`** (which scrolls deliberately) and from **focus-bearing ops** — `b.Focus`, `b.SetValue`, `b.Type` — because the browser's `.focus()` scrolls its target into view by default. If a spec asserts on scroll position, keep focus-bearing ops away from the element under test (or read scroll + act in one atomic `b.Run`).

## The throughline, restated

Every smell above is one read at one instant. Gate readiness, poll outcomes, wait on authoritative signals — and a browser test stops being a coin flip. When you've localized a flake but need to *see* the state at failure (full DOM, console errors, app-store snapshot), go to `biloba:debug-failures`.

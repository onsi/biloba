---
name: flaky-specs
description: Diagnose and prevent flaky Biloba specs — tests that pass locally but fail in CI, fail intermittently under `ginkgo -p`, or fail "somewhere else" than the line that's actually wrong. The throughline — a browser test should never assert on a value it read exactly once — and the concrete smells behind it: single-shot `b.Run(expr,&x)` reads, immediate interactions that race silently and surface the failure later, optimistic-UI/server-reconciliation traps, and async-settling geometry/layout/document-order reads. Use when a browser spec is flaky, nondeterministic, order-dependent, or load-sensitive, or when reviewing a suite for latent races.
---

# Avoiding & fixing flaky Biloba specs

The one rule that prevents almost every Biloba flake:

> **Never assert on a value you read exactly once.** A browser is a pile of async settles — a WS frame, a layout/measure pass, an rAF-scheduled DOM injection, an optimistic→authoritative reconciliation. Any single read can land *before* the thing you care about settles. Poll it instead.

Biloba's ergonomics quietly invite the single-shot read (it has a clean "read a value directly" API), so this is the first thing to suspect when a spec flakes. The smells below are the recurring shapes; each has a polling fix. For the failure-artifact side (reading outlines/screenshots once a spec *has* failed) see `biloba:debug-failures`; for the authoring baseline see `biloba:write-tests`.

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

## Smell 2 — an immediate interaction that races, and fails *somewhere else*

`b.Click(sel)`, `b.Tap(sel)`, `b.SelectText(sel)`, `b.SetValue(sel, …)`, a raw `b.Run(…click())` — every **immediate** (fully-applied) form acts *now* and **does not poll** (Biloba never polls itself). Fire one a frame too early — right after a re-render, a list load, a hero/card injection — and it no-ops or hits a stale element. The cruel part: **the spec doesn't fail at the interaction.** It fails later, at the assertion that depended on it — a downstream `Eventually(...class…)` that times out, or a `null is not an object` from the app's own handler — with nothing pointing back at the racing interaction.

**Fix: gate the interaction on a readiness anchor, then act once, then assert the outcome.**

```go
Eventually(sel).Should(b.BeClickable())   // 1. wait until it's really there & actionable
b.Click(sel)                              // 2. act once
Eventually(out).Should(b.HaveClass("open")) // 3. assert the observable outcome
```

The matcher form folds steps 1–2 together — `Eventually(sel).Should(b.Click())` polls until clickable, *then clicks once*. Prefer it for a plain action.

**Toggles need care — don't put a toggle action inside `Eventually`.** `Eventually(sel).Should(b.Tap())` re-taps every poll, so against a *toggle* it oscillates (open/close/open) and "passes" or "fails" by luck. Drive a toggle to a **target state idempotently** instead: gate, tap once, then poll the end state —

```go
Eventually(sel).Should(b.Exist())
b.Tap(sel)                                  // single toggle
Eventually(sel).Should(b.HaveClass("collapsed"))
```

— or, when a transient re-render can swallow the tap, do an atomic "tap only if not already in the target state" in one `b.Run` eval so the check and the act can't be split by a round-trip.

## Smell 3 — optimistic UI + server reconciliation (the DOM lies)

If your app renders **optimistically** and then a server frame (WS/poll) reconciles to the authoritative state, the DOM you see right after an action is the *pre-confirmation* copy. It can momentarily revert or reorder ("1-frame blink"), and under load can even settle stably-wrong (two async write paths racing). Asserting on the DOM right after the action catches the blink — and **`Eventually` on the DOM cannot save you, because the DOM *is* the optimistic copy** it keeps re-reading.

**Fix: wait on a *server-authoritative* signal, not on anything visible.** Either (a) poll an endpoint your app exposes (`Eventually(func() string { …GET /sessions/{id}… }).Should(Equal("confirmed"))`), or (b) ensure the action is durably acknowledged before the next step. This is not a Biloba bug — but Biloba specs are uniquely exposed to it, so name it when a "waited and still flaky" mystery appears.

## Smell 4 — async-settling geometry / layout / document-order reads

`getBoundingClientRect`, `scrollHeight`/`clientHeight` overflow checks, computed `display`/`getComputedStyle`, and `compareDocumentPosition` of rAF-injected nodes all settle **after the element exists**. A spec that gates on "element exists" and *then* reads geometry races the *measure* — a distinct category from "is it there yet." These are exactly the reads to wrap in `Eventually(b.Run)` (Smell 1): the element being present does not mean it's been laid out.

```go
Eventually("#card").Should(b.Exist())                       // present...
Eventually(b.Run).WithArguments(`document.querySelector("#card").offsetHeight`).
    Should(BeNumerically("<=", 0.8*viewportH))              // ...but measure must still be polled
```

## Fast interactions act in place — they don't scroll or move focus

A useful non-flake fact, since the opposite is easy to assume: **fast-track `b.Click`/`b.Tap` do *not* `scrollIntoView` and do *not* move focus** — a plain fast click is `element.click()` after a visibility check, nothing more. So a fast click never moves the page out from under a scroll/layout assertion; if a scroll position changes around a click, the cause is app-side (a click handler) — don't blame Biloba. Scroll-into-view comes only from **`b.Realistic()`** (which scrolls deliberately) and from **focus-bearing ops** — `b.Focus`, `b.SetValue`, `b.Type`, `b.SendKeys` — because the browser's `.focus()` scrolls its target into view by default. If a spec asserts on scroll position, keep focus-bearing ops away from the element under test (or read scroll + act in one atomic `b.Run`).

## The throughline, restated

Every smell above is one read at one instant. Gate readiness, poll outcomes, wait on authoritative signals — and a browser test stops being a coin flip. When you've localized a flake but need to *see* the state at failure (full DOM, console errors, app-store snapshot), go to `biloba:debug-failures`.

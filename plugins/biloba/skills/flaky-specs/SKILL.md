---
name: flaky-specs
description: Diagnose and prevent flaky Biloba specs — tests that pass locally but fail in CI, fail intermittently under `ginkgo -p`, or fail "somewhere else" than the line that's actually wrong. Biloba polls by default, so the headline rule is "don't reach for b.Immediate()"; the residual smells — single-shot `b.Run(expr,&x)` reads and gate-then-re-read pairs (fix with .Capture), the non-polling SendKeysToWindowImmediately/`*Immediately` verbs, optimistic-UI/server-reconciliation traps (the DOM is the optimistic copy AND a Go-side HTTP read bypasses the browser — barrier on app state with b.GetJSValue, but only against a lazily-created path; force the arrival order with b.HoldResponse + Limit/ReleaseNext), async-settling geometry/layout/document-order reads (incl. a backwards order assertion that passes silently), AllowMissing for absent-on-type properties, network handlers accumulating across an Ordered container (silent in both the timing-out and the silently-green direction), vacuous assertions that can never fail (an unresolved locator scope, BeNetworkIdle before the request starts, an empty Current*ForEach under a negation), and gating on a DOM anchor instead of the URL. Use when a browser spec is flaky, nondeterministic, order-dependent, or load-sensitive, or when reviewing a suite for latent races.
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

**The subtler sibling: gate-then-re-read.** Not a single-shot read — *two* reads, which is worse, because the first one looks like a guard:

```go
Eventually(".figure-frame").Should(b.HaveAttribute("data-block-id", Not(BeEmpty())))  // read #1: it's set
id := b.GetAttribute(".figure-frame", "data-block-id")                                // read #2: …of what?
```

Between the assertion and the getter the page can re-render, re-key the list, or swap the node — so the value you carry forward is one nothing ever asserted on (classic TOCTOU). **`.Capture(&x)` collapses it to one read**: every value-reading matcher writes what it observed on the successful match.

```go
var blockID string
Eventually(".figure-frame").Should(b.HaveAttribute("data-block-id", Not(BeEmpty())).Capture(&blockID))
```

It decodes into your Go type (no `float64` type-assert dance) and writes only on a match — so under `ShouldNot` nothing is captured. Same idea for the `any`-returning getters: `b.GetProperty("#row", "offsetWidth", &n)` takes an optional trailing pointer instead of a type assertion.

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

**A conditional interaction is a spec smell — fix your spec's state determinism, not the click.** When you catch yourself writing "click X *only if* it's in state S" — a `HasElement`/branch before an action, an `if collapsed { click }` inside `Eventually`, any *guarded* or *conditional* interaction — stop. **It means the spec doesn't know its own state**, and in a controlled browser test it always can. Branching to paper over that uncertainty is the wrong layer; worse, it's an active flake source (a check-then-act loop re-clicks on every poll tick and *oscillates* a toggle right back). The fix is **upstream determinism**, not a guarded click:

- **Own your fixture.** If the uncertainty comes from inherited state (a shared `Ordered` block whose earlier specs left a disclosure open-or-closed), create your *own* fresh element in a known state instead of grabbing whatever already exists — the conditional deletes itself.
- **Barrier on the authoritative signal.** If it comes from an optimistic-UI reconcile window (the DOM might still blink back to its pre-confirmation state), wait for the app to actually *fold* the response — the app-state barrier `b.GetJSValue`, not the DOM and not a Go-side HTTP read (Smell 3) — before interacting; then a plain `b.Click` + `Eventually(HaveClass)` is deterministic.

**The idempotence corollary** (why there's no `ClickWhen`-style primitive to reach for): the sanctioned set-and-confirm idiom re-fires its action every poll iteration, which is safe **iff the action is idempotent**. `SetValue("#x", 3)` is idempotent — re-running it converges. A **toggle click is not** — re-running it oscillates. So the very composition shape that's correct for `SetValue` is a flake for a conditional toggle click. That asymmetry is the tell: a spec that *needs* a guarded, non-idempotent interaction is a spec that hasn't pinned its own state. Pin the state.

**The poll-by-default action does not check occlusion — keep an explicit `BeClickable` gate when an overlay may cover the target.** `b.Click(sel)` polls on visible + enabled, but a fast click is `element.click()`; it does **not** verify the element is the topmost thing at its center, so it will happily "click" through a modal/overlay sitting on top. When occlusion is possible, gate with `Eventually(sel).Should(b.BeClickable())` (which adds the topmost-at-center check) before acting, or use `b.Realistic()` (which refuses to click through an overlay). Poll-by-default alone won't catch it. **The click is still occlusion-blind by design — but it now leaves a trail**: because a swallowed click fails *downstream*, pointing nowhere useful, Biloba records a hit-test at click time and names the occluder in the failure artifacts (`biloba:debug-failures`). Nothing about whether the spec passes changes; you just stop having to guess.

**Two interactions can't poll — gate them by hand.** `b.SendKeysToWindowImmediately(...)` (focus-free; routes to the focused element, else `document`/window — only *you* know what should be focused) and the `*Immediately` plural verbs (`ClickEachImmediately`, `SetPropertyForEachImmediately`) act now with no readiness to fold in, so they carry the classic race. Gate explicitly — for `SendKeysToWindowImmediately`, on *focus*:

```go
Eventually("input.search").Should(b.BeFocused())   // gate on focus…
b.SendKeysToWindowImmediately(biloba.Keys.Enter)   // …then send once
```

(To send keys into a specific element, prefer `b.Type(sel, ...)` — it focuses first and *polls*, so it needs no hand-gate.)

## Smell 3 — optimistic UI + server reconciliation (both obvious signals lie)

If your app renders **optimistically** and then a server frame (WS/poll/fetch) reconciles to the authoritative state, the two signals you'd naturally reach for **both prove something other than what you meant**, and a spec built on either can be silently vacuous — green, and testing nothing:

- **The DOM is the optimistic copy.** The click handler wrote it synchronously, before any round trip. Asserting on it proves *the handler ran* — nothing about the response. And **`Eventually` cannot save you here**, because it just keeps re-reading that same optimistic copy. (Under load it can even settle *stably wrong* when two async write paths race, so it doesn't even flake honestly.)
- **A Go-side HTTP read bypasses the browser's event loop entirely.** `Eventually(func() string { …GET /sessions/{id}… }).Should(Equal("confirmed"))` proves *the server persisted something* — not that the tab ever received, parsed, or folded the response. **This is the non-obvious half, and it is the trap that eats the most time** precisely because it looks so much like the right fix for the first one. Both traps can be true simultaneously while the tab has done nothing at all with the result.

**Fix: barrier on *app state* — a signal the renderer itself produces — via `b.GetJSValue`.** Have the app record its own fold decisions on a `window.__x` path (a store subscriber, a reducer log, a `lastFold` stamp), then poll that. It is the only one of the three signals that can only become true if the browser actually processed the response:

```go
b.Run(`app.store.on("fold", () => { window.__storeLog ??= []; window.__storeLog.push(app.store.state) })`)
b.Click("#save")
var log []string
b.GetJSValue("window.__storeLog", &log)   // polls until defined — i.e. until the fold really happened
Ω(log).Should(HaveExactElements("saving", "saved"))
```

`b.GetJSValue(expr[, &ptr])` polls until `expr` is **defined**, retrying through both `undefined` and a *thrown* error — a `ReferenceError` for a global the app hasn't created yet is a not-ready condition, not a bug — so you can install the probe and barrier on it without racing its creation. `null` is a legitimate value and returns immediately; the optional pointer decodes into a concrete type (and dodges the JSON-`float64` gotcha).

> **The `??=` above is load-bearing — and this is the trap inside the fix.** That comment ("polls until defined — i.e. until the fold really happened") is true *only* because the log is created **lazily, by the subscriber**, so "defined" and "at least one fold happened" are the same event. `GetJSValue` gates on definedness and nothing else. Point it at a log the app creates **eagerly** — `this.__log = []` in the store constructor, which is the natural implementation for any log that's part of the product rather than a test fixture (a ring buffer has to be live for a real session) — and it is defined from page load: the barrier returns `[]` on the first tick and gates **nothing**. The spec goes green, in a barrier whose entire purpose was to be the signal that can't be faked, and it never flakes, so nothing ever draws attention to it. Vacuous, permanently.
>
> **For an eagerly-created path, barrier on the *predicate* instead** — one read, one poll, typed result:
>
> ```go
> var log []FoldEntry
> Eventually(`window.__storeLog`).Should(b.EvaluateTo(ContainElement(HaveKeyWithValue("state", "saved"))).Capture(&log))
> ```
>
> Note the asymmetry: `EvaluateTo` hands its sub-matcher the **raw JSON-decoded** value (an `[]any` of `map[string]any` — match with `HaveKeyWithValue`, not `HaveField`), while `Capture` gives you the typed `[]FoldEntry`.
>
> **Rule of thumb:** if you didn't write the `??=` yourself in the line above, check how the path is created before trusting `GetJSValue` as a barrier.

**Counter-indication: `GetJSValue` is the wrong tool wherever *absence is meaningful*.** Because it waits for existence, it cannot express a probe where the global being missing is a valid reading — a write ledger missing on `about:blank` between navigations (missing means *quiet*, not "wait"); `window.__renderErrors` missing on a page that never booted the app (missing means *no errors*); a flag planted before a JS-only tab switch where the assertion is that it **survived** (waiting for it inverts the test); a baseline count taken *before* the action. Those stay `b.Run` with a defensive coalesce — `b.Run("window.__ledger ?? null")` — and that is correct, not a smell to convert.

### Force the arrival order with `b.HoldResponse` — don't hope for it

The bug in an optimistic-UI reconciliation is almost always an *ordering* bug: a stale response landing after a newer local write and clobbering it. It reproduces naturally at maybe 1% — which means **"I ran it 30 times and it was green" proves nothing**, and a fix "verified" that way is unverified. The only honest test is one that *forces* the order:

```go
hold := b.HoldResponse(ContainSubstring("/api/settings"))
b.Click("#refresh")           // fires the request…
hold.Await()                  // …and blocks until its response is genuinely held in flight
b.Click("#rename")            // drive the app into the racy window while the response is frozen
hold.Release()                // now let the stale response land — on your schedule, every run
Ω(hold.Count()).Should(Equal(1))
var log []string
b.GetJSValue("window.__storeLog", &log)             // and barrier on the fold, per above
Ω(log).Should(HaveExactElements("renamed"))         // the stale response must NOT have clobbered it
```

This replaces the usual hand-roll — a channel plus an atomic first-only counter plus a pass-through echo plus a separate gate proving interception even happened — with a handful of lines. `Await` honors `WithTimeout`/`WithContext` set on the tab you build the hold from (`b.WithTimeout(d).HoldResponse(url)`) and otherwise waits 30s; `Count()` is a snapshot but safe to poll (`Eventually(hold.Count)`); every hold is force-released at spec end and by `Prepare()`, so a failing spec can never wedge the tab for the specs after it.

**Know the default before you design the ordering: a hold freezes EVERY matching response, not just the first.** The second request to the same URL is frozen too, and a bare `Release()` frees them all *and* disarms the hold. So if the ordering you're testing is "response #1 is still in flight while response #2 lands," the default can never produce it — cap the hold and let the overflow through:

```go
hold := b.HoldResponse(ContainSubstring("/api/save")).Limit(1)
b.Click("#save"); hold.Await()                            // #1 is held…
b.Click("#save"); Eventually(hold.Count).Should(Equal(2)) // …#2 passes straight through and lands
hold.Release()                                            // now the stale #1 lands, last
```

To step responses through one at a time while keeping the hold armed, use `hold.ReleaseNext()` (releases the oldest still held; fails loudly when nothing is held — a release with nothing to release means your sequencing is off) or `hold.Release(r)` with the response `Await()` handed you. `Await()` always returns the **oldest response still held**, so `Await`/`ReleaseNext`/`Await` walks them in arrival order.

**Its one sharp edge:** matching is **tab-wide and URL-based**, so a hold can catch a response belonging to an *earlier page load* — a URL substring does not identify a page generation. If the flow navigates, scope it to a dedicated `b.NewTab()` (handler lists are per-tab) or assert `Count()` to prove you held the response you meant. And because handlers are first-match-wins, a **second `HoldResponse` for a URL an earlier one already claims is dead code** — re-arm the hold you already have with `ReleaseNext` rather than registering another (Smell 6).

## Smell 4 — async-settling geometry / layout / document-order reads

`getBoundingClientRect`, `scrollHeight`/`clientHeight` overflow checks, computed `display`/`getComputedStyle`, and `compareDocumentPosition` of rAF-injected nodes all settle **after the element exists**. A spec that gates on "element exists" and *then* reads geometry races the *measure* — a distinct category from "is it there yet." The element being present does not mean it's been laid out.

Nearly all of these reads now have a native, layout-aware Biloba expression — reach for it before `b.Run`. Box/scroll/offset reads use `b.GetBoundingBox`/`b.GetScrollOffset`/`b.GetOffsetTopWithin` and their `Have*` matchers (all wait for a non-degenerate box) — and `Box` carries both the **border-box** (`Width`/`Height`, scrollbar gutter included) and the **client box** (`ClientWidth`/`ClientHeight`, scrollbar-excluded content area) so "content width of this scroll container" needs no `b.Run`. Relational layout uses the **pairwise** matchers `b.BeAbove`/`BeBelow`/`BeLeftOf`/`BeRightOf`/`Encloses`/`Overlaps` and the `b.GetGapBetween`/`HaveGapBetween` delta getter (both elements read in one atomic frame). On-screen-ness uses `b.BeInViewport()` (partial overlap; `b.BeInViewport(b.Fully())` for the whole box on screen); document order uses `b.BePrecededBy`/`b.BeFollowedBy` (**read the subject first**: `Eventually(X).Should(b.BePrecededBy(Y))` ⇔ X comes **AFTER** Y; `Eventually(X).Should(b.BeFollowedBy(Y))` ⇔ X comes **BEFORE** Y — see the silent-pass warning below); computed style uses `b.GetComputedStyle`/`HaveComputedStyle` (resolves custom properties). Drop to `Eventually(b.Run)` only for the genuinely specialized reads these don't cover (per-line `getClientRects` wrap detection, SVG path-point geometry, atomic act-then-measure):

```go
Eventually("#card").Should(b.HaveBoundingBox(HaveField("Height", BeNumerically("<=", 0.8*viewportH))))
Eventually("#tab").Should(b.BeAbove("#tile"))                // relational — one atomic two-box probe
Eventually("#note").Should(b.BeInViewport(b.Fully()))       // wholly on screen, not merely laid out
hex := b.GetComputedStyle(".rail", "--stage")               // resolved value (custom properties too)
```

**A backwards document-order assertion doesn't flake — it goes green.** `BePrecededBy`/`BeFollowedBy` keep their names deliberately, but they read backwards to most people, and an inverted assertion **does not announce itself**: on a fixture that happens to satisfy the inverted relation it simply passes, so the spec is silently testing the opposite of what you meant. That's worse than a flake (a flake at least tells you something). The guard is to pin the direction from both sides:

```go
Eventually(noteSel).Should(b.BePrecededBy(sectionSel))   // the note comes AFTER the section
Ω(noteSel).ShouldNot(b.BeFollowedBy(sectionSel))         // …and not before it
```

(When one of these does fail, the message now reports the order actually observed — `Actually: #o-first comes BEFORE #o-second.` — which usually spots an inversion at a glance. Note "anywhere after" includes *inside*: scope with `Locator.NotWithin` when you mean "after Y but not nested in Y".)

**The inverse case — a geometry poll that times out *consistently* (not intermittently).** Under load this looks identical to "needs a bigger timeout," but it usually means the **product** computed a position once and never reconciled — not a slow test. The DOM you're polling is real, but if the page never re-runs the computation `Eventually` can't save you: the value is *stably wrong*, so it sits above threshold for the whole deadline. The fix is product-side (rAF-settle until the value holds, plus a bounded `ResizeObserver` to catch growth-above-the-target after the rAF loop exits), **not** a wider timeout. This mirrors the optimistic-UI trap (Smell 3): `Eventually` on the DOM can't save you when the DOM *is* the optimistic copy — same shape, different axis. The **poll trajectory** Biloba attaches on failure (see `biloba:debug-failures`) is the tell: a flat line = product bug, a monotone approach = latency, a dip-then-rebound = a late reflow.

## Smell 5 — a two-axis getter polling forever on a property the element type doesn't have

The value-getters `GetProperty`/`GetProperties`/`GetAttribute`/`GetAttributes` poll on **two axes**: until the element is present **and** every named property/attribute is *defined*. That's the desired behavior for something that fills in asynchronously (`dataset.poster` populated by a late render). But it bites when the name simply *doesn't exist on that element type* — `b.GetProperty("div.card", "disabled")` (a `<div>` has no `disabled`, so `"disabled" in div` is false) **polls until timeout**, then fails, even though the element was there all along. Same for an attribute that legitimately may be absent.

**Fix: wrap the name in `b.AllowMissing(...)`** — it exempts that name from the "defined" axis, so an absent value comes back as `nil` and never blocks the poll:

```go
b.GetProperty("div.card", b.AllowMissing("disabled"))           // nil instead of a timeout
b.GetProperties("#user", "dataset.firstName", b.AllowMissing("dataset.middleName"))
```

**This failure is now self-explaining — read it before you go hunting.** When a two-axis getter times out because the *property* never became defined (rather than because the element never appeared), the message says so outright: it reports that the element was present the whole time (or only for part of the poll), names the property that stayed undefined, and prints the exact `b.AllowMissing("disabled")` to paste. So a timeout on a getter that names a property is a two-second diagnosis, not an investigation.

(`GetValue`/`GetInnerText`/`GetTextContent` have no "defined" axis — empty string / unselected-radio `""` is a valid value — so they poll on presence only and never need `AllowMissing`.)

A flip-side, *anti*-flake improvement to know about: the `Each*` matchers (`EachBeVisible`/`EachBeEnabled`/`EachHaveClass`/`EachHaveInnerText`/`EachHaveProperty`/…) now **fail on zero matches** ("≥1 match AND all satisfy") rather than passing vacuously. So `Eventually(sel).Should(b.EachBeVisible())` correctly *waits for the elements to appear* instead of passing instantly against an empty set — a former silent false-positive is now a real poll. To assert that nothing matches, use `Eventually(sel).Should(b.HaveCount(0))` or `ShouldNot(b.Exist())` — not the no-arg `EachHaveInnerText()`/`EachHaveTextContent()`, which no longer mean "every text is empty" (they now assert the property is *defined* on every match, and hand you the slice via `.Capture`).

## Smell 6 — network handlers accumulating across an `Ordered` container (order-dependence, not flake)

Network handlers (`StubRequest`/`AbortRequest`/`ModifyRequest`/`ModifyResponse`/`HoldResponse`) are consulted **first-match-wins in registration order**, and `Prepare()` is what clears them. Inside an `Ordered` container with `BeforeEach(func(){ b.Prepare() }, OncePerOrdered)`, **`Prepare()` does not run between the `It`s** — so handlers *accumulate*: a handler registered in an earlier spec permanently claims that URL, and an identical handler registered by a later spec is **silent dead code**. No error, no warning; it simply never runs.

**The signature is distinctive, so learn the detector verbatim: a spec whose interception gate times out only when run with the rest of the suite, and passes when focused alone → check what ran before it in the same process, before diagnosing anything else.** It presents exactly like a flake or a too-short timeout, and it is neither.

**Shadowing is silent in BOTH directions — and the green direction is the dangerous one.** The timeout above is the presentation you'll notice. The other presentation is a spec that **passes**, because the leftover handler did something reasonable-looking with the response.

The shape to recognize is a **stateful** handler — one whose behavior depends on a counter it keeps:

```go
b.ModifyResponse(url).Using(func(r biloba.InterceptedResponse) biloba.StubResponse {
    if atomic.AddInt32(&intercepted, 1) == 1 { /* hold/mangle only the first */ }
    return passthrough
})
```

Registered in an earlier `It`, that handler's counter is already at 1 by the time a later `It` registers its identical one. The later handler is dead code (first-match-wins); the leftover claims the response and — because its counter has moved on — **passes it through untouched**. The page behaves normally, nothing hangs, and the only casualty is the later spec's own counter, which stays 0. **"The hold worked" is exactly what a shadowed hold looks like from the app's side**, so nothing about the app's behavior can tell you. The assertion that catches it is the one on your own interception state:

```go
Eventually(hold.Count).Should(Equal(1))                                  // prove YOUR hold ran
Eventually(func() int32 { return atomic.LoadInt32(&intercepted) }).Should(Equal(int32(1))) // …or your handler
```

Assert it in every spec that installs a handler it depends on. It costs one line and it is the difference between "the interception happened" and "something didn't visibly break."

**Biloba now tells you outright** — you don't have to reason your way there. When a spec fails, a handler that never fired *and* was shadowed by an earlier one is reported with **both registration sites**, so the dead handler and the one that claimed its URL are named for you (see `biloba:debug-failures`). Read that note before believing a timeout is a race. (Note the limit: it reports on **failure**, so the silently-green presentation above still needs your own `Count` assertion to surface.)

Two fixes:
- **Drive both orderings from a single `It`.** Usually reads better as a spec anyway ("A-then-B and B-then-A both converge" is one behavior).
- **Give the holding spec its own `b.NewTab()`.** Handler lists are per-tab, so a fresh tab starts with an empty one.

## Smell 7 — the assertion that cannot fail (vacuous green)

A flake at least tells you something. These pass **every time, forever**, against a page that is completely broken — and they are usually written *as the guard against exactly that breakage*. Where Biloba could make the vacuous case loud it has (see the end of this section); the rest are inherent to what the constructs mean, so you have to know them and anchor around them.

**A locator scope that doesn't resolve matches nothing.** `.Within(...)`/`.Containing(...)` narrow to descendants of the scope — so when the scope never renders, the locator matches zero elements and every *negative* assertion on it is satisfied instantly:

```go
// passes instantly and permanently if #published-list never renders — the white-screen
// failure this guard exists to catch:
Consistently(b.ByTextContains("Draft").Within("#published-list")).ShouldNot(b.Exist())
```

The anchored form is two lines and means what you meant:

```go
Eventually("#published-list").Should(b.Exist())                                        // the scope is real…
Consistently(b.ByTextContains("Draft").Within("#published-list")).ShouldNot(b.Exist())  // …so this bites
```

**`BeNetworkIdle` passes before your request has started.** It means "zero requests in flight *right now*", with no quiet period — so `b.Click("#refresh")` followed by `Eventually(b).Should(b.BeNetworkIdle())` can be satisfied at t=0, before the click's fetch ever left the page. Anchor it on the request first:

```go
b.Click("#refresh")
Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/refresh")))  // it started…
Eventually(b).Should(b.BeNetworkIdle())                                    // …and now it's done
```

**A `Current*ForEach` snapshot returns an empty slice when nothing matches** — which satisfies a *negated* collection assertion: `Ω(b.CurrentInnerTextForEach(".row")).ShouldNot(ContainElement("Draft"))` passes against zero rows. These getters don't poll, so you already owe them a presence gate for timing; the same gate is what keeps a negative honest:

```go
Eventually(".row").Should(b.HaveCount(3))
Ω(b.CurrentInnerTextForEach(".row")).ShouldNot(ContainElement("Draft"))
```

(Positive collection assertions are safer — Gomega's `HaveEach` errors on an empty slice, and Biloba's `Each*` matchers fail on zero matches. It's the negations that need the gate.)

**Two of this family Biloba could fix, and did** — both were negations satisfied by an element that wasn't there:
- **`ShouldNot(b.BeChecked())` on the wrong node.** Selecting the wrapping `<label>`/`<div>` instead of the `<input>` used to read a missing `checked` property, coerce `undefined` to `false`, and pass forever against something that could never be checked. An element with no `checked` property at all is now an **error**, so that mis-selection fails loudly.
- **`ShouldNot(b.BeInViewport())` (and the other geometry matchers) on an element that never rendered.** A missing element is now an **error** rather than a plain non-match, so the negation can't be satisfied by absence; the pairwise/offset probes even name *which* selector went missing (subject, container, or other). Nothing about waiting changes: under `Eventually` an error is a failed attempt, and a present-but-zero-area box is still a silent retry, so the positive direction keeps polling through late layout exactly as before.

(In both cases the *positive* assertion was never vacuous — it's the negation that was.)

**The geometry change takes a real spec with it, so know the replacement idiom.** Gomega counts an assertion satisfied only when the matcher gives the desired answer **and** doesn't error — in *either* direction — so an erroring matcher can't satisfy `ShouldNot` even when the absence is exactly what you were waiting for. The wait-for-teardown spec is the casualty:

```go
/* === used to pass the moment the toast left the DOM; now it times out === */
Eventually("#toast").ShouldNot(b.BeInViewport())

/* === assert disappearance with the matchers that are ABOUT existence === */
Eventually("#toast").ShouldNot(b.Exist())
Eventually("#toast").Should(b.HaveCount(0))
```

Same for `HaveBoundingBox`, `HaveScrollOffset`, `HaveOffsetTopWithin`/`HaveOffsetLeftWithin`, `HaveGapBetween`, the pairwise matchers, and any `Consistently(sel).ShouldNot(...)` where `sel` is conditionally rendered. The rule that falls out: **`Exist`/`HaveCount` answer "is it there?"; the geometry matchers answer "where is it?" and presuppose it's there.** That's how `BeVisible`/`BeEnabled`/`BeClickable` have always behaved, so the convention is now uniform instead of split.

The rest of this family lives with its smell: the **eagerly-created app-state barrier** (Smell 3), the **inverted document-order assertion** (Smell 4), and the **shadowed stateful handler** (Smell 6). The common tell is an assertion whose subject can be *empty or absent* and whose form treats empty-or-absent as success. When you write one, ask: what would make this fail? If the answer is "nothing, if the page never rendered," anchor it.

## Gate on a DOM readiness anchor, not on the URL

The URL flips the moment the router pushes it, so it is the *weakest* available proof that a navigation landed — and it's the one signal that races a live target swap. Gate on the DOM the destination renders, then read the location as a formality:

```go
b.Click("#submit")
Eventually("#dashboard-root").Should(b.Exist())     // strong signal: the destination actually rendered
Ω(b.GetLocation()).Should(HaveSuffix("/dashboard")) // …now a formality
```

`b.GetLocation()`/`b.GetTitle()` (a breaking rename — the old bare `Location`/`Title` names are gone) are polling getters that honor all four knobs, and they treat a transient CDP error — `Inspected target navigated or closed (-32000)`, thrown when Chrome swaps the target out mid-navigation — as a **retryable miss**. Previously that error aborted the poll outright, which bit exactly where you most want to poll a URL. So `Eventually(b.GetLocation).Should(...)` is now safe; it's just still the weaker assertion.

## Fast interactions act in place — they don't scroll or move focus

A useful non-flake fact, since the opposite is easy to assume: **fast-track `b.Click`/`b.Tap` do *not* `scrollIntoView` and do *not* move focus** — a plain fast click is `element.click()` after a visibility check, nothing more. So a fast click never moves the page out from under a scroll/layout assertion; if a scroll position changes around a click, the cause is app-side (a click handler) — don't blame Biloba. Scroll-into-view comes only from **`b.Realistic()`** (which scrolls deliberately) and from **focus-bearing ops** — `b.Focus`, `b.SetValue`, `b.Type` — because the browser's `.focus()` scrolls its target into view by default. If a spec asserts on scroll position, keep focus-bearing ops away from the element under test (or read scroll + act in one atomic `b.Run`).

## The throughline, restated

Every smell above is one read at one instant — or one signal that doesn't mean what it looks like it means. Gate readiness, poll outcomes, barrier on a signal only the browser could have produced, and force the orderings you're claiming to test instead of hoping for them. And note the corollary the smells share: the two worst outcomes here aren't red specs — they're a spec that's **silently vacuous** (Smell 3's DOM/HTTP traps and its eagerly-created barrier, Smell 4's inverted order assertion, all of Smell 7) and a spec that's **silently dead** (Smell 6's shadowed handler, in both its timing-out and its passing form). Neither announces itself, so both are worth grepping for proactively rather than waiting on. When you've localized a flake but need to *see* the state at failure (full DOM, console errors, poll trajectory, app-store snapshot), go to `biloba:debug-failures`.

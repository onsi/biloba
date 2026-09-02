---
name: flaky-specs
description: Diagnose and prevent flaky Biloba specs — tests that pass locally but fail in CI, fail intermittently under `ginkgo -p`, or fail somewhere other than the line that is actually wrong. Biloba polls by default, so the headline rule is "don't reach for b.Immediate()". Covers the residual smells: single-shot `b.Run(expr,&x)` reads and gate-then-re-read pairs (fix with .Capture); the non-polling SendKeysToWindowImmediately and `*Immediately` verbs; optimistic-UI and server-reconciliation traps (barrier on app state with b.GetJSValue, force the arrival order with b.HoldResponse + Limit/ReleaseNext); async-settling geometry, layout, and document-order reads; AllowMissing for properties absent on the element type; network handlers accumulating across an Ordered container; vacuous assertions that can never fail (an unresolved locator scope, BeNetworkIdle before the request starts, an empty Current*ForEach under a negation, a visual baseline written without ever being reviewed, a screenshot tolerance widened until nothing can fail, BILOBA_UPDATE_SCREENSHOTS left set in CI); and gating on a DOM anchor instead of the URL. Use when a browser spec is flaky, nondeterministic, order-dependent, or load-sensitive, or when reviewing a suite for latent races.
---

# Flaky Biloba specs

Biloba polls by default: `b.Click(sel)`, `b.SetValue(sel, v)`, `b.GetProperty(sel, name)` already wait for readiness, act once, then stop. So the residual flake sources are (a) reads you take yourself, (b) the few verbs that can't poll, (c) signals that don't mean what they look like, (d) assertions that can never fail.

**Master rule: never assert on a value you read exactly once.** Poll it.

Failure *artifacts* (outlines, screenshots, poll trajectory) → `biloba:debug-failures`. Authoring baseline → `biloba:write-tests`. Method surfaces → `biloba:api`.

## Triage

| Symptom | Cause | Fix | § |
|---|---|---|---|
| Passes focused, fails when run with the suite | Network handlers accumulated across an `Ordered` container | assert `hold.Count()`; own `b.NewTab()` | [6](#6-network-handlers-in-an-ordered-container) |
| Fails only under `-p` / load | Single-shot `b.Run` read racing an async settle | `Eventually(b.Run).WithArguments(expr)` | [1](#1-reads-you-take-yourself) |
| Fails at an assertion, not at the interaction before it | `b.Immediate()` acted a frame early | delete the `Immediate()` | [2](#2-actions-that-dont-poll) |
| Times out *consistently*, not intermittently | Value is stably wrong — product bug, not a short timeout | read the poll trajectory; fix product-side | [4](#4-layout-geometry-and-document-order) |
| Getter times out on an element that's plainly there | Property is never *defined* on that element type | `b.AllowMissing("name")` | [5](#5-two-axis-getters) |
| Green forever, even against a broken page | Vacuous assertion (empty scope / empty slice / t=0 idle) | anchor the subject first | [7](#7-assertions-that-cannot-fail) |
| Green in test, broken against a real server | Asserted the optimistic DOM, or a Go-side HTTP read | barrier on app state | [3](#3-optimistic-ui--server-reconciliation) |
| "I ran it 30× and it was green" | The ordering was never forced | `b.HoldResponse` | [3](#3-optimistic-ui--server-reconciliation) |
| Click appears to do nothing | Overlay swallowed it (fast `Click` is occlusion-blind) | `b.BeClickable()` gate, or `b.Realistic()` | [2](#2-actions-that-dont-poll) |
| Intermittent "could not find DOM element" on the line *after* a capture | The capture expanded the viewport; the page re-rendered on its breakpoint and unmounted the subject | capture something already in view (scroll, gate on `b.BeInViewport(b.Fully())`) | [7](#7-assertions-that-cannot-fail) |
| Assertion passes but asserts the opposite of your intent | Inverted `BePrecededBy`/`BeFollowedBy` | also assert the inverse doesn't hold | [4](#4-layout-geometry-and-document-order) |
| Suite ends on Ginkgo's `--timeout` with **no** failing spec | A CDP call blocked with no deadline — Gomega can't preempt a blocked callback, so no poll deadline fires | Biloba's own commands are bounded and now fail with `deadline_exceeded`/`page_crashed`/`browser_gone`; if you still hang, it's your own `chromedp` call on `b.Context` — give it a `context.WithTimeout` | `biloba:debug-failures` |

## 1. Reads you take yourself

**Single-shot `b.Run` read — the #1 flake source.** `b.Run` is `func(string, ...any) any`, so it drops straight into `Eventually`:

```go
// WRONG — reads the world once
var centered bool
b.Run(`isCentered()`, &centered)
Expect(centered).To(BeTrue())

// RIGHT — no wrapper closure needed for a scalar expr
Eventually(b.Run).WithArguments(`isCentered()`).Should(BeTrue())
Eventually(b.Run).WithArguments(`document.querySelector("#card").getBoundingClientRect().top`).
    Should(BeNumerically("~", 40, 1))   // JSON numbers decode to float64 — never Equal(intLiteral)
```

`WithArguments` needs a pre-built string. For an interpolated or multi-line expr, build it first (`expr := fmt.Sprintf(...)`) or poll a closure that returns the decoded value.

**Grep detector.** Anchor on the *decode target*, not on `b.Run(` — multi-line scripts put `, &x)` lines away from the call and slip the obvious regex:

```
rg ', &(\w+)\)' -n        # then check whether Expect(x)/Ω(x) follows within a few lines
```

**Gate-then-re-read (TOCTOU).** A matcher assertion followed by a getter is *two* reads; the page can re-render between them.

```go
// WRONG
Eventually(".figure-frame").Should(b.HaveAttribute("data-block-id", Not(BeEmpty())))
id := b.GetAttribute(".figure-frame", "data-block-id")   // …of what?

// RIGHT — .Capture writes the value from the read that satisfied the assertion
var blockID string
Eventually(".figure-frame").Should(b.HaveAttribute("data-block-id", Not(BeEmpty())).Capture(&blockID))
```

`.Capture` decodes into your Go type and writes only on a match (so `ShouldNot` captures nothing). The `any`-returning getters take the same optional trailing pointer: `b.GetProperty("#row", "offsetWidth", &n)`.

**Don't null-guard against a remount in a matcher.** A matcher poll re-resolves the selector from scratch every tick, so it retries straight through a portal migration or list re-key. `document.querySelector(sel)?.x ?? ''` guards belong only inside your own `b.Run` closure, where you hold a node reference.

```go
Eventually(sel).Should(b.HaveProperty("dataset.side", "left"))   // no guard needed
```

**Prefer native geometry getters to `b.Run`** — see §4.

## 2. Actions that don't poll

**Don't reach for `b.Immediate()`.** It acts once and fails fast; fire a frame early and the spec fails *downstream* (a timed-out `Eventually`, or the app's own `null is not an object`) with nothing pointing back. If the default wait is short, widen it — don't drop to `Immediate`.

```go
b.Click("#go")                                // polls until clickable, clicks once, stops
b.WithTimeout(5*time.Second).Click("#go")     // tune the wait
Eventually("#out").Should(b.HaveClass("open"))// assert the observable outcome
```

The matcher form (`Eventually(sel).Should(b.Click())`) is for when you want to own the poll (custom `Consistently`, `.And()` composition). It dispatches once on first success and never re-fires, so it's safe on a toggle.

**The one sanctioned `Immediate()`: set-and-confirm.** Inside a polling closure the action must be `Immediate()`, or you get a poll inside a poll (wasteful, and the inner timeout muddies the failure output):

```go
Eventually(func(g Gomega) {
    b.Immediate().SetValue("#qty", 3)      // acts once per iteration…
    g.Expect("#qty").To(b.HaveValue("3"))  // …until the value sticks against an optimistic revert
}).Should(Succeed())
```

Rule: **action inside a polling closure ⇒ `Immediate()`; action outside one ⇒ neither closure nor `Immediate()`.**

**A conditional interaction is a spec smell.** `if collapsed { click }`, a `HasElement` branch before an action — it means the spec doesn't know its own state, and a check-then-act loop re-clicks every tick and oscillates a toggle. Fix determinism upstream: create your own element in a known state instead of inheriting one from an earlier `Ordered` spec, or barrier on app state (§3) before interacting. (Idempotence corollary: set-and-confirm re-fires its action, so it's correct for `SetValue` and a flake for a toggle click. There is deliberately no `ClickWhen` primitive.)

**Fast `Click` is occlusion-blind by design.** It polls on visible + enabled, then calls `element.click()` — it will click straight through a modal scrim. When an overlay is possible:

```go
Eventually("#submit").Should(b.BeClickable())   // + topmost-at-center
b.Click("#submit")
// or: b.Realistic().Click("#submit")           // refuses to click through
```

The click records a hit-test and names the occluder in the failure artifacts, but only *after* a spec fails — it never changes whether one passes.

**Two verbs genuinely can't poll — gate them by hand.** `b.SendKeysToWindowImmediately(...)` (focus-free; only you know what should be focused) and the `*Immediately` plural verbs (`ClickEachImmediately`, `SetPropertyForEachImmediately`):

```go
Eventually("input.search").Should(b.BeFocused())
b.SendKeysToWindowImmediately(biloba.Keys.Enter)
```

To send keys into a specific element use `b.Type(sel, ...)` — it focuses first and polls.

## 3. Optimistic UI & server reconciliation

If the app renders optimistically and a later server frame reconciles, **both obvious signals lie**:

| Signal | What it actually proves |
|---|---|
| The DOM | The click handler ran. It's the optimistic copy — `Eventually` just re-reads it, and under load it can settle *stably wrong*. |
| A Go-side HTTP read (`GET /sessions/{id}`) | The server persisted something. It bypasses the browser's event loop entirely — the tab may have done nothing with the response. |
| `b.GetJSValue("window.__x")` — **app state** | The renderer itself applied the response. Use this. |

```go
b.Run(`app.store.on("update", () => { window.__storeLog ??= []; window.__storeLog.push(app.store.state) })`)
b.Click("#save")
var log []string
b.GetJSValue("window.__storeLog", &log)   // polls until defined; retries through undefined AND a thrown ReferenceError
Ω(log).Should(HaveExactElements("saving", "saved"))
```

**Trap inside the fix: `GetJSValue` gates on *definedness* and nothing else.** The `??=` above is what makes "defined" and "an update landed" the same event. Point it at a log the app creates **eagerly** (`this.__log = []` in the constructor — the natural implementation for anything shipped as product) and it returns `[]` on the first tick, gating nothing. Vacuous, permanently, and it never flakes so nothing draws attention. Against an eagerly-created path, poll the **predicate**:

```go
var log []SaveEntry
Eventually(`window.__storeLog`).Should(b.EvaluateTo(ContainElement(HaveKeyWithValue("state", "saved"))).Capture(&log))
```

(Asymmetry: `EvaluateTo`'s sub-matcher sees the **raw JSON-decoded** value — `[]any` of `map[string]any`, so `HaveKeyWithValue`, not `HaveField`. `.Capture` gives you the typed `[]SaveEntry`.) If you didn't write the `??=` yourself, check how the path is created before trusting the barrier.

**`GetJSValue` is the wrong tool wherever absence is meaningful** — a ledger missing on `about:blank` (missing = quiet), `window.__renderErrors` missing (= no errors), a flag that must have *survived* a JS-only tab switch, a baseline count taken before the action. Those stay `b.Run("window.__ledger ?? null")`. That's correct, not a smell.

### Force the arrival order with `b.HoldResponse`

Reconciliation bugs are ordering bugs (a stale response landing after a newer local write). They reproduce at ~1%, so "I ran it 30 times" proves nothing.

```go
hold := b.HoldResponse(ContainSubstring("/api/settings"))
b.Click("#refresh")     // fires the request…
hold.Await()            // …blocks until its response is genuinely held in flight
b.Click("#rename")      // drive the app into the racy window
hold.Release()          // let the stale response land, on your schedule, every run
Ω(hold.Count()).Should(Equal(1))
var log []string
b.GetJSValue("window.__storeLog", &log)
Ω(log).Should(HaveExactElements("renamed"))   // the stale response must not have clobbered it
```

**`Count`/`Release` are facts about the network, not the page.** `Count` says the response reached the tab's interceptor; `Release` returns when the release is signalled. Neither says the renderer has done anything with it — the barrier that actually proves that is the `GetJSValue` read above, not the `Release()` call. When the assertion is about what the app did with a response, pair the network fact with an app-state barrier (or the DOM the response produces) — never a sleep.

- **A hold freezes EVERY matching response by default**, and a bare `Release()` frees them all *and* disarms the hold. So "response #1 is still in flight while #2 lands" is not expressible with the default — cap it:
  ```go
  hold := b.HoldResponse(ContainSubstring("/api/save")).Limit(1)
  b.Click("#save"); hold.Await()                                    // #1 held…
  b.Click("#save"); Eventually(hold.PassedThrough).Should(Equal(1)) // …#2 passes straight through
  hold.Release()                                                    // now #1 lands, last
  ```
  Assert `PassedThrough`, not `Count` — the fixture under test is "#2 was NOT held," and `Count()==2` only says that by inference from the limit. A regression that raises the limit to 2 keeps `Count()==2` green while quietly destroying the ordering you pinned. `hold.Held()` is the other half — `Held()+PassedThrough()==Count()` always, and both are snapshots safe to poll.
- `hold.ReleaseNext()` releases the oldest still held and **stays armed** (fails loudly with nothing held — that means your sequencing is off). `Await()` always returns the oldest still held, so `Await`/`ReleaseNext`/`Await` walks arrival order.
- `Await` honors `WithTimeout`/`WithContext` set on the tab you built the hold from (`b.WithTimeout(d).HoldResponse(url)`); otherwise 30s. `Count()`/`Held()`/`PassedThrough()` are snapshots but safe to poll. Holds are force-released at spec end and by `Prepare()`.
- **Sharp edge: matching is tab-wide and URL-based**, so a hold can catch a response from an *earlier page load*. If the flow navigates, scope it to a `b.NewTab()` or assert `Count()`. And handlers are first-match-wins, so a **second `HoldResponse` for a URL an earlier one already claims is dead code** — re-arm the one you have with `ReleaseNext` (§6).

## 4. Layout, geometry, and document order

`getBoundingClientRect`, `scrollHeight`/`clientHeight`, `getComputedStyle`, and `compareDocumentPosition` on rAF-injected nodes all settle **after** the element exists. Gating on "element exists" then reading geometry races the *measure*.

Use the native, layout-aware expressions — they poll until the element is present **and** has a non-degenerate box:

```go
Eventually("#card").Should(b.HaveBoundingBox(HaveField("Height", BeNumerically("<=", 0.8*viewportH))))
Eventually(".hero .sec").Should(b.HaveOffsetTopWithin(".scroller", BeNumerically("<", 120)))
Eventually(".scroller").Should(b.HaveScrollOffset(HaveField("Top", BeNumerically("==", 0))))
Eventually("#tab").Should(b.BeAbove("#tile"))          // pairwise: both boxes read in one atomic frame
Eventually("#note").Should(b.BeInViewport(b.Fully()))  // wholly on screen, not merely laid out
hex := b.GetComputedStyle(".rail", "--stage")          // resolves custom properties
box := b.GetBoundingBox("#card")                       // Box{Top,Left,Width,Height,…,ClientWidth,ClientHeight}
```

`Box.Width`/`Height` are border-box; `ClientWidth`/`ClientHeight` are the scrollbar-excluded content box. Also: `b.GetScrollOffset`, `b.GetOffsetTopWithin`/`GetOffsetLeftWithin`, `b.BeBelow`/`BeLeftOf`/`BeRightOf`/`Encloses`/`Overlaps`, `b.GetGapBetween`/`HaveGapBetween`. Drop to `Eventually(b.Run)` only for what these don't cover (per-line `getClientRects` wrap detection, SVG path points, atomic act-then-measure, WebGL `gl.readPixels`, `elementFromPoint` hit-testing).

**A backwards document-order assertion goes green, it doesn't flake.** `Eventually(X).Should(b.BePrecededBy(Y))` ⇔ X comes **AFTER** Y; `b.BeFollowedBy(Y)` ⇔ X comes **BEFORE** Y. These read backwards to most people, and on a fixture that happens to satisfy the inversion the spec silently tests the opposite of what you meant. Pin the direction from both sides:

```go
Eventually(noteSel).Should(b.BePrecededBy(sectionSel))   // the note comes AFTER the section
Ω(noteSel).ShouldNot(b.BeFollowedBy(sectionSel))         // …and not before it
```

("Anywhere after" includes *inside* — scope with `Locator.NotWithin` when you mean "after Y but not nested in Y". On failure the message reports the order actually observed.)

**A geometry poll that times out *consistently* is a product bug, not a short timeout.** The DOM is real, but if the page computed the position once and never reconciled, the value is *stably wrong* and `Eventually` can't save you. Fix product-side (rAF-settle until the value holds, plus a bounded `ResizeObserver`), not with a wider timeout. The poll trajectory tells you which: **flat** = product bug, **monotone** = latency, **dip-then-rebound** = a late reflow (`biloba:debug-failures`).

## 5. Two-axis getters

`GetProperty`/`GetProperties`/`GetAttribute`/`GetAttributes` poll until the element is present **and** every named property/attribute is *defined*. That bites when the name doesn't exist on that element type — `b.GetProperty("div.card", "disabled")` polls to timeout even though the element was there all along.

```go
b.GetProperty("div.card", b.AllowMissing("disabled"))            // nil instead of a timeout
b.GetProperties("#user", "dataset.firstName", b.AllowMissing("dataset.middleName"))
```

The timeout message self-explains: it says the element was present, names the undefined property, and prints the exact `b.AllowMissing(...)` to paste. Read it before investigating.

**`AllowMissing` only pays off if the decode target can hold "absent."** A JS `null`/`undefined` decodes to the Go zero value, so a plain `*string` flattens "absent" and "present but empty" into the same `""`. Decode into a `**T` instead — absent leaves it `nil`, present allocates:

```go
var key *string
b.GetAttribute(".mark", b.AllowMissing("data-key"), &key)   // key == nil while the attribute is absent
```

Works for every decode target (`.Capture` and the getters' trailing pointer), and is re-evaluated on every observation, so a poll watching an absent → present transition stays honest.

`GetValue`/`GetInnerText`/`GetTextContent` have no "defined" axis (empty string is a valid value) — they poll on presence only and never need `AllowMissing`.

**The `Each*` matchers fail on zero matches** (`EachBeVisible`/`EachBeEnabled`/`EachHaveClass`/`EachHaveInnerText`/`EachHaveProperty`/…): "≥1 match AND all satisfy", so they wait for elements to appear instead of passing vacuously. To assert nothing matches, use `Eventually(sel).Should(b.HaveCount(0))` or `ShouldNot(b.Exist())` — **not** the no-arg `EachHaveInnerText()`/`EachHaveTextContent()`, which now assert the property is *defined* on every match (and capture the slice).

## 6. Network handlers in an `Ordered` container

Handlers (`StubRequest`/`AbortRequest`/`ModifyRequest`/`ModifyResponse`/`HoldResponse`) are first-match-wins in registration order, and `Prepare()` is what clears them. With `BeforeEach(func(){ b.Prepare() }, OncePerOrdered)`, `Prepare()` does **not** run between the `It`s — so handlers accumulate and a later identical handler is **silent dead code**.

**Detector: an interception gate that times out only when the spec runs with the rest of the suite, and passes when focused alone.** Check what ran before it in the same process before diagnosing anything else.

**Shadowing is silent in both directions, and the green direction is worse.** A leftover **stateful** handler — one keeping a counter — claims the response, and because its counter has moved on, passes it through untouched. The app behaves normally; the later spec's own counter stays 0; nothing hangs.

```go
b.ModifyResponse(url).Using(func(r biloba.InterceptedResponse) biloba.StubResponse {
    if atomic.AddInt32(&intercepted, 1) == 1 { /* mangle only the first */ }
    return passthrough
})
```

**Assert on your own interception state in every spec that installs a handler it depends on** — the app's behavior can't tell you. Every registration returns a handle with its own `Count()` (`stub.Count()`/`abort.Count()`/`mod.Count()`/`hold.Count()`) — reach for that before hand-rolling a counter; only a `Using()` callback needing logic beyond counting still needs one:

```go
Eventually(hold.Count).Should(Equal(1))
Eventually(func() int32 { return atomic.LoadInt32(&intercepted) }).Should(Equal(int32(1)))
```

On failure Biloba names both registration sites of a handler that never ran and was shadowed (`biloba:debug-failures`) — but that's a *failure* artifact, so the silently-green case still needs your own `Count` assertion.

Fixes: drive both orderings from a **single `It`** (usually reads better anyway), or give the holding spec its own **`b.NewTab()`** (handler lists are per-tab).

## 7. Assertions that cannot fail

These pass every time, forever, against a completely broken page — and are usually written *as the guard against exactly that breakage*. The tell: the subject can be empty or absent, and the form treats empty-or-absent as success. Ask "what would make this fail?"

**An unresolved locator scope matches nothing**, so every negative assertion on it is satisfied instantly:

```go
// WRONG — passes forever if #published-list never renders (the white screen this guard exists to catch)
Consistently(b.ByTextContains("Draft").Within("#published-list")).ShouldNot(b.Exist())

// RIGHT
Eventually("#published-list").Should(b.Exist())
Consistently(b.ByTextContains("Draft").Within("#published-list")).ShouldNot(b.Exist())
```

**`BeNetworkIdle` means "zero in flight right now"** — no quiet period — so it passes at t=0, before your click's fetch has left the page. Anchor on the request first:

```go
b.Click("#refresh")
Eventually(b).Should(b.HaveMadeRequest(ContainSubstring("/api/refresh")))
Eventually(b).Should(b.BeNetworkIdle())
```

**A `Current*ForEach` snapshot returns an empty slice**, which satisfies a negated collection assertion. These don't poll, so gate presence — the same gate keeps the negative honest:

```go
Eventually(".row").Should(b.HaveCount(3))
Ω(b.CurrentInnerTextForEach(".row")).ShouldNot(ContainElement("Draft"))
```

(Positive collection assertions are safer: Gomega's `HaveEach` errors on an empty slice, and the `Each*` matchers fail on zero matches.)

**Two vacuous negations Biloba now makes loud** — both were satisfied by an element that wasn't there:
- `ShouldNot(b.BeChecked())` on the wrapping `<label>`/`<div>` instead of the `<input>`: an element with no `checked` property is now an **error**.
- `ShouldNot(b.BeInViewport())` and the other geometry matchers on an element that never rendered: a missing element is now an **error** (the pairwise/offset probes name which selector went missing).

**That change costs the wait-for-teardown idiom.** Gomega counts an assertion satisfied only when the matcher gives the desired answer *and* doesn't error, in either direction:

```go
Eventually("#toast").ShouldNot(b.BeInViewport())   // WRONG — now times out
Eventually("#toast").ShouldNot(b.Exist())          // RIGHT
Eventually("#toast").Should(b.HaveCount(0))        // RIGHT
```

Same for `HaveBoundingBox`, `HaveScrollOffset`, `HaveOffsetTopWithin`/`HaveOffsetLeftWithin`, `HaveGapBetween`, the pairwise matchers, and any `Consistently(sel).ShouldNot(...)` on a conditionally-rendered `sel`. **`Exist`/`HaveCount` answer "is it there?"; geometry matchers answer "where is it?" and presuppose it's there** — same as `BeVisible`/`BeEnabled`/`BeClickable` always have.

**A visual baseline that was never reviewed asserts nothing.** If a missing baseline were written on first sight and the spec passed, the very first run would certify whatever the page happened to look like — a broken page included — and the assertion would be green from birth, so nobody would ever open the image. Biloba refuses: a missing baseline is a **failure** that writes the captured `.actual.png` and tells you to re-run with `BILOBA_UPDATE_SCREENSHOTS=1`.

Don't work around it. Pre-writing baselines blind — a scripted "run with update until it's green", or committing whatever the first run produced without looking — recreates exactly the vacuous assertion the refusal exists to prevent. Read the `.actual.png`, then update.

**`BILOBA_UPDATE_SCREENSHOTS` set in CI turns the entire visual suite green.** In update mode every visual assertion captures, writes its baseline, and passes — no comparison happens at all. It's an environment variable, so it gets added to a CI config once, during a legitimate baseline refresh, and then stays. Nothing fails, nothing looks wrong, and the suite reads as coverage while proving nothing. Grep the CI config for it before trusting a green visual run, and keep the variable to local, scoped invocations (`ginkgo --focus=…`).

**And a tolerance widened until nothing can fail is the same bug with pixels.** `b.Tolerance(0.05)` on a small element can absorb an entire component; `b.ChannelTolerance(60)` absorbs a color change. Tune the suite-wide default (`BilobaConfigScreenshotTolerance` / `BilobaConfigScreenshotChannelTolerance`) once against real evidence, and treat a per-assertion tolerance that keeps growing as a signal the subject is nondeterministic — mask the region (`b.Mask`) instead of blurring the whole comparison. → `biloba:visual-assertions`

**A capture used to be able to destroy its own subject.** Reaching content outside the viewport means expanding it, and a responsive page observes that — `matchMedia` flips, `resize` fires, and an app that re-renders on its breakpoint unmounts the subtree being captured, taking component-local state with it. The spec then fails on the line *after* the capture, polling for an element that was there a moment ago. Biloba now expands the viewport only when it has to (a subject outside the viewport, or a document bigger than the viewport) and says so when a subject vanishes across a capture: `the element matching X was present before this capture and gone after it`.

If you see that message, or an intermittent "could not find DOM element" on the line following a capture: **capture something already in view**. `b.ScrollIntoView(sel)`, gate on `b.BeInViewport(b.Fully())`, then capture — Biloba touches nothing.

**A blank capture is a stable capture.** An element capture works below the *document* fold — Biloba expands the main frame's viewport — but an element scrolled out of an **inner** `overflow: auto` pane was never painted, and comes back as a flat rectangle of the pane's background. Baseline that and it matches itself forever. This is the normal shape of any app-shell layout (fixed chrome, inner scrolling pane), so it is not exotic. Biloba now refuses the comparison and names the clipping ancestor; the fix is to scroll the pane first:

```go
b.ScrollIntoView(".figure", b.WithinScroller("#reader-pane"))
Eventually(".figure").Should(b.BeInViewport(b.Fully()))
Eventually(".figure").Should(b.HaveScreenshot("figure"))
```

Every structural gate in front of such a capture (`[data-rendered]` present, `svg` present) passes while the capture is blank — presence is not paintedness.

**Two colour schemes that render identically assert one thing, not two.** `b.InColorSchemes("light","dark")` drives `prefers-color-scheme`, and an app with a manual theme override only follows that media query while it is in its follow-the-system state. A spec that pinned the theme (directly, via a helper, or via a leftover stored preference) writes the same pixels to `home-light.png` and `home-dark.png`. Both look right, both pass, and the dark assertion cannot fail. Biloba warns when two schemes in one assertion capture byte-identical images — treat that warning as a finding, not noise.

Three more vacuous shapes live with their smell and are covered above: the **eagerly-created app-state barrier** (§3), the **inverted document-order assertion** (§4), the **shadowed stateful handler** (§6).

## 8. Two facts that prevent wrong diagnoses

**Gate on a DOM readiness anchor, not on the URL.** The URL flips the instant the router pushes it — the weakest available proof, and the one signal that races a live target swap.

```go
b.Click("#submit")
Eventually("#dashboard-root").Should(b.Exist())     // the destination actually rendered
Ω(b.GetLocation()).Should(HaveSuffix("/dashboard")) // …now a formality
```

`b.GetLocation()`/`b.GetTitle()` (breaking rename — bare `Location`/`Title` are gone) are polling getters honoring all four knobs, and they treat a transient `Inspected target navigated or closed (-32000)` as a retryable miss. So `Eventually(b.GetLocation)` is safe; it's just still the weaker assertion.

**Fast interactions act in place.** `b.Click`/`b.Tap` do **not** `scrollIntoView` and do **not** move focus. If a scroll position changes around a fast click, the cause is app-side. Scroll-into-view comes only from `b.Realistic()` and from focus-bearing ops (`b.Focus`, `b.SetValue`, `b.Type`) whose `.focus()` scrolls its target. If a spec asserts on scroll position, keep focus-bearing ops off the element under test.

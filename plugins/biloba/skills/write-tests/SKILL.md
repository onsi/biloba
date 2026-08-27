---
name: write-tests
description: Author good Biloba specs in your own Ginkgo/Gomega suite — the dual immediate/matcher API (act now vs. return a matcher you poll with Eventually), capturing a matcher's observed value with .Capture instead of asserting-then-re-reading, first-vs-all naming, the navigate-then-readiness-anchor shape (gate on the DOM, then read GetLocation), selecting elements (CSS targeting stable hooks as the default, semantic role/text/label locators, anchoring a locator scope so a negative assertion isn't vacuous, the >>> piercing combinator, XPath), the interaction vocabulary (click variants, drag, scroll, tap, text selection), realistic mode for occlusion/hover smoke tests, visual regression with b.HaveScreenshot against a committed baseline, hermetic tests via request stubbing/aborting/modifying/holding, the GetJSValue app-state barrier (and when it gates nothing), multi-tab flows, and seeding state. Use when writing or reviewing Biloba browser tests.
---

# Writing Biloba specs

Assumes the suite is wired (`biloba:setup`) and you know the principles (`biloba:overview`). Method list → `biloba:api`. XPath → `biloba:xpath`. Flakes → `biloba:flaky-specs`. Docs: <https://onsi.github.io/biloba/#working-with-the-dom>.

## RULE — get these two right in the first draft

1. **Selecting elements.** User-facing things → a **Locator** by role/name/text (`b.ByRole("button").WithName("Save")`, `b.ByText("Sign in")`), which doubles as an a11y guard. Structural/state hooks you own → **CSS on a stable `#id`/`[data-testid]`**, never a styling class.
2. **Assert observable outcomes** — visible text, counts, URL/title, network effects — not internal class/structure.

**Smells to catch in your own draft** (generic-automation muscle memory gets these wrong):

| Smell | Instead |
|---|---|
| `:nth-of-type`, `.btn-primary` | `b.ByCSS(sel).Nth(i)`, or a stable `#id`/`[data-testid]` |
| text-matching XPath | `b.ByText(...)` / `b.ByRole(...).WithName(...)` |
| `b.Run("...querySelectorAll(s).length")` | `b.HaveCount(...)` — reinventing a matcher |
| IIFE-wrapping a script to get `return` | `b.RunAsync` |
| `SetValue` when you meant keystrokes | `b.Type` |
| `b.Run(expr, &x)` then `Expect(x)` — single-shot read | `Eventually(b.Run).WithArguments(expr)` |
| matcher assertion, then a getter for the same value | `.Capture(&x)` on the matcher — one read |
| a guarded/conditional interaction ("click X only if…") | fix the spec's state determinism upstream → `biloba:flaky-specs` |

## The one pattern: dual, and poll-by-default

Most DOM methods have **two forms keyed on argument count**.

```go
// Fully applied → POLLS until ready, acts/reads exactly once, stops. The everyday form.
b.Click("#go")                     // polls until clickable, clicks once
b.SetValue("#name", "Jane")        // polls until settable, sets once
text := b.GetInnerText("#title")   // polls until present, reads once

// Under-applied → returns a matcher YOU poll. For a custom timeout, a Consistently, or .And().
Eventually("#go").Should(b.Click())
Eventually("#name").Should(b.SetValue("Jane"))
Eventually("#title").Should(b.HaveInnerText("Welcome"))
```

A fully-applied action dispatches **once** on the first success and stops — safe even on a **toggle**, no oscillation, no "fired a frame too early" race.

**Tune or opt out** with shallow `*Biloba` clones: `b.WithTimeout(5*time.Second).Click("#go")`, `b.WithPolling(d)`, `b.WithContext(ctx)`. `b.Immediate()` reverts to act-once/fail-fast — **you almost never want it**. Misapplying a knob (e.g. `WithPolling` on a snapshot, or any knob on the bare matcher form) is a hard error → four-bucket table in `biloba:api`.

**First-vs-all naming.** A bare method acts on the **first** match. Plural getters are **snapshots** (no poll — gate presence first); plural actions carry an **`Immediately` suffix** (act now, no poll; the long suffix is a deliberate "know what you're doing" smell). Matchers over all matches are `Each*` and **fail on zero matches**.

| first (polls) | all (snapshot / immediate) | matcher over all |
|---|---|---|
| `GetInnerText` | `CurrentInnerTextForEach` | `EachHaveInnerText` |
| `GetProperty` | `CurrentPropertyForEach` | `EachHaveProperty` |
| `Click` | `ClickEachImmediately` | — |

`EachHaveInnerText`/`EachHaveTextContent`'s sub-matcher and `.Capture` see a typed `[]string` (same slice their `Current*ForEach` getter returns) — `Equal([]string{...})` works. The generic `EachHaveProperty` hands over the raw JSON-decoded `[]any` (properties are heterogeneous) — `Equal([]string{...})` always fails there; reach for `HaveExactElements(...)`. Same shape as `EvaluateTo`'s raw-vs-typed asymmetry, below.

## The spec shape

```go
var _ = Describe("the search page", func() {
	BeforeEach(func() {
		b.Navigate("http://localhost:8080/search")
		Eventually("#results").Should(b.Exist()) // readiness anchor
	})

	It("finds matches", func() {
		b.SetValue("#q", "biloba")
		b.Click(b.ByRole("button").WithName("Search"))
		Eventually(".result").Should(b.HaveCount(BeNumerically(">", 0)))
	})
})
```

- `b.Navigate(url)` asserts the response was `200` (`NavigateWithStatus` for other codes).
- Pick a **stable, meaningful** anchor (a heading, a key container) with `b.Exist()`/`b.BeVisible()`.

**For an in-app navigation, gate on a DOM anchor and *then* read the location — don't poll the URL.** The URL flips the instant the router pushes it, so it's the weakest proof the destination rendered.

```go
b.Click("#submit")
Eventually("#dashboard-root").Should(b.Exist())     // proves the navigation landed…
Ω(b.GetLocation()).Should(HaveSuffix("/dashboard")) // …so this is a formality
```

`b.GetLocation()`/`b.GetTitle()` (**breaking rename** — bare `Location`/`Title` are gone) are polling getters honoring all four knobs, and a transient CDP error mid-navigation (`Inspected target navigated or closed`) is a retryable miss — so `Eventually(b.GetLocation)` is safe, just weaker.

## Pocket matcher cheat-sheet — reach here before `b.Run`

| Want to assert… | Matcher |
|---|---|
| present / visible | `b.Exist()` / `b.BeVisible()` |
| how many match | `b.HaveCount(BeNumerically(">", 0))`; distinct by key: `b.HaveDistinctCount("data-key", 3)` |
| visible text | `b.HaveInnerText("…")` / `b.HaveTextContent(…)` / `b.HaveText(…)` (whitespace-collapsed) |
| a DOM/JS property | `b.HaveProperty("href", …)` / `b.HaveClass("active")`; JSON attr `b.HaveJSONAttribute("data-state", HaveKeyWithValue(…))` (getter `b.GetJSONAttribute(sel, attr, &out)`) |
| actually clickable (visible+enabled+topmost) | `b.BeClickable()` |
| form value | `b.HaveValue(…)` |
| tab URL / title / spawned tab | `b.HaveURL(…)` / `b.HaveTitle(…)` / `b.HaveSpawnedTab(…)`; getters `b.GetLocation()`/`b.GetTitle()` |
| the app applied a server response | `b.GetJSValue("window.__storeLog", &log)` — app-state barrier (below); *not* the DOM, *not* a Go-side HTTP read |
| a network request was made | `Eventually(b).Should(b.HaveMadeRequest(…))` |
| layout / box / scroll | `b.HaveBoundingBox(HaveField("Top", …))` / `b.HaveOffsetTopWithin(container, …)` / `b.HaveScrollOffset(…)`. `Box.Width`/`Height` = border-box; `ClientWidth`/`ClientHeight` = scrollbar-excluded content box |
| A relative to B | `b.BeAbove` / `BeBelow` / `BeLeftOf` / `BeRightOf` / `b.Encloses` / `b.Overlaps`; numeric `b.GetGapBetween(a,b)` → `BoxDelta` |
| on screen / document order | `b.BeInViewport()` (partial; `b.BeInViewport(b.Fully())` = whole box) / `b.BePrecededBy` / `b.BeFollowedBy` |
| computed style | `b.HaveComputedStyle(prop, …)` / `b.HaveComputedStyleNumeric(prop, …)`; color across syntaxes `b.HaveComputedStyle(prop, b.MatchColor("var(--tok)"))`; getters `b.GetComputedStyle`/`GetComputedStyleNumeric`/`NormalizeColor` |
| scroll a target into view (instant) | `b.ScrollIntoView(sel)` — options `b.WithinScroller(container)`, `b.AtTopOffset(px)` |
| an arbitrary JS expression | `Eventually(expr).Should(b.EvaluateTo(matcher))` |
| it still *looks* like this | `b.HaveScreenshot("name")` against a committed baseline → `biloba:visual-assertions` |

- `EvaluateTo`/`Run` decode JS numbers to **float64** — `BeNumerically("==", n)`, not `Equal(intLiteral)`.
- **`BePrecededBy`/`BeFollowedBy` read subject-first**: `Eventually(X).Should(b.BeFollowedBy(Y))` ⇔ X comes **BEFORE** Y. A backwards one passes silently — also assert the inverse doesn't hold.
- **Geometry matchers error on a missing element**, so `Eventually(sel).ShouldNot(b.BeInViewport())` times out. Assert disappearance with `ShouldNot(b.Exist())` / `Should(b.HaveCount(0))`.

## `.Capture(&x)` — don't re-read what you just asserted on

```go
var blockID string
Eventually(".figure-frame").Should(b.HaveAttribute("data-block-id", Not(BeEmpty())).Capture(&blockID))
b.Click("#block-" + blockID)
```

Asserting and *then* calling the getter is two reads of a page that may have changed in between. `Capture` hands you the value from the winning read, decoded into your Go type (a JS number lands in an `*int` as `3`, not `float64(3)`).

- Writes **only on a match** — `ShouldNot` captures nothing; use a `Should` when you need the value.
- Returns a **new** matcher and leaves the receiver alone, so a matcher in a variable is reusable with different targets (`m.Capture(&idA)`, then `m.Capture(&idB)`).
- Narrowing a JS object into a struct with a subset of its fields is fine; capture Biloba's own structs (`Box`, `ScrollOffset`, `BoxDelta`, `Cookie`) into the matching type or an `any` — a different struct is rejected, not half-filled.
- A JS `null`/`undefined` decodes to the Go zero value, so a plain `*string` target can't tell "absent" from "present but empty." Decode into a `**T` instead — absent leaves it `nil`, present allocates. Same rule for the getters' trailing decode pointer. Matters most under `AllowMissing` (`biloba:api`), whose whole point is making absence a value rather than a timeout.
- Value-less matchers (`Exist`, `BeVisible`, `BeInViewport`, the relational ones) and actions-as-matchers deliberately don't compile with it. Full capturable list → `biloba:api`.

## Selecting elements — the vocabulary

A `selector` is a **CSS string** (fastest; `:has()`; `>>>` pierces shadow/iframe), a **`Locator`** (`b.By*`; most resilient, slowest — full-document ARIA scan), or an **`XPath`** (`biloba:xpath`; axis/ordinal; pierces neither).

```go
b.Click("#go")                                     // CSS by id — the preferred default
b.Click("[data-testid=save]")                      // CSS by intentional test-id
Eventually("tr:has(td.overdue)").Should(b.Exist()) // CSS :has() — "the row that contains X"
b.Click(b.ByRole("button").WithName("Save"))       // Locator — role + accessible name (a11y guard)
b.Click(b.ByText("Submit"))                        // Locator — visible text (ByTextContains for substring)
b.SetValue(b.ByLabel("Email"), "jane@acme.com")    // Locator — a control by its label
b.Click(b.XPath("li").WithText("OK").Ancestor("ul"))// XPath — an axis query nothing else expresses
```

Constructors (each text-valued one has a `*Contains` variant): `b.ByRole`, `b.ByText`, `b.ByLabel`, `b.ByPlaceholder`, `b.ByAltText`, `b.ByTitle`, `b.ByTestID` (attr = `biloba.TestIDAttribute`, default `data-testid`), `b.ByCSS(sel)` (raw CSS into the algebra — the structural escape hatch). Refine a role with `.WithName(n)`, `.Level(n)`, or ARIA states `.Checked()`/`.Disabled()`/`.Expanded()`/`.Pressed()`/`.Selected()`.

Locators **compose**, and every filter accepts **any** selector (CSS/XPath/Locator):

```go
b.ByRole("listitem").ContainingText("Product 2")        // .ContainingText / .NotContainingText
b.ByRole("listitem").Containing(b.ByText("Delete"))     // .Containing / .NotContaining (a descendant)
b.ByRole("button").And(".primary")                      // .And / .Or — intersection / union
b.ByRole("button").WithName("Delete").Within("#dialog") // .Within / .NotWithin
b.ByText("Item").Nth(2)                                 // .Nth(i) / .First() / .Last()
b.ByCSS(".story").Nth(1)                                // the 2nd .story
```

**Trap — an unresolved `.Within`/`.Containing` scope matches nothing, so a negative assertion is vacuous** — it passes instantly and forever if the scope never renders. Anchor the scope first:

```go
Eventually("#published-list").Should(b.Exist())                                       // the scope is real…
Consistently(b.ByTextContains("Draft").Within("#published-list")).ShouldNot(b.Exist()) // …so this bites
```

Locators **pierce open shadow roots automatically**; CSS needs `>>>` (one boundary each, open shadow / same-origin iframe only); XPath crosses neither.

```go
b.Click("my-widget >>> button.submit")
Eventually("#editor-frame >>> .toolbar .save").Should(b.Click())
```

Never fetch-then-act — pass the selector *into* the action so find-and-act is one atomic JS snippet.

## The interaction vocabulary

All dual unless noted; all work on both the fast and realistic tracks.

```go
b.DblClick(sel); b.RightClick(sel); b.MiddleClick(sel)
b.Click(sel, b.At(x, y))                          // click at a top-left-origin offset; canvas/map/slider
b.Click(sel, b.Shift(), b.Meta())                 // modifiers held; composes with b.At(...)
b.DragTo(source, target)                          // pointer-based drag — Eventually(src).Should(b.DragTo(tgt))
b.ScrollWheel(sel, deltaX, deltaY)                // +Y down, +X right
b.Tap(sel)                                        // touch tap; takes b.At(...), ignores modifiers
b.Focus(sel); b.Blur(sel); b.Hover(sel)           // Hover fires pointer events, NOT CSS :hover
b.ScrollIntoView(sel, b.AtTopOffset(96))          // also b.WithinScroller(container)
b.Type(sel, "abc"); b.Type(sel, biloba.Keys.Enter)// real keystrokes — SetValue does NOT type
b.Type("textarea", biloba.Keys.Enter, b.Shift())  // Shift-Enter — modifiers work on the keyboard too
```

**`b.Type` is THE element-targeted keyboard method** — text, named `biloba.Keys.*`, and held modifiers. `b.Type(sel, payload...)` polls; `b.Type(payload)` returns a matcher (a lone string, or one-or-more `Keys.*`). Exact disambiguation → `biloba:api`.

`b.At(x,y)`/`b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` (⌘/Win) are **pointer options** on `Click`/`DblClick`/`RightClick`/`MiddleClick`/`Tap` — after the selector, or in place of it in the matcher form (`Eventually(sel).Should(b.Click(b.At(x,y), b.Shift()))`). The modifiers double as keyboard modifiers on `b.Type`/`b.SendKeysToWindowImmediately`.

**Two interactions don't poll — gate them by hand:** `b.SendKeysToWindowImmediately(...)` (focus-free global keystrokes — routes to the focused element, else `document`/window) and the `*Immediately` plural verbs.

```go
Eventually("input.search").Should(b.BeFocused()); b.SendKeysToWindowImmediately(biloba.Keys.Enter)
```

To send keys *into* an element use `b.Type(sel, ...)` — it focuses and polls. Reserve `SendKeysToWindowImmediately` for genuine global hotkeys.

**Fast interactions act in place** — `element.click()` after a visibility check, no `scrollIntoView`, no focus move — so a fast click never shifts the page out from under a scroll/layout assertion. Scroll-into-view comes only from `b.Realistic()` and from focus-bearing ops (`b.Focus`/`b.SetValue`/`b.Type`, because `.focus()` scrolls). If a scroll position moves around a fast click, the cause is app-side.

**`b.SetValue` and frameworks.** It writes through the input's native value setter, so it drives **controlled** React/Vue/Solid inputs (`onChange` fires, state updates) — no need to make an input uncontrolled. It does **not** blur, so an `onBlur` commit / inline-edit-unmount handler won't fire; pair with `b.Blur(sel)`.

**`<select>`.** `b.SetValue(sel, v)` matches the option's underlying **`value`**, not its visible label (`input`+`change` fire with `bubbles:true`, so React `onChange` runs — no realistic mode needed). Pick by label with `b.SetValue("#model", b.ValueLabel("Sonnet"))`. Assert labels via `option.textContent`, the selection via `b.HaveProperty("value", id)`.

### Selecting text (highlight / annotation / editor UIs)

Each produces a genuine `window.getSelection()` range and dispatches `mouseup`, so selection-driven toolbars fire.

```go
b.SelectText("#passage")                          // all of the element's text (dual, polls)
b.SelectText("#passage", "fox")                   // the first "fox"
b.SelectText("#passage", "fox", b.Occurrence(2))  // the 2nd "fox" (1-based)
b.SelectRange("#passage", 4, 9)                   // chars [4,9) across text nodes; fails if out of bounds
b.ClearSelection()

Eventually("#passage").Should(b.SelectText())                       // matcher forms
Eventually("#passage").Should(b.SelectText("fox", b.Occurrence(2))) // REQUIRES an explicit Occurrence
Eventually("#passage").Should(b.SelectRange(4, 9))
```

Read back what's selected: `Eventually("window.getSelection().toString()").Should(b.EvaluateTo("quick"))`.

## Asserting appearance → `biloba:visual-assertions`

When the contract really is "it still looks like this" (a chart, a themed rail, a print layout), `b.HaveScreenshot(name)` compares the subject against a **committed** baseline PNG. Always under `Eventually` — every attempt re-captures and re-compares, so the poll waits out font loading, a `ResizeObserver`, and rAF settling.

```go
Eventually(".chart").WithTimeout(10*time.Second).Should(b.HaveScreenshot("revenue-chart"))
Eventually(b).Should(b.HaveScreenshot("home", b.Mask(".relative-timestamp"), b.InColorSchemes("light", "dark")))
Eventually(b.ViewportOnly()).Should(b.HaveScreenshot("home-fold"))   // just what's on screen, not the whole document
```

The **first run fails** — Biloba never writes a missing baseline and passes. Look at the `.actual.png` it wrote, then create the baseline with `BILOBA_UPDATE_SCREENSHOTS=1 ginkgo -r -p`. Baselines go in `./biloba-baselines` (commit them); the `.actual.png`/`.diff.png` a failure writes are gitignored artifacts. Options: `b.Mask`, `b.Tolerance`, `b.ChannelTolerance`, `b.Animated`, `b.InColorSchemes`. Animations, transitions, and the caret are frozen for you (inside open shadow roots too), and an update run settles the page before writing; scrollbars, cross-platform font rendering, and closed shadow roots are not covered — details in `biloba:visual-assertions`.

Keep it a wide net used narrowly: it fails on every change, intended or not. Text, counts, and state still belong to the ordinary matchers.

## Realistic mode — a handful of smoke tests → `biloba:realistic-mode`

`b.Realistic()` returns a `*Biloba` view of the **same tab** routed through real Chrome DevTools Protocol input: it scrolls into view, waits for stability, **refuses to click through an occluding overlay**, moves the **real pointer** (CSS `:hover` activates). Opt-in — it costs round-trips and can reintroduce timing flake, so quarantine it to smoke tests guarding a drag, an overlay, a `:hover` menu. No per-call decorator.

```go
b.Realistic().Click("#submit")                                   // inline
rb := b.Realistic(); rb.Hover(".menu"); rb.Click(".menu .item")  // per-spec

var _ = Describe("checkout (realistic)", Label("realistic"), func() {   // per-suite
    var rb *biloba.Biloba
    BeforeEach(func() { rb = b.Realistic() })
})
```

With the label, `ginkgo --label-filter='realistic'` runs only that lane; `'!realistic'` keeps it out of the fast inner loop. To merely *assert* an element isn't occluded, use `b.BeClickable()` — deterministic, no realistic round-trips.

## Prefer real backends; stub the network when you must

Favor real backends and fix flakes/perf there. When you must stub, stub only the endpoints you don't want to depend on — everything unmatched passes through.

```go
b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{
	Body:    `[{"name":"Jane"},{"name":"Bob"}]`,
	Headers: map[string]string{"Content-Type": "application/json"},
})
b.Navigate("/app")
Eventually(".user").Should(b.HaveCount(2))
```

Stubs are per-tab and reset by `Prepare()`. Also `b.AbortRequest(url)`, `b.ModifyRequest(url).WithURL/.WithMethod/.WithHeader/.WithBody(...)`, and `b.ModifyResponse(url).WithStatus/.WithHeader/.WithBody/.Using(func(biloba.InterceptedResponse) biloba.StubResponse)` — the `InterceptedResponse` carries `URL` as well as `Status`/`Headers`/`Body`, which is how a transform registered with a *matcher* tells the responses apart. All share one **first-match-wins** handler list. While interception is on Biloba disables the HTTP cache (a cached response raises no interception event), restoring it in `Prepare()`.

Every registration returns a handle — `stub.Count()`/`abort.Count()`/`mod.Count()` — reporting how many dispatches *that handler* claimed. **Assert it fired:** `Eventually(stub.Count).Should(Equal(1))`. A typo'd URL otherwise matches nothing, passes through to the real network, and the spec passes for the wrong reason.

Three traps, detailed in `biloba:flaky-specs`:

- **Observe in order:** `Eventually(b).Should(b.HaveMadeRequest(...))` **first**, *then* `Eventually(b).Should(b.BeNetworkIdle())`. Idle means "nothing in flight right now", so alone it passes at t=0.
- **Handlers accumulate in an `Ordered` container** (`Prepare()` is `OncePerOrdered`), so an earlier `It`'s handler silently shadows a later identical one — sometimes a timeout, sometimes a spec that just passes.
- **Assert your own interception state** (`Eventually(hold.Count).Should(Equal(1))`) so you'd notice either.

**`b.HoldResponse(url)` holds a real response hostage** so you can force an arrival order — the honest way to test optimistic-UI reconciliation, where the bug only shows when a stale response lands *after* the next action.

```go
hold := b.HoldResponse(ContainSubstring("/api/settings"))
b.Click("#refresh")            // fires the request…
hold.Await()                   // …blocks until its response is actually being held
b.Click("#rename")             // drive the app into the racy window
hold.Release()                 // now let the stale response land
Ω(hold.Count()).Should(Equal(1))
```

**By default a hold freezes *every* matching response**, and a bare `Release()` frees them all and disarms the hold. `.Limit(n)` caps how many are held at once — overflow matches fly straight past, which is how you express "hold save #1 while save #2 lands". `Await()` returns the **oldest response still held**; `hold.Release(r)` releases just that one and `hold.ReleaseNext()` releases the oldest, both keeping the hold **armed** (`ReleaseNext` fails loudly if nothing is held). `Await` has its own 30s deadline (`b.WithTimeout(d).HoldResponse(url)`); holds are force-released at spec end and by `Prepare()`. Matching is **tab-wide and URL-based**, so a hold can catch a response from an earlier page load — scope the flow to a `b.NewTab()` when that matters. Full semantics → `biloba:api`; worked orderings → `biloba:flaky-specs`.

`hold.Held()`/`hold.PassedThrough()` split `Count()` into what the hold actually froze vs. what arrived and flew past (at `Limit`, or after a bare `Release()`) — with `.Limit(1)`, assert `Eventually(hold.PassedThrough).Should(Equal(1))` directly rather than inferring "not held" from `Count()` and the limit. And `Count`/`Release` are facts about the network, not the page — pair them with an app-state barrier (`b.GetJSValue`, or the DOM the response produces) when the assertion is about what the app *did* with the response.

## Seed state to skip slow flows

Navigate to a real origin first — `about:blank` can't hold cookies/storage.

```go
b.Navigate("/home")
b.SetCookie(biloba.Cookie{Name: "user", Value: "Joe"})
DeferCleanup(b.ClearCookies)
```

Or shortcut through your app's JS API:

```go
b.Run(`app.load(` + jsonFixture + `); app.redraw()`)
Eventually("#doc-name").Should(b.HaveInnerText("My Fixture Data"))
```

**`b.Run` returns the decoded value directly** — `n := b.Run("app.users.length")` feeds Gomega with no wrapper, `b.Run("expr", &typed)` decodes into a pointer, and there is no need for `runInt`/`runStr` helpers. It's a synchronous *expression*, so a top-level `return` is illegal — use `b.RunAsync` for `fetch`/`await`. Numbers decode to `float64`.

**Poll a `b.Run` read instead of snapshotting it.** It's a plain `func(string, ...any) any`, so it drops straight into `Eventually` — the antidote to the most common flake:

```go
Eventually(b.Run).WithArguments(`isReady()`).Should(BeTrue())
Eventually(b.Run).WithArguments(`document.querySelectorAll(".card").length`).Should(BeNumerically("==", 3))
```

For an interpolated/multi-line expr, pre-build the string (`expr := fmt.Sprintf(...)`) or poll a closure returning the decoded value.

**Don't hand-roll `getBoundingClientRect`/`scrollTop` through `b.Run`** — the geometry getters (`b.GetBoundingBox`/`GetScrollOffset`/`GetOffsetTopWithin` + `Have*` matchers) wait for layout before reading; relational layout has the pairwise matchers and `b.GetGapBetween`; on-screen-ness `b.BeInViewport()`; document order `b.BePrecededBy`/`BeFollowedBy`; computed style `b.GetComputedStyle`. → `biloba:api`

## The app-state barrier: `b.GetJSValue`

`b.GetJSValue(expr[, &ptr])` polls `expr` until it is *defined* and returns it, retrying through both `undefined` and a thrown error (a `ReferenceError` for a not-yet-created global is a not-ready condition); `null` is a legitimate value and returns immediately. Point it at a path the app writes — the only signal that proves the **browser** processed a response (the DOM may be the optimistic copy; a Go-side HTTP read bypasses the tab entirely).

```go
b.Run(`app.store.on("update", () => { window.__storeLog ??= []; window.__storeLog.push(app.store.state) })`)
b.Click("#save")
var log []string
b.GetJSValue("window.__storeLog", &log)   // blocks until the update actually lands
Ω(log).Should(HaveExactElements("saving", "saved"))
```

**The `??=` is what makes this a barrier.** `GetJSValue` gates on *definedness* and nothing else, so this works only because the subscriber creates the log **lazily** — "defined" and "an update landed" are the same event. Against a log the app creates **eagerly** (`this.__log = []` at construction) it is defined from page load, returns `[]` on the first tick, gates nothing, and never flakes: permanently vacuous.

**For an eagerly-created path, poll the *predicate*** — one read, one poll, typed result:

```go
var log []FoldEntry
Eventually(`window.__storeLog`).Should(b.EvaluateTo(ContainElement(HaveKeyWithValue("state", "saved"))).Capture(&log))
```

Asymmetry: `EvaluateTo` hands its sub-matcher the **raw JSON-decoded** value (`[]any` of `map[string]any` — `HaveKeyWithValue`, not `HaveField`); `Capture` gives you the typed `[]FoldEntry`.

**`GetJSValue` is wrong wherever *absence* is meaningful** — a ledger absent on `about:blank` means *quiet*; `window.__renderErrors` absent means *no errors*; a flag planted before a JS-only tab switch must have *survived* (waiting inverts the test); a pre-action baseline count. Those stay `b.Run` with a coalesce (`b.Run("window.__ledger ?? null")`) — correct, not a smell. → `biloba:flaky-specs` §3

## Multi-tab flows

```go
tab := b.NewTab()                // isolated, incognito-like context; closed by Prepare()
login(b, "sally"); login(tab, "jane")
Eventually(userXPath.WithText("Jane")).Should(b.HaveClass("online"))
```

Tabs opened *by the page* (`target="_blank"`) are **spawned tabs**:

```go
tab.Click(linkXPath)
Eventually(tab).Should(tab.HaveSpawnedTab().WithURL("https://youtube.com/..."))
yt := tab.AllSpawnedTabs().Find(tab.TabMatching().WithURL("https://youtube.com/..."))
```

A DOM method always operates on the tab it's invoked on (`tab.Click`, not `b.Click`). Dialogs and downloads are per-tab too — register dialog handlers **before** the action that triggers them.

## When Biloba can't express it

Realism (occlusion, scroll-into-view, real CSS `:hover`) → `b.Realistic()`. Real keystrokes → `b.Type`, not `SetValue`. Everything else — cross-origin frames, geolocation, any CDP feature without a wrapper — drop to chromedp via `b.Context` (`biloba:overview`). Propose an issue if a common pattern is missing.

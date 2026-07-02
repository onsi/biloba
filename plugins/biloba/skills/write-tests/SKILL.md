---
name: write-tests
description: Author good Biloba specs in your own Ginkgo/Gomega suite — the dual immediate/matcher API (act now vs. return a matcher you poll with Eventually), first-vs-all naming, the navigate-then-readiness-anchor shape, selecting elements (CSS targeting stable hooks as the default, semantic role/text/label locators, the >>> piercing combinator, XPath), the interaction vocabulary (click variants, drag, scroll, tap), realistic mode for occlusion/hover smoke tests, hermetic tests via request stubbing/aborting/modifying, multi-tab flows, and seeding state. Use when writing or reviewing Biloba browser tests.
---

# Writing Biloba specs

Assumes Biloba is already wired into the suite (`biloba:setup`) and you know the principles (`biloba:overview`). For the full method list see `biloba:api`; for XPath see `biloba:xpath`. Docs: <https://onsi.github.io/biloba/#working-with-the-dom>.

## RULE — two decisions to get right on the first draft

1. **Selecting elements.** Interactions and user-facing things → a **Locator** by role/name/text (`b.ByRole("button").WithName("Save")`, `b.ByText("Sign in")`), which doubles as an a11y guard. Structural/state hooks you own → **CSS on a stable `#id`/`[data-testid]`**, never a styling class.
2. **Assert observable outcomes** — visible text, counts, URL/title, network effects — not internal class/structure.

**Smells** to catch in your own draft (the wrong-from-generic-automation-muscle-memory moves): positional/styling-class CSS (`:nth-of-type`, `.btn-primary` — to address "the Nth element matching a CSS selector," start from `b.ByCSS(sel).Nth(i)` instead); text-matching XPath where `b.ByText`/`b.ByRole().WithName` fits; reinventing a matcher with `b.Run` (`querySelectorAll(...).length` instead of `b.HaveCount`); IIFE-wrapping a script for `return` (use `b.RunAsync`); `SetValue` when you meant keystrokes (use `b.Type`); a **single-shot read** — `b.Run(expr, &x)` immediately followed by `Expect(x)` — which races any async settle (poll it: `Eventually(b.Run).WithArguments(expr)`; see flaky-specs below).

## The one pattern to internalize: dual, and poll-by-default

Most DOM methods have **two forms keyed on argument count** — and the key thing to internalize is that **the fully-applied form polls until it succeeds**:

- **Fully-applied → polls until it succeeds, then acts/reads once.** This is the everyday form. It waits for the element to be ready, does the thing exactly once, then stops — no flake-prone single-shot race.
  ```go
  b.Click("#go")                  // polls until clickable, clicks once, stops
  b.SetValue("#name", "Jane")     // polls until settable, sets once
  text := b.GetInnerText("#title")// polls until present, reads once
  ```
- **Under-applied → returns a Gomega matcher that *you* poll.** Reach for this when you want to drive the `Eventually`/`Consistently` yourself (a custom timeout, a `Consistently`, composing with `.And()`).
  ```go
  Eventually("#go").Should(b.Click())            // you own the poll
  Eventually("#name").Should(b.SetValue("Jane"))
  Eventually("#title").Should(b.HaveInnerText("Welcome"))
  ```

**Poll-by-default is the whole point.** A fully-applied `b.Click(sel)` **polls until the element exists and is clickable (visible + enabled), dispatches exactly one atomic click on the first success, then succeeds and stops.** It does *not* re-act on later polls, so it is **safe even on a toggle** — there is no oscillation, because the successful action ends the poll. This is what makes Biloba flake-resistant: there is no "fired a frame too early" race to design around. Write `b.Click("#go")` and move on. (The *different* case — "click only **if** it's in state X," e.g. ensure a maybe-collapsed card ends open — is `b.ClickWhen(sel, guardSel)`, **not** a hand-rolled check-then-click loop, which *does* oscillate; see flaky-specs.)

**Tune the poll, or opt out, with `*Biloba` clones** (shallow, à la `Realistic()`): `b.WithTimeout(5*time.Second).Click("#go")`, `b.WithPolling(d)`, `b.WithContext(ctx)`. `b.Immediate().Click("#go")` opts into the old act-once/fail-fast behavior — **you almost never want it**; it reintroduces the classic race. Misapplying config (e.g. `WithPolling` on a snapshot, or any config on the bare matcher form) is a hard error — see `biloba:api` for the four-bucket rule.

**First-vs-all naming.** A bare method acts on/polls for the **first** match; the plural sibling works on **all** current matches. The plural getters are **snapshots** (`Current*ForEach` — no poll, gate presence first) and the plural actions carry an **`Immediately` suffix** (`ClickEachImmediately`, `SetPropertyForEachImmediately` — act now, no poll): `GetInnerText` vs `CurrentInnerTextForEach`/`EachHaveInnerText`; `GetProperty` vs `CurrentPropertyForEach`/`EachHaveProperty`; `Click` vs `ClickEachImmediately`. The name tells you which — and the long `Immediately` suffix is an intentional "know what you're doing" smell.

## The spec shape

Navigate, gate on a **readiness anchor**, then exercise behavior:

```go
var _ = Describe("the search page", func() {
	BeforeEach(func() {
		b.Navigate("http://localhost:8080/search")
		Eventually("#results").Should(b.Exist()) // page is ready once this appears
	})

	It("finds matches", func() {
		b.SetValue("#q", "biloba")                              // polls until settable, sets once
		b.Click(b.ByRole("button").WithName("Search"))          // polls until clickable, clicks once
		Eventually(".result").Should(b.HaveCount(BeNumerically(">", 0))) // assert the outcome
	})
})
```

- `b.Navigate(url)` also asserts the response was `200` (use `NavigateWithStatus` for other codes).
- Pick a **stable, meaningful** anchor (a heading, a key container) — `b.Exist()` or `b.BeVisible()`.
- **Assert on observable outcomes**, not implementation: visible text (`HaveInnerText`/`HaveText`), counts (`HaveCount`), URL/title (`HaveURL`/`HaveTitle`), or network effects (`HaveMadeRequest`).

**Pocket matcher cheat-sheet** — reach here before `b.Run`:

| Want to assert… | Matcher |
|---|---|
| element is present / visible | `b.Exist()` / `b.BeVisible()` |
| how many match | `b.HaveCount(BeNumerically(">", 0))` (distinct by a key: `b.HaveDistinctCount("data-key", 3)`) |
| visible text | `b.HaveInnerText("…")` / `b.HaveText(…)` (textContent) |
| a DOM/JS property | `b.HaveProperty("href", …)` / `b.HaveClass("active")` (JSON-valued attr: `b.HaveJSONAttribute("data-state", HaveKeyWithValue(…))` / getter `b.GetJSONAttribute(sel, attr, &out)`) |
| it's actually clickable (visible+enabled+topmost) | `b.BeClickable()` |
| form value | `b.HaveValue(…)` (also `b.HaveSpawnedTab`, `b.HaveURL`, `b.HaveTitle`) |
| a network request was made | `Eventually(b).Should(b.HaveMadeRequest(…))` |
| layout / box / scroll position | `b.HaveBoundingBox(HaveField("Top", …))` / `b.HaveOffsetTopWithin(container, …)` / `b.HaveScrollOffset(…)` (getters: `b.GetBoundingBox`/`b.GetScrollOffset`/`b.GetOffsetTopWithin`). `Box.Width`/`Height` = border-box; `Box.ClientWidth`/`ClientHeight` = scrollbar-excluded content box |
| element A positioned relative to B | `b.BeAbove(other)` / `BeBelow` / `BeLeftOf` / `BeRightOf` / `b.Encloses(other)` / `b.Overlaps(other)` (numeric: `b.GetGapBetween(a, b)` → `BoxDelta`) |
| on screen after a scroll / document order | `b.BeInViewport()` (partial; `b.BeInViewport(b.Fully())` = whole box on screen) / `b.BePrecededBy(other)` / `b.BeFollowedBy(other)` — read subject first: `Eventually(X).Should(b.BeFollowedBy(Y))` ⇔ X precedes Y |
| resolved computed style value | `b.GetComputedStyle(selector, prop)` (getter; resolves custom properties) / `b.HaveComputedStyle(prop, …)` (matcher); numeric: `b.GetComputedStyleNumeric` / `b.HaveComputedStyleNumeric`; color across syntaxes: `b.HaveComputedStyle(prop, b.Color("var(--tok)"))` / `b.GetResolvedColor(x)` |
| scroll a target into view (instant) | `b.ScrollIntoView(sel)` — options `b.WithinScroller(container)`, `b.AtTopOffset(px)` |
| click only if in a given state (toggle) | `b.ClickWhen(sel, guardSel)` — clicks once while `guardSel` matches, no double-toggle |
| an arbitrary JS expression | `Eventually(expr).Should(b.EvaluateTo(matcher))` |

`EvaluateTo`/`Run` JSON-decode numbers to **float64** — assert with `BeNumerically("==", n)`, not `Equal(intLiteral)`.

## Selecting elements — the vocabulary

The *decision* is the RULE above; here are the mechanics. A `selector` is a **CSS string** (fastest; `:has()`; pierces shadow/iframe via `>>>`), a **semantic `Locator`** (`b.By*`; most resilient, slowest — full-document ARIA scan), or an **`XPath`** value (`biloba:xpath`; axis/ordinal/`text()` queries; pierces neither boundary).

```go
b.Click("#go")                                    // CSS by id — stable hook (preferred default)
b.Click("[data-testid=save]")                     // CSS by intentional test-id
Eventually("tr:has(td.overdue)").Should(b.Exist())// CSS :has() — "the row that contains X"
b.Click(b.ByRole("button").WithName("Save"))      // Locator — role + accessible name (a11y guard)
b.Click(b.ByText("Submit"))                       // Locator — visible text (b.ByTextContains for substring)
b.SetValue(b.ByLabel("Email"), "jane@acme.com")   // Locator — a form control by its label
b.Click(b.XPath("li").WithText("OK").Ancestor("ul"))// XPath — axis query no CSS/locator expresses
```

**Locator constructors** (each text-valued one has a `*Contains` variant): `b.ByRole`, `b.ByText`, `b.ByLabel`, `b.ByPlaceholder`, `b.ByAltText`, `b.ByTitle`, `b.ByTestID` (attr = `biloba.TestIDAttribute`, default `data-testid`), and `b.ByCSS(sel)` — raw CSS into the algebra (the structural escape hatch: ordinally/filter-address a CSS selector, e.g. `b.ByCSS(".story").Nth(1)`). Refine a role with `.WithName(n)`, `.Level(n)` (heading), or ARIA states `.Checked()`/`.Disabled()`/`.Expanded()`/`.Pressed()`/`.Selected()`.

Locators **compose** — and the filters/combinators accept **any** selector (CSS/XPath/Locator), so pathways mix:

```go
b.ByRole("listitem").ContainingText("Product 2")             // .ContainingText / .NotContainingText
b.ByRole("listitem").Containing(b.ByText("Delete"))          // .Containing / .NotContaining (a descendant)
b.ByRole("button").And(".primary")                           // .And / .Or — set intersection / union
b.ByRole("button").WithName("Delete").Within("#dialog")      // .Within(scope)
b.ByText("Item").Nth(2)                                      // .Nth(i)/.First()/.Last() — ordinal
b.ByCSS(".story").Nth(1)                                     // raw CSS into the algebra (the 2nd .story)
```

Locators **pierce open shadow roots automatically** (no `>>>`); CSS needs the `>>>` combinator (one boundary each, open shadow / same-origin iframe only); XPath crosses neither.

```go
b.Click("my-widget >>> button.submit")
Eventually("#editor-frame >>> .toolbar .save").Should(b.Click())
```

Never fetch-then-act — always pass the selector *into* the action so find-and-act is one atomic JS snippet.

## The interaction vocabulary

`b.Click` is the everyday verb (dual: `b.Click(sel)` polls-then-acts; `Eventually(sel).Should(b.Click())` hands you the poll). The fuller set — all dual unless noted, all working on both the fast and realistic tracks:

```go
b.DblClick(sel); b.RightClick(sel); b.MiddleClick(sel)       // dual
b.Click(sel, b.At(x, y))                                     // click at top-left-origin offset; canvas/map/slider
b.Click(sel, b.Shift(), b.Meta())                            // modifiers held; composes with b.At(...)
b.DragTo(source, target)                                     // pointer-based drag; dual — Eventually(src).Should(b.DragTo(tgt))
b.ScrollWheel(sel, deltaX, deltaY)                           // wheel/scroll, +Y down +X right; dual — Eventually(sel).Should(b.ScrollWheel(dx, dy))
b.Tap(sel)                                                   // touch tap (dual); takes b.At(...), ignores modifiers
b.Type(sel, "abc"); b.Type(sel, biloba.Keys.Enter)          // real keystrokes — text and named Keys (SetValue does NOT type)
b.Type("textarea", biloba.Keys.Enter, b.Shift())            // Shift-Enter — modifiers work on the keyboard too
```

**`b.Type` is THE element-targeted keyboard method** — text, named `biloba.Keys.*`, and held modifiers, all in one. It's dual and **polls** like every other action: `b.Type(sel, payload...)` is immediate (selector + ≥1 payload arg), `b.Type(payload)` returns a matcher (a lone string, or one-or-more `Keys.*`). See `biloba:api` for the exact arg disambiguation.

**Two interactions don't poll — gate them by hand.** `b.SendKeysToWindowImmediately(...)` (focus-free global keystrokes — routes to the focused element, else `document`/window; no selector) and the `*Immediately` plural verbs (`ClickEachImmediately`, `SetPropertyForEachImmediately`) act now with nothing to fold readiness into. Put an explicit gate on the line above — for `SendKeysToWindowImmediately` that means proving focus:

```go
Eventually("input.search").Should(b.BeFocused());  b.SendKeysToWindowImmediately(biloba.Keys.Enter)
```

(To send keys *into* a specific element, prefer `b.Type(sel, ...)` — it focuses first and polls. Reserve `SendKeysToWindowImmediately` for genuine global hotkeys / already-focused targets.)

`b.At(x,y)` / `b.Shift()` / `b.Ctrl()` / `b.Alt()` / `b.Meta()` (⌘/Win) are **pointer options** accepted by `Click`/`DblClick`/`RightClick`/`MiddleClick`/`Tap` — after the selector (immediate) or in place of it (matcher: `Eventually(sel).Should(b.Click(b.At(x,y), b.Shift()))`). In fast mode any option switches a click off native `el.click()` to a synthetic event carrying the coords/flags. The **modifiers double as keyboard modifiers**: pass `b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` to `b.Type`/`b.SendKeysToWindowImmediately` (in any position) for Shift-Enter, ⌘-A, etc.

**Fast interactions act in place — no scroll, no focus move.** A fast `b.Click`/`b.Tap` is `element.click()` after a visibility check; it does **not** `scrollIntoView` and does **not** move focus, so it never shifts the page out from under a scroll/layout assertion. Scroll-into-view comes only from `b.Realistic()` (deliberately) and from **focus-bearing ops** — `b.Focus`/`b.SetValue`/`b.Type` — because the browser's `.focus()` scrolls its target into view. If a scroll position moves around a fast click, the cause is app-side, not Biloba.

**`b.SetValue` and frameworks.** `SetValue` writes through the input's native value setter, so it drives **controlled** React/Vue/Solid inputs (whose `value` is bound to state) — `onChange` fires and state updates; no need to make an input uncontrolled for Biloba's sake. For text inputs it focuses + dispatches `input`/`change` but does **not** blur — an `onBlur` commit/inline-edit-unmount handler won't fire from `SetValue`; pair with `b.Blur(sel)` when you want it (`b.SetValue("#name","New"); b.Blur("#name")`).

**`<select>` form values.** `b.SetValue(sel, v)` matches `v` against the option's underlying **`value`**, not its visible label: `b.SetValue("#model", "claude-sonnet-4-6")` (and `b.SetValue` on a native `<select>` already fires `input`+`change` with `bubbles:true`, so React `onChange` runs — no realistic mode needed). To pick by the label the user sees, wrap it: `b.SetValue("#model", b.ValueLabel("Sonnet"))`. Assert labels via `option.textContent` and the selection via the `<select>`'s `value` (`b.HaveProperty("value", id)`).

### Selecting text (highlight / annotation / editor UIs)

For "highlight text → floating menu → Define"-style interactions, use the first-class selection primitives — no `Range`/`getSelection` archaeology needed. Each produces a genuine `window.getSelection()` range and dispatches a `mouseup` so selection-driven toolbars fire:

```go
b.SelectText("#passage")                          // select all of the element's text (dual)
Eventually("#passage").Should(b.SelectText())     // matcher form
b.SelectRange("#passage", 4, 9)                   // select chars 4..8 across text nodes (dual)
Eventually("#passage").Should(b.SelectRange(4, 9))
b.ClearSelection()                                // drop the selection
```

Assert on what's selected by reading it back: `Eventually("window.getSelection().toString()").Should(b.EvaluateTo("quick"))`.

**`b.SelectText` polls** like every dual action — `b.SelectText(".blocks p")` waits until a matching element exists before selecting, so it's safe against content that appears asynchronously or nondeterministically (e.g. streamed/agent output). Use `b.WithTimeout(d)` if the default wait is too short, or the matcher form inside `Eventually` when you want to drive the poll yourself.

## Realistic mode — for a handful of smoke tests

By default every interaction is a fast, atomic **simulation** (`element.click()` after synchronous visibility/enabled checks — no scroll, no occlusion test, no real `:hover`; see `biloba:overview` principle 2). That's what you want for the overwhelming bulk of specs.

`b.Realistic()` returns a `*Biloba` view of the **same tab** whose interactions run through **real Chrome DevTools Protocol input** instead. A realistic click scrolls the element into view, waits for it to stop moving, **refuses to click through an occluding overlay**, moves the **real pointer** (so hover-gated clicks fire and CSS `:hover` activates), and dispatches a genuine mouse/touch/key event. The whole interaction vocabulary above works on both tracks.

It's **opt-in** because it costs real round-trips and can reintroduce timing flake — quarantine it to a handful of smoke tests that guard the realism the fast path trades away (a drag, an overlay, a `:hover` menu). There is deliberately **no per-call decorator**; the handle is the one seam. It composes at three scopes:

```go
b.Realistic().Click("#submit")                    // inline — the handle is cheap to make
rb := b.Realistic(); rb.Hover(".menu"); rb.Click(".menu .item")  // per-spec

var _ = Describe("checkout (realistic)", Label("realistic"), func() {  // per-suite
    var rb *biloba.Biloba
    BeforeEach(func() { rb = b.Realistic() })
    // ...use rb throughout...
})
```

With a `Label("realistic")`, `ginkgo --label-filter='realistic'` runs only the realistic lane and `--label-filter='!realistic'` keeps it out of the fast inner loop. For the full fast-vs-realistic **capability matrix** (what each track actually does, per interaction) and the deep dive, see `biloba:realistic-mode` and <https://onsi.github.io/biloba/#realistic-interactions>. To merely *assert* an element isn't occluded without paying for realistic mode, use the deterministic `b.BeClickable()` matcher (visible + enabled + topmost-at-its-center).

## Run with real backends.  But stub the network if all else fails.

Favor testing against real backends whenever possible and focus on fixing flakes and performance there.  But, if you must stub, stub the endpoints you don't want to depend on; everything unmatched passes through to the real network (`#stubbing-and-observing-the-network`):

```go
b.StubRequest(ContainSubstring("/api/users"), biloba.StubResponse{
	Body:    `[{"name":"Jane"},{"name":"Bob"}]`,
	Headers: map[string]string{"Content-Type": "application/json"},
})
b.Navigate("/app")
Eventually(".user").Should(b.HaveCount(2))
```

Stubs are per-tab and reset by `Prepare()`. Beyond `StubRequest` you can `b.AbortRequest(url)` (fail it), `b.ModifyRequest(url).WithURL/.WithMethod/.WithHeader/.WithBody(...)` (continue with overrides), and `b.ModifyResponse(url).WithStatus/.WithHeader/.WithBody/.Using(func(biloba.InterceptedResponse) biloba.StubResponse)` (rewrite a real response) — all share one first-match-wins handler list with `StubRequest`. Observe requests with `Eventually(b).Should(b.HaveMadeRequest(...))` and wait for quiet with `Eventually(b).Should(b.BeNetworkIdle())`.

## Seed state to skip slow flows

Set an auth cookie or `localStorage` to jump past login (navigate to a real origin first — `about:blank` can't hold cookies/storage):

```go
b.Navigate("/home")
b.SetCookie(biloba.Cookie{Name: "user", Value: "Joe"})
DeferCleanup(b.ClearCookies)
```

Or shortcut straight through your app's JS API (`#running-arbitrary-javascript`):

```go
b.Run(`app.load(` + jsonFixture + `); app.redraw()`)
Eventually("#doc-name").Should(b.HaveInnerText("My Fixture Data"))
```

**`b.Run` returns the decoded value directly** — `n := b.Run("app.users.length")` feeds Gomega without a wrapper, and `b.Run("expr", &typed)` decodes into a pointer. Don't write `runInt`/`runStr` helpers. `b.Run` is a synchronous *expression*, so a top-level `return` is illegal — use `b.RunAsync` (which wraps a function body, so `return`/`await` work) for `fetch`/`await`. `b.EvaluateTo` asserts on a JS expression directly. Remember numbers decode to `float64` (use `BeNumerically`).

**Poll a `b.Run` read instead of snapshotting it.** `b.Run` is a plain `func(string, ...any) any`, so it drops straight into `Eventually` — this is the antidote to the single most common flake (a one-shot read that races an async settle), and it needs no wrapper closure for a scalar expr:

```go
Eventually(b.Run).WithArguments(`isReady()`).Should(BeTrue())                         // bool
Eventually(b.Run).WithArguments(`document.querySelectorAll(".card").length`).Should(BeNumerically("==", 3)) // float64!
Eventually(b.Run).WithArguments(`document.title`).Should(Equal("Done"))               // string
```

For an interpolated/multi-line expr, pre-build the string (`expr := fmt.Sprintf(...)`; `Eventually(b.Run).WithArguments(expr)…`) or poll a closure that returns the decoded value. See `biloba:flaky-specs` for why single-shot reads flake.

**Don't hand-roll `getBoundingClientRect`/`scrollTop` through `b.Run` — Biloba has pollable geometry getters** (`b.GetBoundingBox`/`b.GetScrollOffset`/`b.GetOffsetTopWithin` + their `Have*` matchers) that wait for the element to be laid out (non-degenerate box) before reading. Relational layout ("A above/encloses/overlaps B") has the pairwise matchers `b.BeAbove`/`BeBelow`/`BeLeftOf`/`BeRightOf`/`Encloses`/`Overlaps` and the `b.GetGapBetween` delta getter (both boxes read in one atomic frame); on-screen-ness has `b.BeInViewport()`, document order has `b.BePrecededBy`/`b.BeFollowedBy`, and resolved computed style has `b.GetComputedStyle`. They're the idiomatic fix for the most race-prone class of `b.Run` reads → `biloba:api`.

## Multi-tab flows

```go
tab := b.NewTab()                // isolated, incognito-like context; closed by Prepare()
login(b, "sally"); login(tab, "jane")
Eventually(userXPath.WithText("Jane")).Should(b.HaveClass("online"))
```

Tabs opened *by the page* (e.g. `target="_blank"`) are **spawned tabs** — find them with the `HaveSpawnedTab`/`AllSpawnedTabs` (or `HaveTab`/`AllTabs`) queries:

```go
tab.Click(linkXPath)
Eventually(tab).Should(tab.HaveSpawnedTab().WithURL("https://youtube.com/..."))
yt := tab.AllSpawnedTabs().Find(tab.TabMatching().WithURL("https://youtube.com/..."))
```

A DOM method always operates on the tab it's invoked on (`tab.Click`, not `b.Click`). Dialogs and downloads are per-tab too — register dialog handlers **before** the action that triggers them.

## When Biloba can't express it

For realism (occlusion, scroll-into-view, real CSS `:hover`) reach for `b.Realistic()` (above) before chromedp. For everything else — cross-origin frames, geolocation, any CDP feature without a wrapper — drop to chromedp via `b.Context` (the escape hatch in `biloba:overview`). For real keystrokes use `b.Type` rather than `SetValue`.

Propose opening an issue if a common pattern is missing.

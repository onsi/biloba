---
name: api
description: One-line reference for every Biloba method and matcher, grouped by area — selectors/locators, lifecycle, poll-config (WithTimeout/WithPolling/WithContext/Immediate), capturing a matcher's observed value (.Capture), navigation (GetLocation/GetTitle), cookies/storage, tabs, DOM existence/visibility/contents/properties/forms, clicking and interactions (incl. drag/scroll/tap/modifiers/text-selection), realistic mode, keyboard, uploads, element JS, dialogs, downloads, arbitrary JS (incl. the GetJSValue app-state barrier), network stubbing/aborting/modifying/observing/holding (HoldResponse + Limit/ReleaseNext), and screenshots/outline/window. Use to look up the exact method or matcher name and shape. Methods marked (dual) poll until they succeed when fully applied and return a pollable matcher when under-applied.
---

# Biloba API reference

Terse lookup. **(dual)** = **polls until it succeeds** when fully applied (`b.Click("#go")` waits until the element is clickable, acts once, then stops), returns a Gomega matcher when under-applied (poll it yourself with `Eventually`). **(matcher)** = always returns a matcher. **first** = acts on the first match; **each** = acts on all matches. Selectors are CSS strings, `XPath` (see `biloba:xpath`), or semantic **`Locator`**s. Full docs: <https://onsi.github.io/biloba/>.

**Poll-by-default.** A fully-applied action/getter polls (the flake-resistant default — see `biloba:flaky-specs`). `b.Immediate()` opts back into act-once/fail-fast (rarely needed). The under-applied matcher form is for when you want to drive the `Eventually`/`Consistently` yourself.

## Selectors / locators
Three pathways, all flow through every method/matcher. **CSS is the default** (target stable `#id`/`[data-testid]` hooks, not styling classes); **locators** second (a11y assertions + readable text/label identifiers); **XPath** the rare power tool (axis/ordinal). CSS fastest, XPath fast, locators slowest (full-document ARIA scan).
- **CSS string** (`"#id"`, `".cls"`, `:has()`); `>>>` pierces open shadow roots / same-origin iframes (one boundary per `>>>`). **XPath** via `b.XPath(...)` (see `biloba:xpath`) — does **not** pierce shadow/iframe.
- **Locator constructors** (all have a `*Contains` variant where text-valued): `b.ByRole(role)`; `b.ByText(t)`/`b.ByTextContains(t)`; `b.ByLabel(t)`/`b.ByLabelContains(t)`; `b.ByPlaceholder(t)`; `b.ByAltText(t)`; `b.ByTitle(t)`; `b.ByTestID(id)` (attr = `biloba.TestIDAttribute`, default `"data-testid"`); `b.ByCSS(selector)` — raw CSS into the algebra (the only *structural* constructor; use to ordinally/filter-address a CSS selector, e.g. `b.ByCSS(".story").Nth(1)` for "the 2nd", instead of `:nth-of-type` or XPath).
- **Role refinements**: `.WithName(n)`/`.WithNameContains(n)` (accessible name); `.Level(n)` (heading level); ARIA-state filters `.Checked()`/`.Disabled()`/`.Expanded()`/`.Pressed()`/`.Selected()`.
- **Composition** (all accept any CSS/XPath/Locator): `.ContainingText(t)`/`.NotContainingText(t)`; `.Containing(sel)`/`.NotContaining(sel)`; `.And(sel)`/`.Or(sel)` (intersection/union); `.Within(scope)`/`.NotWithin(scope)` (descendants-of / not-descendants-of — `NotWithin` composes with `BePrecededBy`/`BeFollowedBy` for "follows in flow, not nested"); `.Nth(i)`/`.First()`/`.Last()`. Example: `b.ByRole("listitem").Containing(b.ByText("Delete")).Within("#cart").First()`.
- **A scope that doesn't resolve matches nothing** — which makes a *negative* assertion vacuous: `Consistently(b.ByTextContains("Draft").Within("#published-list")).ShouldNot(b.Exist())` passes instantly and permanently if `#published-list` never renders (the white-screen failure you wrote the guard for). Anchor the scope first: `Eventually("#published-list").Should(b.Exist())`, *then* assert the absence.
- Locators **pierce open shadow roots** automatically (no `>>>` needed). Accname covers aria-labelledby/aria-label/`<label>`/alt/placeholder/value/text/figcaption/caption/title.

## Lifecycle / config
- `biloba.SpinUpChrome(GinkgoT(), ...SpinUpOption)` — start Chrome (process 1). Options: `HighFidelityHeadless()`, `AutoInstallHeadlessShell()`, `HeadlessShellPath(p)`, `StartingWindowSize(w,h)`, `ChromeFlags(...)`. See `biloba:setup`.
- `biloba.ConnectToChrome(GinkgoT(), ...BilobaConfig)` — open this process's root tab `b`. Config: see `biloba:debug-failures`.
- `b.Prepare()` — reset the root tab between specs (BeforeEach, `OncePerOrdered`).
- `b.Context` — the tab's `chromedp` context (escape hatch).

## Poll config  (shallow `*Biloba` clones, à la `Realistic()`; not reset by `Prepare()`)
Tune or opt out of poll-by-default. Each returns a lightweight view of the same tab — use per-call (`b.WithTimeout(5*time.Second).Click("#go")`).
- `b.WithTimeout(d)` — override the `Eventually` timeout (else Gomega's global default).
- `b.WithPolling(d)` — override the polling interval.
- `b.WithContext(ctx)` — thread a context into the poll (cancellation aborts the wait).
- `b.Immediate()` — opt into act-once / fail-fast (today's old immediate behavior); the escape hatch, rarely needed.
- **Four-bucket rule** — misapplying config is a **hard error** (fails the spec):

  | Bucket | Methods | `WithTimeout`/`WithContext` | `WithPolling` | `Immediate` |
  |---|---|---|---|---|
  | **Polling** | dual actions, value-getters (incl. `GetLocation`/`GetTitle`/`GetJSValue`) | ✓ | ✓ | ✓ |
  | **Waiting command** | `Navigate`, `Capture*Screenshot*` (own ~30s/~5s defaults), `HoldResponse`+`ResponseHold.Await` (own 30s default) | ✓ (overrides own default) | error | error |
  | **Snapshot** | `HasElement`/`Count`/`Current*ForEach`/`ResponseHold.Count`/… | error | error | error |
  | **One-shot mutation** | `SetCookie`/`StubRequest`/`*Immediately`/`Run`/`RunAsync`/… | error | error | error |

  Configuring a call that resolves to a **bare matcher** (a `(matcher)` method, or the under-applied form of a dual method like `b.WithTimeout(d).Click()`) is also a hard error — configure the `Eventually`, not the matcher.

## Capturing values from matchers  (`.Capture(&target)`)
Every matcher that **reads a value off the page** returns a `*ValueMatcher` and takes `.Capture(&x)`: it writes what it observed into `x` as a side effect of a *successful* match, so you poll for a condition and keep the value that satisfied it **in one read**.
```go
var blockID string
Eventually(".figure-frame").Should(b.HaveAttribute("data-block-id", Not(BeEmpty())).Capture(&blockID))
```
- **Why:** asserting with a matcher and *then* calling a getter is two reads of a page that may have changed in between (the classic TOCTOU shape — a repaint between the two leaves you holding a value nothing ever asserted on).
- **Typed decode:** `target` is any non-nil pointer; a JS number lands in an `*int` as `3`, not `float64(3)`, and a JS object decodes into your struct. A type mismatch that can never come true (a string into an `*int`) fails **immediately** rather than waiting out the timeout.
- **Only on a match:** `ShouldNot`/`NotTo` captures nothing. While polling, the target is overwritten each successful attempt and holds the value from the attempt that passed.
- **Capturable:** `HaveAttribute`, `HaveProperty` (**both** forms, incl. existence-only), `HaveValue`, `HaveInnerText`, `HaveTextContent`, `HaveText`, `HaveClass`, `HaveCount`, `HaveDistinctCount`, `HaveJSONAttribute`, `HaveComputedStyle`, `HaveComputedStyleNumeric`, `EachHaveInnerText`, `EachHaveTextContent`, `EachHaveClass`, `EachHaveProperty` (**both** forms — the existence-only form reads the values in the same round-trip and hands back the same `[]any` slice the value-matching form does, so the no-arg `EachHaveInnerText()`/`EachHaveTextContent()` capture too), `HaveBoundingBox`, `HaveScrollOffset`, `HaveOffsetTopWithin`/`HaveOffsetLeftWithin`, `HaveGapBetween`, `EvaluateTo`, `HaveURL`, `HaveTitle`, `HaveLocalStorageItem`/`HaveSessionStorageItem`, `HaveNumLocalStorageItems`/`HaveNumSessionStorageItems`, `HaveNumCookies`, and `HaveCookie` (its own `.Capture(&cookie)`, composing with `.WithValue/.WithPath/…` in either order).
- **Not capturable, deliberately** (`.Capture` won't compile): matchers that read no value — `Exist`, `BeVisible`, `BeEnabled`, `BeClickable`, `BeFocused`, `BeChecked`, `BeInViewport`, `BeAbove`/`BeBelow`/`Encloses`/`Overlaps`/`BePrecededBy`/`BeFollowedBy`, `BeNetworkIdle` — and the actions-as-matchers (`Click`, `SetValue`, `Type`, …). Two more read something but stay bare on purpose: **`MatchColor`** (it does run JS, to normalize both colors — but it's a *sub-matcher* you hand to `HaveComputedStyle`; capture the style off `HaveComputedStyle` itself) and **`HaveMadeRequest`** (a builder-style `*RequestQuery`, not a `*ValueMatcher`; it already renders the requests it observed in its failure message, and the request object itself comes from `b.AllRequests().Find(b.RequestMatching(...))`).
- **`.Capture` returns a new matcher and leaves the receiver alone.** A `*ValueMatcher` held in a variable can be reused with different targets — `m.Capture(&idA)` then `m.Capture(&idB)` — without the second stealing the first's. `HaveCookie`'s `.Capture` copies too, and composes with the `.With*` refinements in either order.
- **Narrowing works for JS objects, not for Biloba's own structs.** Capturing a JS object into a struct that reads a *subset* of its fields is fine and common (`HaveJSONAttribute`). Capturing a Biloba struct — `Box`, `ScrollOffset`, `BoxDelta`, `Cookie` — into a *different* struct is rejected rather than half-filled in silence (they share field names and carry no json tags). Capture into the matching Biloba type (or an `any`) and read the fields you want.
- **Getter counterpart:** the `any`-returning getters take an optional trailing pointer with the same decode rules — `b.GetProperty("#row", "offsetWidth", &n)`, `b.GetAttribute(sel, "data-block-id", &id)`, `b.GetValue(sel, &v)`, `b.CurrentPropertyForEach(sel, name, &names)`, `b.CurrentAttributeForEach(...)`, `b.CurrentValueForEach(...)`. The value is still returned as before.

## Navigation
- `b.Navigate(url)` — navigate, assert `200`.
- `b.NavigateWithStatus(url, code)` — navigate, assert a specific status.
- `b.GetLocation()` / `b.GetTitle()` → string — current URL / title. **Polling getters** (the `Get` prefix means "polls and returns a value"): they honor all four knobs, and a **transient CDP error is a retryable miss** — a live navigation legitimately throws `Inspected target navigated or closed (-32000)` while Chrome swaps the target, which used to abort the poll. (**Breaking rename** — the old bare `Location`/`Title` names are gone.)
- `b.HaveURL(string|matcher)` (matcher) — assert tab URL; same transient-error-is-a-miss behavior under `Eventually`.
- `b.HaveTitle(string|matcher)` (matcher) — assert tab title.
- **Prefer a DOM readiness anchor over polling the URL**: `Eventually("#dashboard-root").Should(b.Exist())` proves the navigation *landed*, then `Ω(b.GetLocation()).Should(HaveSuffix("/dashboard"))` is a formality (see `biloba:write-tests`).

## Cookies & storage  (navigate to a real origin first)
- `b.SetCookie(...Cookie)` — set one or more cookies (default domain = current URL).
- `b.GetCookies()` → `Cookies` — all cookies in this context.
- `b.ClearCookies()` — clear them.
- `b.HaveCookie(name|matcher)` (matcher) — chain `.WithValue/.WithPath/.WithDomain/.WithSameSite/.WithSecure(...)/.WithHTTPOnly(...)`.
- `b.CookieMatching(...)` — same query as a predicate for `Cookies.Find/Filter`.
- `b.HaveNumCookies(int|matcher)` (matcher).
- `b.LocalStorage()` / `b.SessionStorage()` → handle with `Set(k,v)`, `Get(k,&ptr)`, `GetAll()`, `Remove(k)`, `Clear()`, `Length()` (JSON round-tripped).
- `b.HaveLocalStorageItem(key[, value])` / `b.HaveSessionStorageItem(...)` (matcher).
- `b.HaveNumLocalStorageItems(...)` / `b.HaveNumSessionStorageItems(...)` (matcher).

## Tabs
- `b.NewTab()` → `*Biloba` — new isolated tab (own context); closed by `Prepare()`.
- `tab.Close()` — close a tab (returns error; `Eventually(tab.Close).Should(Succeed())` during downloads).
- `b.AllTabs()` / `b.AllSpawnedTabs()` → `Tabs`.
- `b.HaveTab()` / `b.HaveSpawnedTab()` (matcher) — chain `.WithURL/.WithTitle/.WithDOMElement(selector)`.
- `b.TabMatching()` — same query as a predicate for `Tabs.Find/Filter`.

## Existence, count, visibility, enabled
- `b.HasElement(selector)` → bool (first).
- `b.Exist()` (matcher) — element matches.
- `b.Count(selector)` → int / `b.HaveCount(int|matcher)` (matcher).
- `b.HaveDistinctCount(attribute, int|matcher)` (matcher) — count of **distinct** values `attribute` takes across matches (dedupe transient double-painted nodes by a stable `data-*` key).
- `b.BeVisible()` (matcher) — non-zero `offsetWidth`/`offsetHeight`. / `b.EachBeVisible()` (matcher) — **≥1 match AND all visible** (fails on zero matches).
- `b.BeEnabled()` (matcher) — `!el.disabled`. / `b.EachBeEnabled()` (matcher) — **≥1 match AND all enabled** (fails on zero matches).
- `b.BeClickable()` (matcher) — visible + enabled + topmost at its center (deterministic occlusion guard; opt-in, `Click` does **not** run it).

## Contents, classes, attributes, state
- `b.GetInnerText(selector)` → string (first; **polls** until the element is present — empty string is a valid value) / `b.HaveInnerText(string|matcher)` (matcher, exact).
- `b.GetTextContent(selector)` → string (first; polls until present) / `b.HaveTextContent(string|matcher)` (matcher).
- `b.HaveText(string|matcher)` (matcher) — trims & collapses whitespace before matching.
- `b.CurrentInnerTextForEach(selector)` → []string (each; **snapshot**, no poll) / `b.EachHaveInnerText(value|matcher)` (matcher — **≥1 match AND all satisfy**; the no-arg form no longer means "every text is empty" — it now asserts the property is *defined* on every match and captures the slice, so assert none via `HaveCount(0)`). Same for `b.CurrentTextContentForEach` / `b.EachHaveTextContent(...)`.
- `b.HaveClass(string|matcher)` (matcher) — string ⇒ "list contains"; matcher receives `[]string`. / `b.EachHaveClass(string)` (matcher) — **≥1 match AND all have the class** (fails on zero matches).
- `b.HaveAttribute(name[, string|matcher])` (matcher) — HTML attribute via `getAttribute`.
- `b.HaveComputedStyle(prop, string|matcher)` (matcher) — via `getComputedStyle`; getter counterpart `b.GetComputedStyle(selector, prop)` → string (see Geometry).
- `b.BeChecked()` (matcher) — checkbox/radio checked. An element with **no `checked` property at all** is an **error**, not "unchecked" — so selecting the wrapping `<label>`/`<div>` instead of the `<input>` fails loudly instead of letting `ShouldNot(b.BeChecked())` pass forever.
- `b.BeFocused()` (matcher) — is `document.activeElement`.

## Properties  (`.` paths like `dataset.name`; JS types preserved — numbers are `float64`)
**Two-axis polling**: the singular `Get*` getters poll until the element is present **AND** every named property/attribute is *defined*. Wrap a name in `b.AllowMissing("name")` to make an absent value a valid `nil` rather than something to wait for. **Sharp edge:** a property that simply doesn't exist on that element type (e.g. `disabled` on a `<div>` — `"disabled" in div` is false) would block the poll forever — wrap it in `AllowMissing`. The names params accept `string` or `AllowMissing` (`any`).
- `b.GetProperty(selector, name[, &ptr])` → any (first; polls; optional trailing pointer decodes the value for you) / `b.SetProperty(selector, name, value)` (dual) / `b.HaveProperty(name[, value|matcher])` (matcher).
- `b.GetProperties(selector, ...names)` → `Properties` (first; polls); getters `GetString/GetInt/GetFloat64/GetBool/GetStringSlice`.
- `b.GetAttribute(selector, name[, &ptr])` → any (first; polls; raw `getAttribute` markup, not the resolved property; optional trailing pointer decodes) / `b.GetAttributes(selector, ...names)` → `Properties` (first; polls).
- `b.AllowMissing(name)` — wrap a name passed to the four two-axis getters (`GetProperty`/`GetProperties`/`GetAttribute`/`GetAttributes`) so absent ⇒ `nil`, doesn't block the poll. No effect elsewhere.
- `b.GetJSONAttribute(selector, attribute, &out)` (first; polls until present, set, **and** parses as JSON, then decodes into the `out` pointer — a `*struct`/`*map`/`*any`). / `b.HaveJSONAttribute(attribute, matcher)` (matcher) — parses the attribute and hands the decoded value (`map[string]any`/`[]any`/`float64`, à la `encoding/json` into `any`) to `matcher`; composes with `gstruct`.
- **Snapshot plural getters (no poll; `nil` for absent; gate presence first with `Eventually(sel).Should(b.HaveCount(n))`):** `b.CurrentPropertyForEach(selector, name[, &ptr])` → []any, `b.CurrentPropertiesForEach(selector, ...names)` → `SliceOfProperties` (getters return slices; `.Get(key)`, `.Find(key, val|matcher)`, `.Filter(key, val|matcher)`), `b.CurrentAttributeForEach(selector, name[, &ptr])` → []any, `b.CurrentAttributesForEach(selector, ...names)` → `SliceOfProperties`. They return an **empty slice** when nothing matches — which quietly satisfies a *negated* collection assertion (`Ω(b.CurrentInnerTextForEach(".row")).ShouldNot(ContainElement("Draft"))` passes against zero rows), so the `HaveCount(n)` gate is what keeps a negative honest as well as what fixes the timing. (Positive assertions are safer: Gomega's `HaveEach` errors on an empty slice.)
- `b.SetPropertyForEachImmediately(selector, name, value)` — set on **all** matches now, no poll (the `Immediately` suffix is the "know what you're doing" smell). / `b.EachHaveProperty(name[, ...])` (matcher — ≥1 match AND all satisfy).

## Form values  (rationalizes text/checkbox/radio/multi-select)
- `b.GetValue(selector[, &ptr])` → any (first; polls until present — empty string / unselected radio `""` is a valid value, no "defined" axis; bool for checkbox, checked radio's `value`, `[]string` for multi-select; optional trailing pointer decodes). / `b.CurrentValueForEach(selector[, &ptr])` → []any (each; snapshot, no poll).
- `b.SetValue(selector, value)` (dual) — requires visible+enabled; focuses, sets, blurs, fires `input`+`change`. Does **not** type real keys. For a `<select>` the value is matched against the **option `value`**, not its visible label (assert labels via `option.textContent`).
- `b.ValueLabel(label)` — wrap a `SetValue` arg to target a `<select>` option by its **visible label** instead of its value: `b.SetValue(sel, b.ValueLabel("Sonnet"))`. Multi-select: pass a slice whose entries are `ValueLabel`s (labels and raw values may be mixed). `<select>` only.
- `b.HaveValue(value|matcher)` (matcher).

## Geometry  (pollable layout reads — fold in layout-readiness; use instead of hand-rolled `b.Run` geometry)
**Readiness**: getters poll until the element is present **AND** laid out (non-degenerate box, `width`/`height` > 0) — so you never read a zero box mid-layout. All return viewport-relative CSS pixels. Each getter has a `Have*` matcher counterpart for `Eventually(sel).Should(...)` when the value is converging.
**A missing element is an error, not a non-match** — so `Ω("#toast").ShouldNot(b.BeInViewport())` no longer passes vacuously against an element that never rendered (the pairwise/offset probes name *which* selector went missing: subject, container, or other). Under `Eventually` an error is still just a failed attempt, so waiting for an element to show up works exactly as before; and a present-but-zero-area box stays a silent retry, so positive assertions keep polling through late layout.
> **Migration:** Gomega never counts an assertion satisfied while the matcher errors — in *either* direction — so this also breaks the legitimate **wait-for-teardown** spec: `Eventually("#toast").ShouldNot(b.BeInViewport())` used to pass the moment the node left the DOM and now times out. Same for `HaveBoundingBox`/`HaveScrollOffset`/`HaveOffsetTopWithin`/`HaveGapBetween`, the pairwise matchers, and any `Consistently(sel).ShouldNot(...)` where `sel` is conditionally rendered. **Assert disappearance with the matchers that are *about* existence** — `Eventually("#toast").ShouldNot(b.Exist())` or `Should(b.HaveCount(0))` — and keep the geometry matchers for claims about an element that *is* there. This makes geometry consistent with `BeVisible`/`BeEnabled`/`BeClickable`, which have always errored on a missing element; the convention is now uniform rather than split.
- `b.GetBoundingBox(selector)` → `Box{Top,Left,Width,Height,Bottom,Right,CenterX,CenterY,ClientWidth,ClientHeight}` (first; polls). / `b.HaveBoundingBox(matcher)` — matcher receives the `Box` (compose with Gomega's `HaveField`). `Width`/`Height` are **border-box** (incl. scrollbar gutter, like `getBoundingClientRect`); `ClientWidth`/`ClientHeight` are the **client box** (`clientWidth`/`clientHeight` — content+padding, scrollbar excluded) — reach for them for "content width of this scroll container."
- `b.GetScrollOffset(selector)` → `ScrollOffset{Top,Left,MaxTop,MaxLeft}` (scroll container; `Top==MaxTop` ⇒ scrolled to bottom). / `b.HaveScrollOffset(matcher)`.
- `b.GetOffsetTopWithin(selector, container)` → float64 (`element.top - container.top`; "scrolled near the top of the pane"). `b.GetOffsetLeftWithin(selector, container)` is the horizontal sibling. / `b.HaveOffsetTopWithin(container, value|matcher)`, `b.HaveOffsetLeftWithin(container, value|matcher)`.
- **Pairwise (element-to-element; both boxes read in one atomic frame — don't split into two `GetBoundingBox`es, that loses the single-frame poll):** `b.BeAbove(other)` (`subject.Bottom<=other.Top`), `b.BeBelow(other)`, `b.BeLeftOf(other)` (`subject.Right<=other.Left`), `b.BeRightOf(other)`, `b.Encloses(other)` (contains on all 4 edges), `b.Overlaps(other)` (boxes intersect) — all matchers: `Eventually(subjectSel).Should(b.BeAbove(otherSel))`.
- `b.GetGapBetween(selector, other)` → `BoxDelta{Top,Left,Bottom,Right,Width,Height,CenterX,CenterY}` (subject minus other; first; polls — `CenterX~0` ⇒ shared center line, `Width~0` ⇒ same size). / `b.HaveGapBetween(other, value|matcher)` — matcher receives the `BoxDelta`.
- `b.BeInViewport(options...)` (matcher) — element is laid out **and** its box intersects the visible layout viewport (actually on screen; ≠ `BeVisible`, which is only "rendered"). Partial overlap counts by default; pass `b.Fully()` to require the whole box on screen (all 4 edges within the viewport).
- `b.BePrecededBy(other)` / `b.BeFollowedBy(other)` (matchers) — document order via `compareDocumentPosition`. **Read the subject first:** `Eventually(X).Should(b.BeFollowedBy(Y))` ⇔ **X comes BEFORE Y**; `Eventually(X).Should(b.BePrecededBy(Y))` ⇔ **X comes AFTER Y**. ("Quiz renders after the note" = `Eventually(noteSel).Should(b.BeFollowedBy(quizSel))`.) These read backwards to most people and **a backwards assertion does not announce itself** — on a fixture that happens to satisfy the inverted relation it just passes. Guard it by asserting the inverse does *not* hold too (`Ω(noteSel).ShouldNot(b.BeFollowedBy(sectionSel))`). On failure the message reports the order actually observed (`Actually: #note comes BEFORE #section.`). "Anywhere after" includes *inside* — scope with `Locator.NotWithin` for "after Y but not nested in Y".
- `b.GetComputedStyle(selector, property)` → string (first; polls; resolved value via `getPropertyValue`, so kebab-case names and CSS custom properties like `--stage` resolve — the getter counterpart of `HaveComputedStyle`, for Go-side math on the value).
- `b.GetComputedStyleNumeric(selector, property)` → float64 (first; polls; leading number of the computed value via `parseFloat`, so `"16px"`→`16`; non-numeric value fails). / `b.HaveComputedStyleNumeric(property, number|matcher)` (matcher; a plain number compares with `BeNumerically`).
- `b.NormalizeColor(color)` → string (**one-shot snapshot / pure transform**, no selector, no poll) — normalizes any CSS `<color>` incl. a `var(--token)` chain (resolved against `:root`-scoped custom properties) to canonical `rgb()`/`rgba()`. / `b.MatchColor(color)` (matcher) — normalizes **both** sides to `rgb()` before comparing; pass as the expected to `HaveComputedStyle`: `b.HaveComputedStyle("stroke", b.MatchColor("var(--tok-teal)"))`.

## Clicking & interactions  (pragmatic simulations)
- `b.Click(selector)` (dual) — visible+enabled, then `el.click()`.
- `b.DblClick(selector)` (dual) — two clicks + `dblclick`. `b.RightClick(selector)` (dual) — `mousedown`/`mouseup`/`contextmenu`. `b.MiddleClick(selector)` (dual) — `mousedown`/`mouseup`/`auxclick`.
- `b.Tap(selector)` (dual) — synthetic touch/pointer events + `click` (realistic: real CDP `touchStart`/`touchEnd`); accepts `b.At(...)`, ignores modifiers.
- **Pointer options** — `b.At(x,y)` (offset from top-left, à la canvas/map/slider), `b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` (⌘/Win) — accepted by `Click`/`DblClick`/`RightClick`/`MiddleClick`/`Tap`, after the selector or in place of it (matcher form). They compose: `b.Click(sel, b.At(30,40), b.Shift())`. In fast mode any option switches a click off native `el.click()` to a synthetic event carrying coords+flags; realistic uses real CDP input natively.
- `b.DragTo(source, target)` (dual) — pointer-based drag (`pointerdown`/`move`/`up`); drives @dnd-kit-style DnD, not native HTML5 `draggable`. Matcher subject is the source: `Eventually(src).Should(b.DragTo(tgt))`.
- `b.ScrollWheel(selector, deltaX, deltaY)` (dual; matcher form `b.ScrollWheel(deltaX, deltaY)`) — `wheel` event then scrolls nearest scrollable ancestor (realistic: real CDP wheel); +deltaY=down, +deltaX=right.
- `b.ClickEachImmediately(selector)` — click all visible+enabled matches now, no poll (the `Immediately` suffix flags the no-readiness-fold smell; gate presence first).
- `b.Focus(selector)` (dual) / `b.Blur(selector)` (dual) / `b.Hover(selector)` (dual; fires pointer/mouse events, not CSS `:hover`).
- `b.ScrollIntoView(selector, ...ScrollOption)` (dual) — bare = native `scrollIntoView()`; options: `b.WithinScroller(container)` (scroll a specific container, not the nearest ancestor), `b.AtTopOffset(px)` (land the target `px` below the container top — "clear the sticky header"). Instant/deterministic, occlusion-*un*aware (use `Realistic()` for the animated, hit-tested scroll).
- `b.SelectText(selector)` (dual) — select all of the element's text as a real `window.getSelection()` range, dispatching `mouseup` (drives highlight→menu/annotation UIs).
- `b.SelectRange(selector, start, end)` (dual; matcher form `b.SelectRange(start, end)`) — select chars `[start, end)` across the element's text nodes; same range+mouseup. Read back with `Eventually("window.getSelection().toString()").Should(b.EvaluateTo(…))`.
- `b.ClearSelection()` — clear any active selection (no matcher).

## Realistic mode  (opt-in; real CDP input instead of fast JS simulation)
- `b.Realistic()` → `*Biloba` — a view of the **same tab** whose interactions run through real Chrome DevTools Protocol input: scrolls into view, waits for stability, refuses to click through an occluding overlay, moves the real pointer (CSS `:hover` activates), dispatches genuine mouse/touch/key input. Per-spec opt-in (real round-trips, can reintroduce flake); the whole interaction vocabulary above works on it. No per-call decorator.
- Compose inline (`b.Realistic().Click(sel)`), per-spec (`rb := b.Realistic()`), or per-suite (`Label("realistic")` + `BeforeEach{ rb = b.Realistic() }`, then `ginkgo --label-filter='realistic'`/`'!realistic'`). Fast-vs-realistic capability matrix: <https://onsi.github.io/biloba/#realistic-interactions>.

## Keyboard  (real key events, via chromedp)
- `b.Type(...)` (dual) — **the** element-targeted keyboard method: focus, then genuine keystrokes (text **and** named `Keys.*`); **appends**; focusing scrolls into view. Arg disambiguation (after stripping modifiers):
  - `b.Type(selector, payload...)` — **immediate** (polls): selector + ≥1 payload arg. `b.Type("input", "hello")`, `b.Type("input", "hello", biloba.Keys.Enter)`, `b.Type("input", biloba.Keys.Enter)`.
  - `b.Type(payload)` — **matcher**: a single string, or one-or-more `Keys.*`. `Eventually("#in").Should(b.Type("hello"))`, `Eventually("#in").Should(b.Type(biloba.Keys.Enter))`.
  - Limitation: the matcher form can't mix *leading text + trailing keys* (`b.Type("hello", Keys.Enter)` reads as immediate selector=`"hello"`). Fine — the immediate form polls, so use it; the matcher form is only for custom `Consistently`/composition.
- `b.SendKeysToWindowImmediately(...parts)` — **focus-free, no selector, no matcher, no poll**: text + named keys land on the focused element, else fire on `document`/window (global hotkeys). Only you know what should be focused, so it can't poll — gate first: `Eventually(sel).Should(b.BeFocused())` then send. To type *into* a specific element, use `b.Type` (which focuses it).
- `biloba.Keys.{Enter,Tab,Escape,Backspace,Delete,Arrow{Up,Down,Left,Right},Home,End,PageUp,PageDown}`.
- **Modifiers** `b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` work here too (same values as the pointer modifiers): pass them in any position to `Type`/`SendKeysToWindowImmediately` for Shift-Enter, ⌘-A, etc. — `b.Type("textarea", biloba.Keys.Enter, b.Shift())`.

## Uploads
- `b.SetUpload(selector, ...paths)` (dual; matcher form `b.SetUpload(path)` or, for multiple files, `b.SetUpload([]string{...})`) — set `<input type=file>` files via CDP (paths must exist on Chrome's machine); fires `change`. In the matcher form multiple files must be a single `[]string` (bare variadic paths would be ambiguous with the immediate selector+paths form).

## Run JS on selected elements
- `b.InvokeOn(selector, method, ...args)` → any (first; **polls** until present) — `el[method](...args)`.
- `b.InvokeOnEachImmediately(selector, method, ...args)` → []any (each; snapshot, no poll).
- `b.InvokeWith(selector, jsFn, ...args)` → any (first; polls until present) — `jsFn(el, ...args)`.
- `b.InvokeWithEachImmediately(selector, jsFn, ...args)` → []any (each; snapshot, no poll).

## Dialogs  (register handlers BEFORE the triggering action; per-tab; reset by Prepare)
- `b.HandleAlertDialogs()` / `HandleConfirmDialogs()` / `HandlePromptDialogs()` / `HandleBeforeUnloadDialogs()` → `DialogHandler`.
- chain `.MatchingMessage(string|matcher)`, `.WithResponse(bool)`, `.WithText(s)`.
- `b.RemoveDialogHandler(h)`.
- `b.Dialogs()` → filter `.OfType(biloba.DialogType...)`, `.MatchingMessage(...)`, `.MostRecent()`.
- `b.HaveAlertDialog(...)` etc. (matcher). Defaults: alerts accepted; confirm/prompt cancelled; beforeunload accepted.

## Downloads  (per-tab; auto-tracked)
- `b.AllDownloads()` / `b.AllCompleteDownloads()` → `Downloads`.
- `b.HaveDownloaded([filename])` (matcher) — chain `.WithURL(...)`, `.WithContent([]byte|matcher)`; complete downloads only.
- `b.DownloadMatching(...)` — predicate for `Downloads.Find`.
- `Download`: `.URL`, `.Filename`, `.IsComplete()/.IsCancelled()/.IsActive()`, `.Content()` → []byte.

## Arbitrary JS  (runs on the global `window`; wrap object literals in parens)
- `b.Run(script[, &ptr])` → any — synchronous **expression**; returns the decoded value (no `return` allowed at top level — it errors with a hint pointing to `RunAsync`/IIFE). Pollable: `Eventually(b.Run).WithArguments(expr).Should(matcher)` (it's a `func(string,...any) any`) — the fix for a single-shot read that races an async settle (`biloba:flaky-specs`).
- `b.RunAsync(script[, &ptr])` / `b.RunErrAsync(...)` — body of an async fn; you `return` the awaited value (use this for `await`/`fetch`).
- `b.GetJSValue(expression[, &ptr])` → any — **polls** `expression` until it is *defined*, then returns it. The **app-state barrier**: point it at a global the app writes (`window.__storeLog`) to prove the *browser* processed something. Retries through `undefined` **and** through a thrown error (a `ReferenceError` for a not-yet-created global is "not ready", not a bug); `null` is a legitimate value and returns immediately. Pass a pointer to decode into a concrete type (the way around the JSON-`float64` gotcha).
  - **It gates on definedness and nothing else.** As a barrier it works only when the path is created **lazily, by the very event you're waiting for** (`window.__log ??= []` inside the subscriber). Against a log the app creates **eagerly** it returns `[]` on the first tick and gates nothing — see `biloba:flaky-specs` Smell 3.
  - **Wrong wherever absence is meaningful** — because it *waits* for existence it cannot express a probe where "the global isn't there" is a valid reading (a ledger absent on `about:blank` means *quiet*; `window.__renderErrors` absent means *no errors*; a flag planted before a JS-only tab switch that must have *survived*; a baseline count taken before the action). Those stay `b.Run` with a defensive coalesce (`window.__x ?? null`) — that is correct, not a smell.
  - For a *condition* rather than a value, use `Eventually(expr).Should(b.EvaluateTo(matcher))` — and `.Capture(&typed)` on it to keep the value that satisfied the condition.
- `b.EvaluateTo(value|matcher)` (matcher) — assert a JS expression's result. Numbers decode to `float64` — use `BeNumerically`, not `Equal(intLiteral)`. **Asymmetry worth knowing:** the sub-matcher you pass sees the **raw JSON-decoded** value (`[]any` of `map[string]any` — match with `HaveKeyWithValue`, not `HaveField`), while `.Capture(&typed)` hands you the **typed** value decoded into your own struct.
- `b.JSFunc(script)` → `.Invoke(...args)` string — JSON-encodes args into an invocable snippet.
- `b.JSVar(nameOrExpr)` — reference a JS variable/expression as a `JSFunc` argument (don't quote it).

## Network  (per-tab; reset by Prepare)
- `b.StubRequest(url string|matcher, biloba.StubResponse{Status,Body,Headers})` — first handler enables interception; unmatched requests pass through. Handlers below share one ordered, first-match-wins list.
- **First-match-wins bites in an `Ordered` container**: `Prepare()` is `OncePerOrdered`, so handlers **accumulate** across the `It`s — a handler registered by an earlier spec permanently claims that URL and an identical one in a later spec is silent dead code. Symptom + fixes in `biloba:flaky-specs`.
- While interception is on, Biloba **disables the HTTP cache** (`Network.setCacheDisabled(true)`, restored by `Prepare()`) — a cached response never traverses the network stack, so it would raise no Fetch event and silently skip every stub/abort/modify handler (it also means `ModifyResponse.Using` can no longer be handed a stale cached body).
- `b.AbortRequest(url string|matcher)` — fail matching requests (page's fetch rejects).
- `b.ModifyRequest(url string|matcher)` → builder `.WithURL(u).WithMethod(m).WithHeader(n,v).WithBody(b)` — continue to the real network with overrides (only what you set).
- `b.ModifyResponse(url string|matcher)` → builder `.WithStatus(s).WithHeader(n,v).WithBody(b)` or `.Using(func(biloba.InterceptedResponse) biloba.StubResponse)` — rewrite the real response (reads real status/headers/body; heavier: pauses twice).
- `b.HoldResponse(url string|matcher)` → `*ResponseHold` — intercept the **real** response and **hold it hostage** in flight until you release it (then it passes through unchanged). Builds on `ModifyResponse` (same tab scoping, same first-match-wins list). **The** tool for forcing an arrival order when testing optimistic-UI reconciliation → `biloba:flaky-specs`.
  - **A hold freezes EVERY matching response by default**, not just the first — a second request to the same URL is frozen too. That default is what you want for "nothing lands until I say so"; it is *not* what you want for "hold #1 while #2 flies past" (see `Limit`).
  - `hold.Await()` → `InterceptedResponse` — blocks until a match is actually being held, then returns **the oldest one still held** (immediately if one already arrived). Responses that passed through because the hold was at its `Limit` were never held, so `Await` skips them; after a bare `Release()` it returns the first response the hold intercepted rather than blocking. Waiting command: own 30s default, honors `WithTimeout`/`WithContext` **set on the tab you build the hold from** (`b.WithTimeout(d).HoldResponse(u)`); `WithPolling`/`Immediate` are a hard error.
  - `hold.Limit(n)` → `*ResponseHold` — hold at most `n` concurrently; while `n` are held, further matches **pass straight through untouched** (they still count). `Limit(1)` is what makes "the first save's response is held while the second lands" expressible. Releasing frees capacity, so the next match is held again. `n ≥ 1`; unlimited by default. The limit is consulted **only as each response arrives**, so set it when you build the hold: lowering it later never releases responses already held, and raising it never retroactively holds one that already flew past.
  - `hold.Release()` — **terminal**: everything currently held goes through AND the hold stops holding future matches. Idempotent.
  - `hold.Release(r)` — release just the response `Await()` handed you; the hold **stays armed** so the next match is held like the first. Releasing something this hold isn't holding fails the spec (matched by value, oldest first).
  - `hold.ReleaseNext()` — release the oldest still-held response and stay armed — the step-one-at-a-time form (`Await(); ReleaseNext(); Await()` waits for the next one). Fails the spec when nothing is held: a release with nothing to release means the spec's sequencing is off, and that should be loud.
  - `hold.Count()` → int — **every** match intercepted so far, held or passed through; a snapshot (no knobs) but safe to poll: `Eventually(hold.Count).Should(Equal(1))`. It's also the assertion that proves the interception you *think* happened actually did.
  - Held responses are **force-released at spec end and by `Prepare()`** — a failing spec can't wedge the tab.
  - **Sharp edge:** matching is **tab-wide and URL-based**, so a hold can catch a response from an *earlier* page load — a URL substring does not identify a page generation. Scope it to a dedicated `b.NewTab()`, or assert `Count()`, when that matters. First-match-wins also means a **second `HoldResponse` for a URL an earlier one already claims is dead code** — re-arm the hold you already have with `ReleaseNext` instead.
- `b.HaveMadeRequest(url string|matcher)` (matcher) — chain `.WithMethod(m)`.
- `b.AllRequests()` → `Requests` (each `*Request` has `.URL/.Method/.Headers/.ResourceType`); `b.RequestMatching(...)` predicate for `.Find/.Filter`.
- `b.BeNetworkIdle()` (matcher) — zero in-flight requests, evaluated the instant it's polled (no quiet period). Tracks **HTTP** requests only (keyed on `Network.requestWillBeSent`/`loadingFinished` request IDs); a long-lived **WebSocket** does not keep it busy, so it won't wait for WS frames. **It passes before the request you're waiting for has even started** — `b.Click("#refresh")` then `Eventually(b).Should(b.BeNetworkIdle())` can satisfy at t=0 with nothing in flight yet. Anchor it: `Eventually(b).Should(b.HaveMadeRequest(...))` first, *then* wait for idle.

## Screenshots, outline, window  (details in biloba:debug-failures)
- `b.Outline()` → string — indented DOM text.
- `b.A11yOutline()` → string — accessibility tree (role + name).
- `b.CaptureScreenshot()` → []byte (PNG) / `b.CaptureImgcatScreenshot()` → string / `b.CaptureScreenshotToFile(path)` → abs path.
- `b.CaptureScreenshotOf(selector)` → []byte / `b.CaptureImgcatScreenshotOf(selector)` → string / `b.CaptureScreenshotOfToFile(selector, path)` → abs path — clipped to the first matching element (any selector; works below the fold and across `>>>` boundaries).
- `b.SetWindowSize(w, h, ...opt)` (auto-resets via DeferCleanup) / `b.WindowSize()`. Because it registers its own `DeferCleanup` to restore the prior size, you don't need a manual restore — and you must **not** call it from inside another `DeferCleanup` (Ginkgo forbids nesting), so call it bare in `BeforeEach`/`BeforeAll`.

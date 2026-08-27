---
name: api
description: One-line reference for every Biloba method and matcher, grouped by area — selectors/locators, lifecycle, poll-config (WithTimeout/WithPolling/WithContext/Immediate), capturing a matcher's observed value (.Capture), navigation (GetLocation/GetTitle), cookies/storage, tabs, DOM existence/visibility/contents/properties/forms, clicking and interactions (incl. drag/scroll/tap/modifiers/text-selection), realistic mode, keyboard, uploads, element JS, dialogs, downloads, arbitrary JS (incl. the GetJSValue app-state barrier), network stubbing/aborting/modifying/observing/holding (HoldResponse + Limit/ReleaseNext), screenshots/outline/window, and visual regression (HaveScreenshot + Mask/Tolerance/ChannelTolerance/Animated/InColorSchemes). Use to look up the exact method or matcher name and shape. Methods marked (dual) poll until they succeed when fully applied and return a pollable matcher when under-applied.
---

# Biloba API reference

Terse lookup. Docs: <https://onsi.github.io/biloba/>.

**Naming conventions — assume these, they're only restated where an entry breaks them:**

| Shape | Means |
|---|---|
| `Have*` / `Be*` / `Each*` / `Match*` | a Gomega matcher. `Each*` = **≥1 match AND all satisfy** (fails on zero matches). |
| `Get*` | polls the **first** match until ready, returns the value. Optional trailing `&ptr` decodes it. |
| `Current*ForEach` | **snapshot** over **all** matches, no poll, returns a slice (empty when nothing matches). |
| `*Immediately` | acts on **all** current matches now, no poll. The long suffix is a "know what you're doing" smell. |
| **(dual)** | fully applied → **polls**, acts once, stops (`b.Click("#go")`). Under-applied → a matcher you poll (`Eventually("#go").Should(b.Click())`). |

Selectors: CSS strings, `XPath` (`biloba:xpath`), or `Locator`s. `b.Immediate()` opts out of poll-by-default; rarely needed.

## Selectors / locators

**CSS is the default** (stable `#id`/`[data-testid]`, not styling classes); **locators** for a11y + text/label identifiers; **XPath** the rare axis/ordinal tool. Speed: CSS > XPath > locators (full-document ARIA scan).

- **CSS**: `"#id"`, `.cls`, `:has()`. `>>>` pierces one open-shadow-root / same-origin-iframe boundary per occurrence.
- **XPath**: `b.XPath(...)` — pierces neither.
- **Locators** (`*Contains` variant on every text-valued one): `b.ByRole(r)`, `b.ByText`, `b.ByLabel`, `b.ByPlaceholder`, `b.ByAltText`, `b.ByTitle`, `b.ByTestID(id)` (attr = `biloba.TestIDAttribute`, default `"data-testid"`), `b.ByCSS(sel)` — raw CSS into the algebra, the only *structural* constructor (`b.ByCSS(".story").Nth(1)` for "the 2nd", not `:nth-of-type`). They **pierce open shadow roots** automatically. Accessible name covers aria-labelledby/aria-label/`<label>`/alt/placeholder/value/text/figcaption/caption/title.
- **Role refinements**: `.WithName(n)`/`.WithNameContains(n)`, `.Level(n)`, `.Checked()`/`.Disabled()`/`.Expanded()`/`.Pressed()`/`.Selected()`.
- **Composition** (each accepts any CSS/XPath/Locator): `.ContainingText`/`.NotContainingText`, `.Containing`/`.NotContaining`, `.And`/`.Or`, `.Within`/`.NotWithin`, `.Nth(i)`/`.First()`/`.Last()`. E.g. `b.ByRole("listitem").Containing(b.ByText("Delete")).Within("#cart").First()`.
- **Trap — an unresolved scope matches nothing, so a negative assertion is vacuous.** Anchor the scope first:
  ```go
  Eventually("#published-list").Should(b.Exist())
  Consistently(b.ByTextContains("Draft").Within("#published-list")).ShouldNot(b.Exist())
  ```

## Lifecycle

- `biloba.SpinUpChrome(GinkgoT(), ...SpinUpOption)` — start Chrome (process 1). Options: `HighFidelityHeadless()`, `AutoInstallHeadlessShell()`, `HeadlessShellPath(p)`, `StartingWindowSize(w,h)`, `SkipConfigFile()`, `ChromeFlags(...)`. → `biloba:setup`
- `biloba.ConnectToChrome(GinkgoT(), ...BilobaConfig)` — open this process's root tab `b`. Config → `biloba:debug-failures`
- `b.Prepare()` — reset the root tab between specs (`BeforeEach`, `OncePerOrdered`).
- `b.Context` — the tab's `chromedp` context (escape hatch).

## Poll config

Shallow `*Biloba` clones (like `Realistic()`); not reset by `Prepare()`. Use per-call: `b.WithTimeout(5*time.Second).Click("#go")`.

`b.WithTimeout(d)` · `b.WithPolling(d)` · `b.WithContext(ctx)` (cancellation aborts the wait) · `b.Immediate()` (act once, fail fast).

**Four-bucket rule — misapplying a knob is a hard error:**

| Bucket | Methods | `WithTimeout`/`WithContext` | `WithPolling` | `Immediate` |
|---|---|---|---|---|
| **Polling** | dual actions, value-getters (incl. `GetLocation`/`GetTitle`/`GetJSValue`) | ✓ | ✓ | ✓ |
| **Waiting command** | `Navigate`, `Capture*Screenshot*` (own ~30s/~5s defaults), `HoldResponse`+`ResponseHold.Await` (own 30s) | ✓ (overrides own default) | error | error |
| **Snapshot** | `HasElement`/`Count`/`Current*ForEach`/`ResponseHold.Count`/… | error | error | error |
| **One-shot mutation** | `SetCookie`/`StubRequest`/`*Immediately`/`Run`/`RunAsync`/… | error | error | error |

Configuring anything that resolves to a **bare matcher** — a `(matcher)` method (`b.HaveScreenshot(name)`, `b.Exist()`, …), or the under-applied dual form (`b.WithTimeout(d).Click()`) — is also a hard error. Configure the `Eventually`, not the matcher.

## `.Capture(&target)`

Every matcher that **reads a value off the page** returns a `*ValueMatcher` with `.Capture(&x)`: it writes what it observed on a *successful* match, so the gate and the read are one read (asserting then re-reading with a getter is two reads of a page that may have changed — TOCTOU).

```go
var blockID string
Eventually(".figure-frame").Should(b.HaveAttribute("data-block-id", Not(BeEmpty())).Capture(&blockID))
```

- **Typed decode:** any non-nil pointer, decoded à la `encoding/json` — a JS number lands in an `*int` as `3`, not `float64(3)`; a JS object lands in your struct. An impossible mismatch (string into `*int`) fails **immediately** instead of waiting out the timeout.
- **Absent vs. zero:** a JS `null`/`undefined` decodes to the Go zero value, so a plain `*string` target can't tell "absent" apart from "present but empty." Decode into a `**T` instead — absent leaves it `nil`, present allocates: `var key *string; b.GetAttribute(".mark", b.AllowMissing("data-key"), &key)`. Re-evaluated on every observation, so a poll watching an absent→present transition stays honest. Matters most under `AllowMissing` (below), whose whole point is making absence a value rather than a timeout.
- **Only on a match:** `ShouldNot`/`NotTo` captures nothing. While polling the target is overwritten each successful attempt; it holds the value from the attempt that passed.
- **Returns a NEW matcher, leaves the receiver alone** — `m.Capture(&idA)` then `m.Capture(&idB)` is safe. `HaveCookie`'s `.Capture` copies too and composes with `.With*` in either order.
- **Narrowing works for JS objects, not Biloba structs.** A struct naming a subset of a JS object's fields is fine (`HaveJSONAttribute`). `Box`/`ScrollOffset`/`BoxDelta`/`Cookie` into a *different* struct is rejected rather than half-filled — capture into the matching type or an `any`.
- **Capturable:** `HaveAttribute`, `HaveProperty` (both forms incl. existence-only), `HaveValue`, `HaveInnerText`, `HaveTextContent`, `HaveText`, `HaveClass`, `HaveCount`, `HaveDistinctCount`, `HaveJSONAttribute`, `HaveComputedStyle`, `HaveComputedStyleNumeric`, `EachHaveInnerText`, `EachHaveTextContent`, `EachHaveClass`, `EachHaveProperty` (both forms — the existence-only form reads values in the same round-trip, so no-arg `EachHaveInnerText()`/`EachHaveTextContent()` capture too), `HaveBoundingBox`, `HaveScrollOffset`, `HaveOffsetTopWithin`/`HaveOffsetLeftWithin`, `HaveGapBetween`, `EvaluateTo`, `HaveURL`, `HaveTitle`, `HaveLocalStorageItem`/`HaveSessionStorageItem`, `HaveNumLocalStorageItems`/`HaveNumSessionStorageItems`, `HaveNumCookies`, `HaveCookie`.
- **Not capturable (won't compile):** value-less matchers — `Exist`, `BeVisible`, `BeEnabled`, `BeClickable`, `BeFocused`, `BeChecked`, `BeInViewport`, `BeAbove`/`BeBelow`/`BeLeftOf`/`BeRightOf`/`Encloses`/`Overlaps`/`BePrecededBy`/`BeFollowedBy`, `BeNetworkIdle`, `HaveScreenshot` — and the actions-as-matchers (`Click`, `SetValue`, `Type`, …). Two read something but stay bare on purpose: **`MatchColor`** (a sub-matcher — capture off the `HaveComputedStyle` you hand it to) and **`HaveMadeRequest`** (a `*RequestQuery` builder; it renders observed requests in its failure message, and the request object comes from `b.AllRequests().Find(b.RequestMatching(...))`).
- **Getter counterpart:** the `any`-returning getters take an optional trailing pointer with the same decode rules — `b.GetProperty("#row", "offsetWidth", &n)`, `b.GetAttribute`, `b.GetValue`, `b.CurrentPropertyForEach`, `b.CurrentAttributeForEach`, `b.CurrentValueForEach`. The value is still returned.

## Navigation

- `b.Navigate(url)` — navigate, assert `200`. / `b.NavigateWithStatus(url, code)`.
- `b.GetLocation()` / `b.GetTitle()` → string — **polling getters**, all four knobs. A transient CDP error mid-navigation (`Inspected target navigated or closed (-32000)`) is a retryable miss. (**Breaking rename** — bare `Location`/`Title` are gone.)
- `b.HaveURL(string|matcher)` / `b.HaveTitle(string|matcher)` — same transient-error behavior.
- **Gate on a DOM anchor, not the URL** — `Eventually("#dashboard-root").Should(b.Exist())` proves the navigation landed; a `GetLocation()` read is then a formality.

## Cookies & storage  (navigate to a real origin first)

- `b.SetCookie(...Cookie)` (default domain = current URL) · `b.GetCookies()` → `Cookies` · `b.ClearCookies()`.
- `b.HaveCookie(name|matcher)` — chain `.WithValue/.WithPath/.WithDomain/.WithSameSite/.WithSecure(...)/.WithHTTPOnly(...)`.
- `b.CookieMatching(...)` — predicate for `Cookies.Find/Filter`. · `b.HaveNumCookies(int|matcher)`.
- `b.LocalStorage()` / `b.SessionStorage()` → `Set(k,v)`, `Get(k,&ptr)`, `GetAll()`, `Remove(k)`, `Clear()`, `Length()` (JSON round-tripped).
- `b.HaveLocalStorageItem(key[, value])` / `HaveSessionStorageItem` / `HaveNumLocalStorageItems` / `HaveNumSessionStorageItems`.

## Tabs

- `b.NewTab()` → `*Biloba` — isolated tab (own context); closed by `Prepare()`.
- `tab.Close()` → error (`Eventually(tab.Close).Should(Succeed())` during downloads).
- `b.AllTabs()` / `b.AllSpawnedTabs()` → `Tabs`.
- `b.HaveTab()` / `b.HaveSpawnedTab()` — chain `.WithURL/.WithTitle/.WithDOMElement(selector)`.
- `b.TabMatching()` — predicate for `Tabs.Find/Filter`.

## Existence, count, visibility, enabled

- `b.HasElement(selector)` → bool (first; snapshot) · `b.Exist()`.
- `b.Count(selector)` → int (snapshot) · `b.HaveCount(int|matcher)`.
- `b.HaveDistinctCount(attribute, int|matcher)` — count of **distinct** values `attribute` takes across matches (dedupes double-painted nodes by a stable `data-*` key).
- `b.BeVisible()` — non-zero `offsetWidth`/`offsetHeight`. · `b.BeEnabled()` — `!el.disabled`. · `b.BeClickable()` — visible + enabled + topmost at its center (deterministic occlusion guard; `Click` does **not** run it). · `b.EachBeVisible()` / `b.EachBeEnabled()`.
- **Every `Each*` matcher means "≥1 match AND all satisfy"** — it fails on zero matches instead of passing vacuously. Assert nothing matches with `HaveCount(0)` / `ShouldNot(b.Exist())`.

## Contents, classes, attributes, state

- `b.GetInnerText(selector)` → string (polls on presence — `""` is a valid value) · `b.HaveInnerText(string|matcher)` (exact).
- `b.GetTextContent(selector)` → string · `b.HaveTextContent(string|matcher)`.
- `b.HaveText(string|matcher)` — trims & collapses whitespace first.
- `b.CurrentInnerTextForEach(selector)` → []string · `b.EachHaveInnerText(value|matcher)`. Same pair for `CurrentTextContentForEach`/`EachHaveTextContent`. The **no-arg** `EachHaveInnerText()`/`EachHaveTextContent()` no longer mean "every text is empty" — they assert the property is *defined* on every match and capture the slice. Sub-matcher and `.Capture` both see a typed `[]string` (the same slice the `Current*ForEach` getter returns), so `Equal([]string{...})` and `HaveExactElements(...)` both work — contrast the generic `EachHaveProperty` below, which hands over raw `[]any`.
- `b.HaveClass(string|matcher)` — a string ⇒ "list contains"; a matcher receives `[]string`. · `b.EachHaveClass(string)`.
- `b.HaveAttribute(name[, string|matcher])` — via `getAttribute`.
- `b.HaveComputedStyle(prop, string|matcher)` — via `getComputedStyle`; getter `b.GetComputedStyle` (see Geometry).
- `b.BeChecked()`. **An element with no `checked` property at all is an error, not "unchecked"** — selecting the wrapping `<label>`/`<div>` fails loudly instead of letting `ShouldNot(b.BeChecked())` pass forever.
- `b.BeFocused()` — is `document.activeElement`.

## Properties  (`.` paths like `dataset.name`; JS types preserved — numbers are `float64`)

**Two-axis polling:** the singular `Get*` getters poll until the element is present **AND** every named property/attribute is *defined*.

- **Trap:** a property that doesn't exist on that element type (`disabled` on a `<div>`) blocks the poll to timeout. Wrap it — `b.GetProperty("div.card", b.AllowMissing("disabled"))` → `nil`, no wait. Name params take `string` or `AllowMissing`; `AllowMissing` applies to `GetProperty`/`GetProperties`/`GetAttribute`/`GetAttributes` only.
- `b.GetProperty(selector, name[, &ptr])` → any · `b.SetProperty(selector, name, value)` (dual) · `b.HaveProperty(name[, value|matcher])` (matcher).
- `b.GetProperties(selector, ...names)` → `Properties`; `GetString/GetInt/GetFloat64/GetBool/GetStringSlice`.
- `b.GetAttribute(selector, name[, &ptr])` → any (raw `getAttribute` markup, not the resolved property) · `b.GetAttributes(selector, ...names)` → `Properties`.
- `b.GetJSONAttribute(selector, attribute, &out)` (polls until present, set, **and** JSON-parseable, then decodes into `*struct`/`*map`/`*any`) · `b.HaveJSONAttribute(attribute, matcher)` — hands the sub-matcher the decoded value (`map[string]any`/`[]any`/`float64`); composes with `gstruct`.
- **Snapshot plural getters — no poll, `nil` for absent, gate presence first with `Eventually(sel).Should(b.HaveCount(n))`:** `b.CurrentPropertyForEach(selector, name[, &ptr])` → []any · `b.CurrentPropertiesForEach(selector, ...names)` → `SliceOfProperties` (`.Get(key)`, `.Find(key, val|matcher)`, `.Filter(key, val|matcher)`) · `b.CurrentAttributeForEach` · `b.CurrentAttributesForEach`.
  - **Trap:** they return an **empty slice** when nothing matches, which satisfies a *negated* collection assertion (`Ω(b.CurrentInnerTextForEach(".row")).ShouldNot(ContainElement("Draft"))` passes against zero rows). The `HaveCount(n)` gate keeps the negative honest. Positive assertions are safer — Gomega's `HaveEach` errors on an empty slice.
- `b.SetPropertyForEachImmediately(selector, name, value)` — all matches now, no poll. · `b.EachHaveProperty(name[, ...])` — sub-matcher and `.Capture` see the raw JSON-decoded `[]any` (properties are heterogeneous, nothing honest to convert to); `Equal([]string{...})` always fails here — use `HaveExactElements(...)`.

## Form values  (rationalizes text/checkbox/radio/multi-select)

- `b.GetValue(selector[, &ptr])` → any (polls on presence only — `""`/unselected radio is a valid value; bool for checkbox, checked radio's `value`, `[]string` for multi-select) · `b.CurrentValueForEach(selector[, &ptr])` → []any.
- `b.SetValue(selector, value)` (dual) — requires visible+enabled; focuses, sets, fires `input`+`change`. Does **not** type real keys (use `b.Type`). For a `<select>` the value matches the **option `value`**, not its visible label.
- `b.ValueLabel(label)` — wrap a `SetValue` arg to target a `<select>` option by **visible label**: `b.SetValue(sel, b.ValueLabel("Sonnet"))`. Multi-select: pass a slice (labels and raw values may mix). `<select>` only.
- `b.HaveValue(value|matcher)`.

## Geometry  (pollable layout reads — use instead of hand-rolled `b.Run` geometry)

Getters poll until the element is present **AND** laid out (non-degenerate box). All values are viewport-relative CSS pixels. Each getter has a `Have*` matcher counterpart.

**A missing element is an error, not a non-match.** So `Eventually("#toast").ShouldNot(b.BeInViewport())` **times out** — Gomega never counts an erroring matcher as satisfied, in either direction. **Assert disappearance with `ShouldNot(b.Exist())` or `Should(b.HaveCount(0))`**; keep geometry matchers for elements that *are* there. Applies to `HaveBoundingBox`/`HaveScrollOffset`/`HaveOffsetTopWithin`/`HaveOffsetLeftWithin`/`HaveGapBetween`, the pairwise matchers, and any `Consistently(sel).ShouldNot(...)` on a conditionally-rendered `sel`. Under `Eventually` an error is still just a failed attempt, and a present-but-zero-area box is a silent retry, so positive assertions poll through late layout. (Matches `BeVisible`/`BeEnabled`/`BeClickable`, which always errored on a missing element. The pairwise/offset probes name *which* selector went missing: subject, container, or other.)

- `b.GetBoundingBox(selector)` → `Box{Top,Left,Width,Height,Bottom,Right,CenterX,CenterY,ClientWidth,ClientHeight}` · `b.HaveBoundingBox(matcher)` (receives the `Box`; compose with `HaveField`). `Width`/`Height` are **border-box** (incl. scrollbar gutter, like `getBoundingClientRect`); `ClientWidth`/`ClientHeight` are the **client box** (content+padding, scrollbar excluded) — "content width of this scroll container".
- `b.GetScrollOffset(selector)` → `ScrollOffset{Top,Left,MaxTop,MaxLeft}` (`Top==MaxTop` ⇒ scrolled to bottom) · `b.HaveScrollOffset(matcher)`.
- `b.GetOffsetTopWithin(selector, container)` → float64 (`element.top - container.top`) · `b.GetOffsetLeftWithin` · `b.HaveOffsetTopWithin(container, value|matcher)` · `b.HaveOffsetLeftWithin(...)`.
- **Pairwise** (both boxes read in one atomic frame — don't split into two `GetBoundingBox`es): `b.BeAbove(other)` (`subject.Bottom<=other.Top`), `b.BeBelow`, `b.BeLeftOf` (`subject.Right<=other.Left`), `b.BeRightOf`, `b.Encloses` (all 4 edges), `b.Overlaps`. All matchers: `Eventually(subject).Should(b.BeAbove(other))`.
- `b.GetGapBetween(selector, other)` → `BoxDelta{Top,Left,Bottom,Right,Width,Height,CenterX,CenterY}` (subject minus other; `CenterX~0` ⇒ shared center line, `Width~0` ⇒ same size) · `b.HaveGapBetween(other, value|matcher)`.
- `b.BeInViewport(options...)` — laid out **and** intersecting the visible layout viewport (≠ `BeVisible`, which is only "rendered"). Partial overlap counts; `b.Fully()` requires all 4 edges on screen.
- `b.BePrecededBy(other)` / `b.BeFollowedBy(other)` — document order via `compareDocumentPosition`. **Read the subject first:** `Should(b.BeFollowedBy(Y))` ⇔ X comes **BEFORE** Y; `Should(b.BePrecededBy(Y))` ⇔ X comes **AFTER** Y. A backwards assertion doesn't flake — it silently passes on a fixture satisfying the inverse, so pin both sides (`Ω(X).ShouldNot(b.BePrecededBy(Y))`). Failure messages report the observed order (`Actually: #note comes BEFORE #section.`). "Anywhere after" includes *inside* — scope with `Locator.NotWithin`.
- `b.GetComputedStyle(selector, property)` → string (`getPropertyValue`, so kebab-case names and custom properties like `--stage` resolve).
- `b.GetComputedStyleNumeric(selector, property)` → float64 (leading number via `parseFloat`: `"16px"`→`16`; non-numeric fails) · `b.HaveComputedStyleNumeric(property, number|matcher)` — a plain number compares with `BeNumerically`.
- `b.NormalizeColor(color)` → string (**pure transform**: no selector, no poll) — any CSS `<color>` incl. a `var(--token)` chain (resolved against `:root`) → canonical `rgb()`/`rgba()`. · `b.MatchColor(color)` — normalizes **both** sides; pass as the expected: `b.HaveComputedStyle("stroke", b.MatchColor("var(--tok-teal)"))`.

## Clicking & interactions  (pragmatic simulations)

- `b.Click(selector)` (dual) — visible+enabled, then `el.click()`.
- `b.DblClick` (dual) — two clicks + `dblclick`. · `b.RightClick` — `mousedown`/`mouseup`/`contextmenu`. · `b.MiddleClick` — `mousedown`/`mouseup`/`auxclick`.
- `b.Tap(selector)` (dual) — synthetic touch/pointer + `click` (realistic: real CDP `touchStart`/`touchEnd`); accepts `b.At(...)`, ignores modifiers.
- **Pointer options** — `b.At(x,y)` (offset from top-left; canvas/map/slider) and `b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` (⌘/Win) — accepted by `Click`/`DblClick`/`RightClick`/`MiddleClick`/`Tap`, after the selector or in place of it (matcher form). They compose: `b.Click(sel, b.At(30,40), b.Shift())`. In fast mode any option switches the click off native `el.click()` to a synthetic event carrying coords+flags.
- `b.DragTo(source, target)` (dual) — pointer-based drag (`pointerdown`/`move`/`up`); drives @dnd-kit-style DnD, **not** native HTML5 `draggable`. Matcher subject is the source.
- `b.ScrollWheel(selector, deltaX, deltaY)` (dual; matcher form drops the selector) — `wheel` event then scrolls the nearest scrollable ancestor; +deltaY=down, +deltaX=right.
- `b.ClickEachImmediately(selector)` — click all visible+enabled matches now, no poll (gate presence first).
- `b.Focus` / `b.Blur` / `b.Hover` (all dual; `Hover` fires pointer/mouse events, **not** CSS `:hover`).
- `b.ScrollIntoView(selector, ...ScrollOption)` (dual) — bare = native `scrollIntoView()`. Options: `b.WithinScroller(container)` (a specific container, not the nearest ancestor), `b.AtTopOffset(px)` (land `px` below the container top — "clear the sticky header"). Instant, occlusion-*un*aware.
- **Fast clicks act in place** — no `scrollIntoView`, no focus move. Scroll-into-view comes only from `Realistic()` and from focus-bearing ops (`Focus`/`SetValue`/`Type`).

### Selecting text

Each produces a genuine `window.getSelection()` range and dispatches `mouseup` (drives highlight→menu/annotation UIs).

- `b.SelectText(selector)` (dual) — all of the element's text.
- `b.SelectText(selector, substring[, b.Occurrence(n)])` (dual) — a **substring**; first occurrence by default, `b.Occurrence(n)` is 1-based. The **matcher form requires an explicit `b.Occurrence(n)`** so it can't be confused with select-all: `Eventually("#passage").Should(b.SelectText("fox", b.Occurrence(2)))`.
- `b.SelectRange(selector, start, end)` (dual; matcher form drops the selector) — chars `[start, end)` across the element's text nodes; fails the spec if out of bounds.
- `b.ClearSelection()` — no matcher form.
- Read back with `Eventually("window.getSelection().toString()").Should(b.EvaluateTo(…))`.

## Realistic mode → `biloba:realistic-mode`

`b.Realistic()` → `*Biloba` — a view of the **same tab** routed through real CDP input: scrolls into view, waits for stability, refuses to click through an occluding overlay, moves the real pointer (CSS `:hover` activates), dispatches genuine mouse/touch/key input. The whole interaction vocabulary works on it; it composes with the poll-config clones (`b.Realistic().WithTimeout(d).Click(sel)`). No per-call decorator. Per-spec opt-in — real round-trips, can reintroduce flake.

## Keyboard  (real key events, via chromedp)

- `b.Type(...)` (dual) — **the** element-targeted keyboard method: focuses (which scrolls into view), then genuine keystrokes (text **and** named `Keys.*`); **appends**. Arg disambiguation (after stripping modifiers):
  - `b.Type(selector, payload...)` — polling form: selector (CSS string, `XPath`, or `Locator`) + ≥1 payload arg. `b.Type("input", "hello", biloba.Keys.Enter)`, `b.Type(b.ByLabel("Email"), "jane@example.com")`.
  - `b.Type(payload)` — matcher form: a single string, or one-or-more `Keys.*`. `Eventually("#in").Should(b.Type(biloba.Keys.Enter))`.
  - The matcher form can't mix leading text + trailing keys (`b.Type("hello", Keys.Enter)` reads as selector=`"hello"`). Use the polling form.
- `b.SendKeysToWindowImmediately(...parts)` — **focus-free, no selector, no matcher, no poll**: lands on the focused element, else fires on `document`/window (global hotkeys). Gate it yourself: `Eventually(sel).Should(b.BeFocused())` then send. To type *into* an element use `b.Type`.
- `biloba.Keys.{Enter,Tab,Escape,Backspace,Delete,Arrow{Up,Down,Left,Right},Home,End,PageUp,PageDown}`.
- **Modifiers** `b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` (same values as the pointer modifiers) work here too, in any position: `b.Type("textarea", biloba.Keys.Enter, b.Shift())`.

## Uploads

- `b.SetUpload(selector, ...paths)` (dual) — set `<input type=file>` files via CDP (paths must exist on Chrome's machine); fires `change`. Matcher form: `b.SetUpload(path)` or, for multiple files, a single `b.SetUpload([]string{...})` (bare variadic paths would be ambiguous with the immediate form).

## Run JS on selected elements

- `b.InvokeOn(selector, method, ...args)` → any — `el[method](...args)`. · `b.InvokeOnEachImmediately(...)` → []any.
- `b.InvokeWith(selector, jsFn, ...args)` → any — `jsFn(el, ...args)`. · `b.InvokeWithEachImmediately(...)` → []any.

## Dialogs  (per-tab; reset by `Prepare`; register handlers BEFORE the triggering action)

- `b.HandleAlertDialogs()` / `HandleConfirmDialogs()` / `HandlePromptDialogs()` / `HandleBeforeunloadDialogs()` → `DialogHandler`; chain `.MatchingMessage(string|matcher)`, `.WithResponse(bool)`, `.WithText(s)`. · `b.RemoveDialogHandler(h)`.
- `b.Dialogs()` → `Dialogs`, filtered with `.OfType(biloba.DialogTypeAlert|DialogTypeConfirm|DialogTypePrompt|DialogTypeBeforeunload)`, `.MatchingMessage(string|matcher)`, `.MostRecent()`. There is **no** dialog matcher — assert with plain Gomega: `Ω(b.Dialogs().MostRecent()).Should(HaveField("Message", "…"))`.
- `Dialog{Type, Message, DefaultPrompt, HandleResponse, HandleText, Autohandled}` (`Autohandled` = no registered handler matched, so a Biloba default ran).
- Default handling: alerts accepted; confirm/prompt cancelled; beforeunload accepted.

## Downloads  (per-tab; auto-tracked)

- `b.AllDownloads()` / `b.AllCompleteDownloads()` → `Downloads`.
- `b.HaveDownloaded([filename])` — chain `.WithURL(...)`, `.WithContent([]byte|matcher)`; complete downloads only.
- `b.DownloadMatching(...)` — predicate for `Downloads.Find`.
- `Download`: `.URL`, `.Filename`, `.IsComplete()/.IsCancelled()/.IsActive()`, `.Content()` → []byte.

## Arbitrary JS  (runs on the global `window`; wrap object literals in parens)

- `b.Run(script[, &ptr])` → any — a synchronous **expression**; returns the decoded value. A top-level `return` is illegal (errors with a hint pointing at `RunAsync`/IIFE). Pollable: `Eventually(b.Run).WithArguments(expr).Should(matcher)` — it's a `func(string,...any) any`. → `biloba:flaky-specs`
- `b.RunAsync(script[, &ptr])` — the body of an async fn; you `return` the awaited value (use for `await`/`fetch`).
- `b.RunErr(script, ...args)` / `b.RunErrAsync(...)` → `(any, error)` — error-returning siblings; handle the error yourself instead of failing the spec.
- `b.EvaluateTo(value|matcher)` — assert a JS expression's result. Numbers decode to `float64` — `BeNumerically`, not `Equal(intLiteral)`. **Asymmetry:** the sub-matcher sees the **raw JSON-decoded** value (`[]any` of `map[string]any` — `HaveKeyWithValue`, not `HaveField`); `.Capture(&typed)` hands you the value decoded into your struct.
- `b.JSFunc(script)` → `.Invoke(...args)` string — JSON-encodes args into an invocable snippet. · `b.JSVar(nameOrExpr)` — reference a JS variable/expression as a `JSFunc` argument (don't quote it).
- `b.GetJSValue(expression[, &ptr])` → any — **polls** until `expression` is *defined*, then returns it. The **app-state barrier**: point it at a global the app writes (`window.__storeLog`) to prove the *browser* processed something. Retries through `undefined` **and** a thrown error (a `ReferenceError` for a not-yet-created global is "not ready"); `null` is a legitimate value and returns immediately. The pointer decodes into a concrete type (dodges the `float64` gotcha).
  - **It gates on definedness and nothing else** — it only barriers on a path created **lazily by the event you're waiting for** (`window.__log ??= []` inside the subscriber). Against an **eagerly**-created log it returns `[]` on the first tick and gates nothing.
  - **Wrong wherever absence is meaningful** (a ledger absent on `about:blank` means *quiet*; `window.__renderErrors` absent means *no errors*; a flag that must have *survived*; a pre-action baseline). Those stay `b.Run` with a coalesce (`window.__x ?? null`).
  - For a *condition* rather than a value: `Eventually(expr).Should(b.EvaluateTo(matcher).Capture(&typed))`. → `biloba:flaky-specs` §3

## Network  (per-tab; reset by `Prepare`)

- `b.StubRequest(url string|matcher, biloba.StubResponse{Status,Body,Headers})` — the first handler enables interception; unmatched requests pass through.
- `b.AbortRequest(url string|matcher)` — fail matching requests (the page's fetch rejects).
- `b.ModifyRequest(url string|matcher)` → `.WithURL(u).WithMethod(m).WithHeader(n,v).WithBody(b)` — continue to the real network, overriding only what you set.
- `b.ModifyResponse(url string|matcher)` → `.WithStatus(s).WithHeader(n,v).WithBody(b)` or `.Using(func(biloba.InterceptedResponse) biloba.StubResponse)` — rewrite the real response (reads real status/headers/body; heavier — pauses twice).
- **Every registration returns a handle whose `Count()` reports how many dispatches *that handler* claimed** — `*RequestStub`/`*RequestAbort`/`*RequestModification`/`*ResponseModification` (plus `*ResponseHold` below). A snapshot, safe to poll: `Eventually(stub.Count).Should(Equal(1))`. **Assert your handler fired** — a typo'd URL otherwise matches nothing, goes straight to the real network, and the spec passes for the wrong reason. `Count` is a fact about *that handler*, not the URL: first-match-wins means a handler shadowed by an earlier one stays at 0.
- **All handlers share one ordered, first-match-wins list.** In an `Ordered` container `Prepare()` is `OncePerOrdered`, so handlers **accumulate** across the `It`s: an earlier spec's handler permanently claims that URL and a later identical one is silent dead code. → `biloba:flaky-specs` §6
- While interception is on Biloba **disables the HTTP cache** (`Network.setCacheDisabled(true)`, restored by `Prepare()`) — a cached response raises no Fetch event and would skip every handler.
- `b.HaveMadeRequest(url string|matcher)` — chain `.WithMethod(m)`.
- `b.AllRequests()` → `Requests` (`*Request` has `.URL/.Method/.Headers/.ResourceType`); `b.RequestMatching(...)` predicate for `.Find/.Filter`.
- `b.BeNetworkIdle()` — zero in-flight requests at the instant it's polled, no quiet period. Tracks **HTTP** only (`Network.requestWillBeSent`/`loadingFinished`); a long-lived **WebSocket** does not keep it busy. **It passes before your request has started** — anchor with `Eventually(b).Should(b.HaveMadeRequest(...))` first, *then* wait for idle.

### `b.HoldResponse(url string|matcher)` → `*ResponseHold`

Intercepts the **real** response and holds it in flight until you release it (then it passes through unchanged). Builds on `ModifyResponse` — same tab scoping, same first-match-wins list. The tool for forcing arrival order in optimistic-UI reconciliation → `biloba:flaky-specs`.

- **A hold freezes EVERY matching response by default**, not just the first. That's right for "nothing lands until I say so"; use `Limit` for "hold #1 while #2 flies past".
- `hold.Await()` → `InterceptedResponse` — blocks until a match is actually held, then returns the **oldest one still held** (immediately if one already arrived). Responses that passed through at `Limit` were never held, so `Await` skips them; after a bare `Release()` it returns the first response the hold intercepted rather than blocking. Waiting command: own 30s default, honors `WithTimeout`/`WithContext` set on **the tab you build the hold from** (`b.WithTimeout(d).HoldResponse(u)`); `WithPolling`/`Immediate` are a hard error.
- `hold.Limit(n)` → `*ResponseHold` — hold at most `n` concurrently; while `n` are held, further matches **pass straight through untouched** (they still count). Releasing frees capacity. `n ≥ 1`; unlimited by default. The limit is consulted **only as each response arrives** — set it when you build the hold (lowering it later releases nothing; raising it retro-holds nothing).
- `hold.Release()` — **terminal**: everything held goes through AND the hold stops holding future matches. Idempotent.
- `hold.Release(r)` — release just the response `Await()` gave you; the hold **stays armed**. Releasing something this hold isn't holding fails the spec (matched by value, oldest first).
- `hold.ReleaseNext()` — release the oldest still-held response and stay armed (`Await(); ReleaseNext(); Await()` steps through them). **Fails the spec when nothing is held.**
- `hold.Count()` → int — **every** match intercepted so far, held or passed through. A snapshot (no knobs) but safe to poll: `Eventually(hold.Count).Should(Equal(1))`. Assert it — it's the only proof the interception you think happened actually did.
- `hold.Held()` / `hold.PassedThrough()` → int — split `Count()`: `Held` is how many this hold actually froze (cumulative, including ones since released); `PassedThrough` is how many arrived and were never frozen (at `Limit`, or after a bare `Release()`). `Held()+PassedThrough()==Count()` always; both snapshots, safe to poll. With `Limit(1)` the fixture under test is usually "response #2 was NOT held" — assert `Eventually(hold.PassedThrough).Should(Equal(1))` directly rather than `Count()==2`, which only implies it by inference from the limit (a regression that raises the limit keeps `Count` green while destroying the ordering under test).
- **`Count`/`Release` are facts about the network, not the page** — `Count` says a response reached the tab's interceptor, `Release` returns when the release is signalled; neither says the renderer did anything with it. When the assertion is about what the *app* did with the response, pair them with an app-state barrier (`b.GetJSValue`, or the DOM the response produces), not a sleep.
- Held responses are **force-released at spec end and by `Prepare()`**.
- **Sharp edge:** matching is **tab-wide and URL-based**, so a hold can catch a response from an *earlier* page load. Scope to a dedicated `b.NewTab()`, or assert `Count()`. A **second `HoldResponse` for a URL an earlier one already claims is dead code** — re-arm the hold you have with `ReleaseNext`.

## Screenshots, outline, window → `biloba:debug-failures`

- `b.Outline()` → string (indented DOM) · `b.A11yOutline()` → string (accessibility tree: role + name).
- `b.CaptureScreenshot()` → []byte (PNG) · `b.CaptureImgcatScreenshot()` → string · `b.CaptureScreenshotToFile(path)` → abs path.
- `b.CaptureScreenshotOf(selector)` · `b.CaptureImgcatScreenshotOf(selector)` · `b.CaptureScreenshotOfToFile(selector, path)` — clipped to the first match (any selector; works below the fold and across `>>>`).
- `b.Artifacts()` → `[]biloba.Artifact` (`Kind`/`Path`/`Label`) — the files Biloba wrote this spec: failure screenshots, visual `.actual`/`.diff`/baseline PNGs, `CaptureScreenshotToFile` output. Snapshot (rejects all knobs); cleared by `Prepare()`. Failure screenshots are written by a cleanup, so read from a `ReportAfterEach`, **not** an `AfterEach`.
- `b.SetWindowSize(w, h, ...opt)` · `b.WindowSize()`. `SetWindowSize` registers its own `DeferCleanup` to restore the prior size — don't restore manually, and don't call it from inside another `DeferCleanup` (Ginkgo forbids nesting). Call it bare in `BeforeEach`/`BeforeAll`.

## Visual regression → `biloba:visual-assertions`

- `b.HaveScreenshot(name string, ...ScreenshotOption)` — **bare matcher**; compares the subject against the committed baseline `<name>.png`. Subject is a selector (element capture, clipped to its box) or the tab `b` (whole page). Every poll attempt captures and compares afresh, so the poll waits out fonts/`ResizeObserver`/rAF. `name` may contain `/` for subdirectories. Configure the `Eventually` (a knob on the matcher is a hard error); Gomega's 1s default is usually too short.
  ```go
  Eventually(".card").WithTimeout(10*time.Second).Should(b.HaveScreenshot("card"))
  Eventually(b).Should(b.HaveScreenshot("home", b.InColorSchemes("light", "dark")))
  ```
- **Options** (`ScreenshotOption`): `b.Mask(selectors...)` paints matches flat gray on both sides of the comparison (no-op if nothing matches) · `b.Tolerance(fraction)` at most `fraction` (0..1) of pixels may differ · `b.ChannelTolerance(delta)` a pixel only counts as differing when an R/G/B/A channel is off by more than `delta` (the antialiasing absorber) · `b.Animated()` opts out of the automatic animation/transition/caret/smooth-scroll freeze (which reaches open shadow roots, not closed ones) · `b.InColorSchemes(schemes...)` compares once per emulated `prefers-color-scheme`, all must match, baseline per scheme `<name>-<scheme>.png`. Both tolerances default to `0` (exact).
- **Baselines are committed** (`./biloba-baselines`, `BilobaConfigScreenshotBaselinesDir` / `BILOBA_SCREENSHOT_BASELINES_DIR`); the `.actual.png`/`.diff.png` a failure writes are **gitignored**, landing in the failure-screenshots dir. Suite-wide tolerance: `BilobaConfigScreenshotTolerance(fraction)` / `BilobaConfigScreenshotChannelTolerance(delta)`.
- **A missing baseline fails** (never write-and-pass) and says to re-run with `BILOBA_UPDATE_SCREENSHOTS=1`, which writes baselines and reports in words what changed. Update mode captures until three in a row match before writing (so a write takes ~0.5–2.2s) and warns if the page never settles. The var takes `1/t/true/y/yes/on`; an unrecognised value warns. A failed comparison prints a text diagnosis (pixel counts, the changed-region shape, the untouched side) plus the three paths.

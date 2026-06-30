---
name: api
description: One-line reference for every Biloba method and matcher, grouped by area — selectors/locators, lifecycle, poll-config (WithTimeout/WithPolling/WithContext/Immediate), navigation, cookies/storage, tabs, DOM existence/visibility/contents/properties/forms, clicking and interactions (incl. drag/scroll/tap/modifiers/text-selection), realistic mode, keyboard, uploads, element JS, dialogs, downloads, arbitrary JS, network stubbing/aborting/modifying/observing, and screenshots/outline/window. Use to look up the exact method or matcher name and shape. Methods marked (dual) poll until they succeed when fully applied and return a pollable matcher when under-applied.
---

# Biloba API reference

Terse lookup. **(dual)** = **polls until it succeeds** when fully applied (`b.Click("#go")` waits until the element is clickable, acts once, then stops), returns a Gomega matcher when under-applied (poll it yourself with `Eventually`). **(matcher)** = always returns a matcher. **first** = acts on the first match; **each** = acts on all matches. Selectors are CSS strings, `XPath` (see `biloba:xpath`), or semantic **`Locator`**s. Full docs: <https://onsi.github.io/biloba/>.

**Poll-by-default.** A fully-applied action/getter polls (the flake-resistant default — see `biloba:flaky-specs`). `b.Immediate()` opts back into act-once/fail-fast (rarely needed). The under-applied matcher form is for when you want to drive the `Eventually`/`Consistently` yourself.

## Selectors / locators
Three pathways, all flow through every method/matcher. **CSS is the default** (target stable `#id`/`[data-testid]` hooks, not styling classes); **locators** second (a11y assertions + readable text/label identifiers); **XPath** the rare power tool (axis/ordinal). CSS fastest, XPath fast, locators slowest (full-document ARIA scan).
- **CSS string** (`"#id"`, `".cls"`, `:has()`); `>>>` pierces open shadow roots / same-origin iframes (one boundary per `>>>`). **XPath** via `b.XPath(...)` (see `biloba:xpath`) — does **not** pierce shadow/iframe.
- **Locator constructors** (all have a `*Contains` variant where text-valued): `b.ByRole(role)`; `b.ByText(t)`/`b.ByTextContains(t)`; `b.ByLabel(t)`/`b.ByLabelContains(t)`; `b.ByPlaceholder(t)`; `b.ByAltText(t)`; `b.ByTitle(t)`; `b.ByTestID(id)` (attr = `biloba.TestIDAttribute`, default `"data-testid"`).
- **Role refinements**: `.WithName(n)`/`.WithNameContains(n)` (accessible name); `.Level(n)` (heading level); ARIA-state filters `.Checked()`/`.Disabled()`/`.Expanded()`/`.Pressed()`/`.Selected()`.
- **Composition** (all accept any CSS/XPath/Locator): `.ContainingText(t)`/`.NotContainingText(t)`; `.Containing(sel)`/`.NotContaining(sel)`; `.And(sel)`/`.Or(sel)` (intersection/union); `.Within(scope)`; `.Nth(i)`/`.First()`/`.Last()`. Example: `b.ByRole("listitem").Containing(b.ByText("Delete")).Within("#cart").First()`.
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
  | **Polling** | dual actions, value-getters | ✓ | ✓ | ✓ |
  | **Waiting command** | `Navigate`, `Capture*Screenshot*` (own ~30s/~5s defaults) | ✓ (overrides own default) | error | error |
  | **Snapshot** | `HasElement`/`Count`/`Current*ForEach`/`Title`/… | error | error | error |
  | **One-shot mutation** | `SetCookie`/`StubRequest`/`*Immediately`/`Run`/`RunAsync`/… | error | error | error |

  Configuring a call that resolves to a **bare matcher** (a `(matcher)` method, or the under-applied form of a dual method like `b.WithTimeout(d).Click()`) is also a hard error — configure the `Eventually`, not the matcher.

## Navigation
- `b.Navigate(url)` — navigate, assert `200`.
- `b.NavigateWithStatus(url, code)` — navigate, assert a specific status.
- `b.Location()` / `b.Title()` — current URL / title (**immediate snapshot** — no poll, rejects every config knob; drive your own poll via `Eventually(b.Title)` or the `HaveURL`/`HaveTitle` matchers below).
- `b.HaveURL(string|matcher)` (matcher) — assert tab URL.
- `b.HaveTitle(string|matcher)` (matcher) — assert tab title.

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
- `b.BeVisible()` (matcher) — non-zero `offsetWidth`/`offsetHeight`. / `b.EachBeVisible()` (matcher) — **≥1 match AND all visible** (fails on zero matches).
- `b.BeEnabled()` (matcher) — `!el.disabled`. / `b.EachBeEnabled()` (matcher) — **≥1 match AND all enabled** (fails on zero matches).
- `b.BeClickable()` (matcher) — visible + enabled + topmost at its center (deterministic occlusion guard; opt-in, `Click` does **not** run it).

## Contents, classes, attributes, state
- `b.GetInnerText(selector)` → string (first; **polls** until the element is present — empty string is a valid value) / `b.HaveInnerText(string|matcher)` (matcher, exact).
- `b.GetTextContent(selector)` → string (first; polls until present) / `b.HaveTextContent(string|matcher)` (matcher).
- `b.HaveText(string|matcher)` (matcher) — trims & collapses whitespace before matching.
- `b.CurrentInnerTextForEach(selector)` → []string (each; **snapshot**, no poll) / `b.EachHaveInnerText(value|matcher)` (matcher — **≥1 match AND all satisfy**; the no-arg `BeEmpty()` form is gone — assert none via `HaveCount(0)`). Same for `b.CurrentTextContentForEach` / `b.EachHaveTextContent(...)`.
- `b.HaveClass(string|matcher)` (matcher) — string ⇒ "list contains"; matcher receives `[]string`. / `b.EachHaveClass(string)` (matcher) — **≥1 match AND all have the class** (fails on zero matches).
- `b.HaveAttribute(name[, string|matcher])` (matcher) — HTML attribute via `getAttribute`.
- `b.HaveComputedStyle(prop, string|matcher)` (matcher) — via `getComputedStyle`; getter counterpart `b.GetComputedStyle(selector, prop)` → string (see Geometry).
- `b.BeChecked()` (matcher) — checkbox/radio checked.
- `b.BeFocused()` (matcher) — is `document.activeElement`.

## Properties  (`.` paths like `dataset.name`; JS types preserved — numbers are `float64`)
**Two-axis polling**: the singular `Get*` getters poll until the element is present **AND** every named property/attribute is *defined*. Wrap a name in `b.AllowMissing("name")` to make an absent value a valid `nil` rather than something to wait for. **Sharp edge:** a property that simply doesn't exist on that element type (e.g. `disabled` on a `<div>` — `"disabled" in div` is false) would block the poll forever — wrap it in `AllowMissing`. The names params accept `string` or `AllowMissing` (`any`).
- `b.GetProperty(selector, name)` → any (first; polls) / `b.SetProperty(selector, name, value)` (dual) / `b.HaveProperty(name[, value|matcher])` (matcher).
- `b.GetProperties(selector, ...names)` → `Properties` (first; polls); getters `GetString/GetInt/GetFloat64/GetBool/GetStringSlice`.
- `b.GetAttribute(selector, name)` → any (first; polls; raw `getAttribute` markup, not the resolved property) / `b.GetAttributes(selector, ...names)` → `Properties` (first; polls).
- `b.AllowMissing(name)` — wrap a name passed to the four two-axis getters (`GetProperty`/`GetProperties`/`GetAttribute`/`GetAttributes`) so absent ⇒ `nil`, doesn't block the poll. No effect elsewhere.
- **Snapshot plural getters (no poll; `nil` for absent; gate presence first with `Eventually(sel).Should(b.HaveCount(n))`):** `b.CurrentPropertyForEach(selector, name)` → []any, `b.CurrentPropertiesForEach(selector, ...names)` → `SliceOfProperties` (getters return slices; `.Get(key)`, `.Find(key, val|matcher)`, `.Filter(key, val|matcher)`), `b.CurrentAttributeForEach(selector, name)` → []any, `b.CurrentAttributesForEach(selector, ...names)` → `SliceOfProperties`.
- `b.SetPropertyForEachImmediately(selector, name, value)` — set on **all** matches now, no poll (the `Immediately` suffix is the "know what you're doing" smell). / `b.EachHaveProperty(name[, ...])` (matcher — ≥1 match AND all satisfy).

## Form values  (rationalizes text/checkbox/radio/multi-select)
- `b.GetValue(selector)` → any (first; polls until present — empty string / unselected radio `""` is a valid value, no "defined" axis; bool for checkbox, checked radio's `value`, `[]string` for multi-select). / `b.CurrentValueForEach(selector)` → []any (each; snapshot, no poll).
- `b.SetValue(selector, value)` (dual) — requires visible+enabled; focuses, sets, blurs, fires `input`+`change`. Does **not** type real keys. For a `<select>` the value is matched against the **option `value`**, not its visible label (assert labels via `option.textContent`).
- `b.ValueLabel(label)` — wrap a `SetValue` arg to target a `<select>` option by its **visible label** instead of its value: `b.SetValue(sel, b.ValueLabel("Sonnet"))`. Multi-select: pass a slice whose entries are `ValueLabel`s (labels and raw values may be mixed). `<select>` only.
- `b.HaveValue(value|matcher)` (matcher).

## Geometry  (pollable layout reads — fold in layout-readiness; use instead of hand-rolled `b.Run` geometry)
**Readiness**: getters poll until the element is present **AND** laid out (non-degenerate box, `width`/`height` > 0) — so you never read a zero box mid-layout. All return viewport-relative CSS pixels. Each getter has a `Have*` matcher counterpart for `Eventually(sel).Should(...)` when the value is converging.
- `b.GetBoundingBox(selector)` → `Box{Top,Left,Width,Height,Bottom,Right,CenterX,CenterY}` (first; polls). / `b.HaveBoundingBox(matcher)` — matcher receives the `Box` (compose with Gomega's `HaveField`).
- `b.GetScrollOffset(selector)` → `ScrollOffset{Top,Left,MaxTop,MaxLeft}` (scroll container; `Top==MaxTop` ⇒ scrolled to bottom). / `b.HaveScrollOffset(matcher)`.
- `b.GetOffsetTopWithin(selector, container)` → float64 (`element.top - container.top`; "scrolled near the top of the pane"). `b.GetOffsetLeftWithin(selector, container)` is the horizontal sibling. / `b.HaveOffsetTopWithin(container, value|matcher)`, `b.HaveOffsetLeftWithin(container, value|matcher)`.
- **Pairwise (element-to-element; both boxes read in one atomic frame — don't split into two `GetBoundingBox`es, that loses the single-frame poll):** `b.BeAbove(other)` (`subject.Bottom<=other.Top`), `b.BeBelow(other)`, `b.BeLeftOf(other)` (`subject.Right<=other.Left`), `b.BeRightOf(other)`, `b.Encloses(other)` (contains on all 4 edges), `b.Overlaps(other)` (boxes intersect) — all matchers: `Eventually(subjectSel).Should(b.BeAbove(otherSel))`.
- `b.GetGapBetween(selector, other)` → `BoxDelta{Top,Left,Bottom,Right,Width,Height,CenterX,CenterY}` (subject minus other; first; polls — `CenterX~0` ⇒ shared center line, `Width~0` ⇒ same size). / `b.HaveGapBetween(other, value|matcher)` — matcher receives the `BoxDelta`.
- `b.BeInViewport()` (matcher) — element is laid out **and** its box intersects the visible layout viewport (actually on screen; ≠ `BeVisible`, which is only "rendered"). Partial overlap counts.
- `b.BePrecededBy(other)` / `b.BeFollowedBy(other)` (matchers) — document order via `compareDocumentPosition`.
- `b.GetComputedStyle(selector, property)` → string (first; polls; resolved value via `getPropertyValue`, so kebab-case names and CSS custom properties like `--stage` resolve — the getter counterpart of `HaveComputedStyle`, for Go-side math on the value).

## Clicking & interactions  (pragmatic simulations)
- `b.Click(selector)` (dual) — visible+enabled, then `el.click()`.
- `b.DblClick(selector)` (dual) — two clicks + `dblclick`. `b.RightClick(selector)` (dual) — `mousedown`/`mouseup`/`contextmenu`. `b.MiddleClick(selector)` (dual) — `mousedown`/`mouseup`/`auxclick`.
- `b.Tap(selector)` (dual) — synthetic touch/pointer events + `click` (realistic: real CDP `touchStart`/`touchEnd`); accepts `b.At(...)`, ignores modifiers.
- **Pointer options** — `b.At(x,y)` (offset from top-left, à la canvas/map/slider), `b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` (⌘/Win) — accepted by `Click`/`DblClick`/`RightClick`/`MiddleClick`/`Tap`, after the selector or in place of it (matcher form). They compose: `b.Click(sel, b.At(30,40), b.Shift())`. In fast mode any option switches a click off native `el.click()` to a synthetic event carrying coords+flags; realistic uses real CDP input natively.
- `b.DragTo(source, target)` (dual) — pointer-based drag (`pointerdown`/`move`/`up`); drives @dnd-kit-style DnD, not native HTML5 `draggable`. Matcher subject is the source: `Eventually(src).Should(b.DragTo(tgt))`.
- `b.ScrollWheel(selector, deltaX, deltaY)` (dual; matcher form `b.ScrollWheel(deltaX, deltaY)`) — `wheel` event then scrolls nearest scrollable ancestor (realistic: real CDP wheel); +deltaY=down, +deltaX=right.
- `b.ClickEachImmediately(selector)` — click all visible+enabled matches now, no poll (the `Immediately` suffix flags the no-readiness-fold smell; gate presence first).
- `b.Focus(selector)` (dual) / `b.Blur(selector)` (dual) / `b.Hover(selector)` (dual; fires pointer/mouse events, not CSS `:hover`) / `b.ScrollIntoView(selector)` (dual).
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
- `b.EvaluateTo(value|matcher)` (matcher) — assert a JS expression's result. Numbers decode to `float64` — use `BeNumerically`, not `Equal(intLiteral)`.
- `b.JSFunc(script)` → `.Invoke(...args)` string — JSON-encodes args into an invocable snippet.
- `b.JSVar(nameOrExpr)` — reference a JS variable/expression as a `JSFunc` argument (don't quote it).

## Network  (per-tab; reset by Prepare)
- `b.StubRequest(url string|matcher, biloba.StubResponse{Status,Body,Headers})` — first handler enables interception; unmatched requests pass through. Handlers below share one ordered, first-match-wins list.
- `b.AbortRequest(url string|matcher)` — fail matching requests (page's fetch rejects).
- `b.ModifyRequest(url string|matcher)` → builder `.WithURL(u).WithMethod(m).WithHeader(n,v).WithBody(b)` — continue to the real network with overrides (only what you set).
- `b.ModifyResponse(url string|matcher)` → builder `.WithStatus(s).WithHeader(n,v).WithBody(b)` or `.Using(func(biloba.InterceptedResponse) biloba.StubResponse)` — rewrite the real response (reads real status/headers/body; heavier: pauses twice).
- `b.HaveMadeRequest(url string|matcher)` (matcher) — chain `.WithMethod(m)`.
- `b.AllRequests()` → `Requests` (each `*Request` has `.URL/.Method/.Headers/.ResourceType`); `b.RequestMatching(...)` predicate for `.Find/.Filter`.
- `b.BeNetworkIdle()` (matcher) — zero in-flight requests. Tracks **HTTP** requests only (keyed on `Network.requestWillBeSent`/`loadingFinished` request IDs); a long-lived **WebSocket** does not keep it busy, so it won't wait for WS frames.

## Screenshots, outline, window  (details in biloba:debug-failures)
- `b.Outline()` → string — indented DOM text.
- `b.A11yOutline()` → string — accessibility tree (role + name).
- `b.CaptureScreenshot()` → []byte (PNG) / `b.CaptureImgcatScreenshot()` → string / `b.CaptureScreenshotToFile(path)` → abs path.
- `b.CaptureScreenshotOf(selector)` → []byte / `b.CaptureImgcatScreenshotOf(selector)` → string / `b.CaptureScreenshotOfToFile(selector, path)` → abs path — clipped to the first matching element (any selector; works below the fold and across `>>>` boundaries).
- `b.SetWindowSize(w, h, ...opt)` (auto-resets via DeferCleanup) / `b.WindowSize()`. Because it registers its own `DeferCleanup` to restore the prior size, you don't need a manual restore — and you must **not** call it from inside another `DeferCleanup` (Ginkgo forbids nesting), so call it bare in `BeforeEach`/`BeforeAll`.

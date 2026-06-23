## 0.7.1

Update plugin skills to reflect most recent changes and feedback.

## 0.7.0

### Features

- Add `b.GetAttribute(selector, name)` and `b.GetAttributeForEach(selector, name)` immediate getters - the attribute siblings of `b.GetProperty`/`b.GetPropertyForEach`, for reading raw HTML attributes into a Go variable.

### Fixes

- Harden tab and connection setup against transient failures under heavy parallel load: `NewTab` retries (and fails cleanly instead of returning a nil tab that panics on first use), tab registration retries the attach probe without closing a healthy spawned tab mid-recovery, and the idempotent connect-time setup round-trips (viewport/focus emulation, target info) retry with jittered backoff.
- Give full-page screenshot capture a generous (5s) per-tab timeout instead of a tight 1s, so a slow capture under load no longer spuriously reports "Timed out attempting to fetch screenshot".

## 0.6.0

### Features
- Keyboard modifier combos: hold `b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` during `Type`/`SendKeys` for hotkeys like Shift-Enter, Cmd-Enter, and Cmd-A (`b.SendKeys("textarea", biloba.Keys.Enter, b.Shift())`). These are the same shared modifier options you already hold during a click — no more dropping to chromedp for a modifier combo.
- `biloba.Keys` now covers the full editing/navigation/lock/function keyboard: added `Space`, `Insert`, `CapsLock`, `NumLock`, `ScrollLock`, `ContextMenu`, `PrintScreen`, `Pause`, `Help`, `Clear`, and `F1`–`F24`.

### Fixes
- Focus/blur events now fire reliably in full ("new") headless Chrome (e.g. driving `google-chrome`) — Biloba enables focus emulation on every tab, so `Focus`/`Blur` and your `onBlur` handlers work even though the headless window never holds OS focus.

## 0.5.2

### Features
- `SetValue` now writes through the native value setter, so it drives controlled React/Vue/Solid inputs (state-bound values are no longer reconciled away).
- New `Blur()` action + matcher (`b.Blur(sel)` / `Eventually(sel).Should(b.Blur())`) — fire commit-on-blur handlers after a `SetValue`.
- New `TextContent`/`HaveTextContent`/`TextContentForEach`/`EachHaveTextContent` family — layout-independent text reads, robust for dynamic content in headless.
- Occurrence-aware `SelectText`: `b.SelectText(sel, substring, b.Occurrence(n))` selects the Nth (1-based) occurrence of a word.

### Fixes
- **Behavior change:** `SetValue` no longer blurs text inputs — an `onBlur` handler no longer fires as a side effect of `SetValue` (use `b.Blur` to fire it).

## 0.5.1

### Features
- Biloba now warns once at spin-up if the connected Chrome is older than the minimum supported major version, with upgrade instructions. It tracks the latest stable Chrome and never warns on newer versions.

### Fixes
- Fix `ScrollIntoView` (and realistic-mode scroll-before-type) on Chrome 150+, where `Element.scrollIntoView()` now returns a Promise instead of `undefined` and was being shoved into the JS bridge's `success` field.

## 0.5.0

### Features
- `b.ValueLabel(label)` lets `SetValue` target a `<select>` option by its visible label instead of its `value` (works for single- and multi-select; labels and raw values can be mixed in a multi-select slice).

### Fixes
- `b.Run(script, nil)` now treats a nil decode target as "discard the result" instead of failing with `json: Unmarshal(nil)`.
- Decoding an `undefined` JS result into a non-nil pointer now fails with a directive error (omit the decode target for side-effect-only scripts, or return a value) instead of a cryptic JSON error.

## 0.4.0

### Features
- **First-class text selection.** `b.SelectText(selector)` selects all of an element's text and `b.SelectRange(selector, start, end)` selects a character sub-range across the element's text nodes — each producing a genuine `window.getSelection()` range and dispatching a `mouseup` so selection-driven UIs (highlight→menu toolbars, annotation layers, editors) react. Both follow the dual immediate/matcher convention (`Eventually("#p").Should(b.SelectText())`, `b.SelectRange(start, end)` as the matcher form); `b.ClearSelection()` drops the selection. Closes the gap that previously forced users into hand-rolled `document.createRange()`/`getSelection()` scripts.

### Fixes
- **`b.XPath()` / `b.RelativeXPath()` no longer double-prefix already-formed expressions.** A parenthesized/grouped expression like `(//ul[...])[3]` (or a leading `*` wildcard step) was being turned into `//(//ul[...])[3]`, an invalid XPath. They now pass any expression beginning with `/`, `./`, `(`, or `*` through verbatim, only prepending an axis to a bare element name.
- **`b.Run` now hints toward `RunAsync` when a script uses a top-level `return`.** `b.Run` evaluates a synchronous expression, so a top-level `return` is an `Illegal return statement` syntax error; the failure message now points you at `b.RunAsync` (which wraps your script in a function body) or an IIFE instead of leaving the raw V8 error.

### Debugging
- **Console errors are replayed at the top of the failure block.** Whenever a spec fails, Biloba now gathers every `console.error`/`console.assert` the page logged during the spec (across all tabs) and attaches them under "Console errors logged before this failure" — the originating error (e.g. the exception behind a React error boundary) is usually the root cause and was otherwise buried in the streamed timeline. No configuration required.

### Docs
- Documented text selection, the `Run` top-level-`return` rule, and the `float64`-for-numbers gotcha (`EvaluateTo`/`Run` JSON-decode numbers, so use `BeNumerically` not `Equal(intLiteral)`).
- `biloba:*` skills: `setup` now hands off loudly to `biloba:write-tests`; `write-tests` leads with a selector RULE, a "common smells" list, and a pocket matcher cheat-sheet; `debug-failures` documents the console-error surfacing; `api`/`write-tests` document the new selection primitives.

## 0.3.1

New performance comparisons with Playwright now online at [biloba-comparison](https://github.com/onsi/biloba-comparison).  tl;dr Biloba is 2.5-3x faster.

### Fixes

- **Realistic-mode clicks now pierce open shadow roots.** The realistic actionability/hittability check verifies the target (or a descendant) is the topmost element at its center point via `elementFromPoint` — but for an element inside an open shadow root `elementFromPoint` retargets to the shadow *host* and `Node.contains` doesn't cross the shadow boundary, so the check called every shadow-DOM element obscured and `b.Realistic().Click()` (and `DblClick`/`RightClick`/`BeClickable`/etc.) timed out. The hit-test now descends through each host's `shadowRoot.elementFromPoint` and walks the flattened (composed) tree to confirm containment across shadow boundaries, matching the fast track's `>>>`-piercing behavior. ([#5](https://github.com/onsi/biloba/issues/5))

## 0.3.0

0.3.0 keeps pushing Biloba toward being best-in-class for AI-driven browser testing, with two headline additions:

- **Semantic locators** — select elements the way a *user* perceives them (by accessible role + name, visible text, or form label) instead of by brittle CSS/XPath structure, with Playwright-style composition.
- **A realistic interaction track** — `b.Realistic()` routes interactions through *real* Chrome DevTools Protocol input (scroll-into-view, occlusion-aware clicks, genuine CSS `:hover`, real drags/wheel/touch) for the handful of specs that need the realism Biloba's fast atomic default trades away.

Plus a fuller interaction vocabulary (double / right / middle-click, tap, drag, wheel) with composable pointer options, network request **abort / modify**, and element-level screenshots.

Here are all the details, as generated by Claude:

---

## Features

### Selecting elements: semantic locators
- **`b.ByRole` / `b.ByText` / `b.ByLabel` and friends.** Select elements the way a user perceives them — by accessible role + name, visible text, or form label. Constructors: `b.ByRole("button").WithName("Save")` (and `.WithNameContains`), `b.ByText(...)`/`b.ByTextContains(...)`, `b.ByLabel(...)`/`b.ByLabelContains(...)`, plus **`b.ByPlaceholder`/`b.ByPlaceholderContains`**, **`b.ByAltText`/`b.ByAltTextContains`**, **`b.ByTitle`/`b.ByTitleContains`**, and **`b.ByTestID(id)`** (matches `data-testid` by default; the attribute is the package var `biloba.TestIDAttribute`). A `Locator` flows through every DOM method and matcher (and realistic mode), built on an in-page ARIA role + accessible-name engine. Coverage is pragmatic (explicit + common implicit roles; the common accessible-name sources — `aria-labelledby`/`aria-label`/`<label>`/`alt`/`placeholder`/`value`/text/`<figcaption>`/`<caption>`/`title`). Reach for these to assert the user-perceivable thing (a free a11y guard) or when a visible label is the natural identifier; CSS targeting stable `#id`/`[data-testid]` hooks stays the recommended default, and XPath is the rare axis/ordinal power tool.
- **Locators compose — filters, combinators, states, ordinals, and shadow piercing.** Refine a role with **`.Level(n)`** (heading level) and the ARIA-state filters **`.Checked()`**/**`.Disabled()`**/**`.Expanded()`**/**`.Pressed()`**/**`.Selected()`**. Filter by content with **`.ContainingText(t)`/`.NotContainingText(t)`** (visible text) and **`.Containing(sel)`/`.NotContaining(sel)`** (has a matching descendant). Combine with **`.And(sel)`/`.Or(sel)`** (set intersection/union), scope with **`.Within(scope)`**, and pick an ordinal with **`.Nth(i)`/`.First()`/`.Last()`**. Every selector-taking method (`Within`/`Containing`/`And`/`Or`/…) accepts **any** selector — CSS, `XPath`, or another `Locator` — so the pathways compose freely: `b.ByRole("button").And(".primary")`, `b.ByRole("listitem").Containing(b.ByText("Delete")).Within("#cart")`. Locators **pierce open shadow roots** automatically, so `b.ByRole(...)`/`b.ByText(...)` find elements inside open custom-element shadow DOM with no `>>>`.

### A second interaction track: realistic mode
- **`b.Realistic()`** returns a view of the tab whose interactions use *real* CDP input instead of Biloba's fast atomic JavaScript simulations. `Click`/`ClickEach` scroll into view, wait for the element to stop moving, move the real pointer (so hover-gated clicks fire), refuse to click through an occluding overlay, translate `>>>` same-origin iframe coordinates, and dispatch a real mouse click; `Hover` activates genuine CSS `:hover`; `SetValue` types text with real keys and toggles checkboxes with real clicks (native radio/`<select>` fall back to the fast JS path); `Type`/`SendKeys` scroll into view before typing. It's opt-in *per spec* — the same tab, just with its interactions routed through CDP — for the handful of smoke tests where the realism Biloba trades away for speed actually matters; the default `b` keeps its fast, atomic behavior. (Realistic interactions cost real round-trips and can reintroduce timing sensitivity — that's the deliberate, quarantined cost.)

### Richer interactions (across both the fast and realistic tracks)
- **Double-click & right-click** — `b.DblClick` and `b.RightClick` (dual immediate/matcher, same visible+enabled checks as `Click`). Fast mode fires synthetic `dblclick` / `contextmenu` events; realistic mode dispatches real CDP double-clicks and right-button clicks (firing Chrome's native context menu).
- **Middle-click** — `b.MiddleClick` (dual immediate/matcher) middle-clicks an element (fast mode fires `mousedown`/`mouseup`/`auxclick`; realistic mode dispatches a real middle-button click).
- **Tap (touch)** — `b.Tap(selector)` (dual immediate/matcher) taps an element. Fast mode dispatches synthetic touch/pointer events (`pointerdown`/`pointerup` + `touchstart`/`touchend`) plus a culminating `click`; realistic mode dispatches a real CDP touch (`touchStart`/`touchEnd`) — genuine trusted touch input.
- **Drag-and-drop** — `b.DragTo(source, target)` drags one element onto another with a pointer-based drag (`pointerdown`/`pointermove`/`pointerup`), driving modern pointer-based DnD libraries (@dnd-kit and friends). Dual immediate/matcher: pass both selectors to act immediately, or pass only the target and poll the source — `Eventually("#card").Should(b.DragTo("#column"))` — so it waits until both endpoints are present before dragging. Realistic mode drives the same drag with real CDP mouse input. It intentionally does **not** drive native HTML5 `draggable` — drop to `chromedp` via `b.Context` for that.
- **Mouse wheel** — `b.ScrollWheel(selector, deltaX, deltaY)` scrolls the wheel over an element (positive deltaY=down, deltaX=right). Immediate-only (no matcher form). Fast mode dispatches a synthetic `wheel` event and then manually scrolls the nearest scrollable ancestor unless a handler called `preventDefault()`; realistic mode dispatches a real CDP wheel event that scrolls via genuine trusted input.
- **Pointer options (offsets & modifiers)** — `Click`, `DblClick`, `RightClick`, `MiddleClick`, and `Tap` accept composable, typed pointer options after the selector (or in place of it, in the matcher form): `b.At(offsetX, offsetY)` targets a point offset from the element's top-left corner (à la Playwright's `position`; for `<canvas>`/map/slider apps), and `b.Shift()`/`b.Ctrl()`/`b.Alt()`/`b.Meta()` hold a keyboard modifier down (`Meta` is Command on macOS / Windows key elsewhere). They compose — `b.Click("#canvas", b.At(30, 40), b.Shift())` — and work in the matcher form — `Eventually("#canvas").Should(b.Click(b.At(30, 40), b.Shift()))`. A plain `b.Click(sel)` still calls native `el.click()`; adding any option switches the fast path to a synthetic `mousedown`/`mouseup`/`click` carrying the real `clientX`/`clientY` and modifier flags (a deliberate fidelity-for-control trade). Realistic mode uses real CDP input and honors the options natively. (`Tap` ignores modifiers — they don't apply to touch — but honors `b.At`.)

### Network: abort & modify
- **`b.AbortRequest` / `b.ModifyRequest` / `b.ModifyResponse`** — building on 0.2.0's stub-and-observe, the request handlers now form one ordered, first-match-wins list (per-tab, reset by `Prepare()`). `b.AbortRequest(url)` fails matching requests (the page's fetch rejects). `b.ModifyRequest(url)` returns a chainable builder (`.WithURL/.WithMethod/.WithHeader/.WithBody`) that continues the request to the real network with overrides. `b.ModifyResponse(url)` rewrites the real response coming back — chain `.WithStatus/.WithHeader/.WithBody`, or `.Using(func(biloba.InterceptedResponse) biloba.StubResponse)` to read the real status/headers/body and return a replacement (enables CDP response-stage interception).

### Screenshots
- **Element-level screenshots** — `b.CaptureScreenshotOf(selector)`, `b.CaptureImgcatScreenshotOf(selector)`, and `b.CaptureScreenshotOfToFile(selector, path)` capture just the first element matching any Biloba selector (clipped to its bounding box). Works for elements below the fold (no scroll needed) and same-origin `>>>`-pierced iframe/shadow elements (coordinates translated to the top-level page).

### New matchers
- **`b.BeClickable()`** — visible + enabled + not obscured: a deterministic, atomic occlusion/hittability check (via `elementFromPoint`) that catches an element being covered by an overlay or scrolled out of the viewport (which plain `Click` would silently click through). A cheap way to assert actionability without paying for realistic mode.
- **`b.EachBeVisible()` / `b.EachBeEnabled()` / `b.EachHaveClass(string)`** — `Each*` counterparts to `BeVisible`/`BeEnabled`/`HaveClass` that assert on **every** element matching `selector` (vacuously true when none match), mirroring `EachHaveProperty`/`EachHaveInnerText`.

### Fixes
- **Realistic-mode wheel/scroll input now works to the bottom of the viewport under `HighFidelityHeadless()`.** Full ("new") headless Chrome composites into a small virtual screen (default 800×600) regardless of `--window-size`, and its compositor's trusted-input surface was clamped to that screen — so an element Biloba reported as `inViewport` (against the taller emulated layout viewport) could sit below the real input surface, where trusted CDP wheel/scroll gestures were silently dropped. Biloba now grows the emulated *screen* to match the viewport and re-asserts it after each navigation, keeping the layout viewport and the real compositor input surface in agreement. No effect on the default `chrome-headless-shell` lane or on the fast track.

### Tooling / docs
- **New `biloba:realistic-mode` skill** and refreshed `biloba:*` skills covering the realistic interaction track and the expanded interaction + locator vocabulary.
- `docs/index.md` now presents the **three selection pathways** (CSS as the recommended default, semantic locators, XPath) with guidance on when to reach for each, plus the fast-vs-realistic interaction capability matrix.

## 0.2.0

Biloba's back after a long hiatus!  I (Onsi) am planning on using this thing to drive development of a new complex single page app with Claude.  Development is now focused on making Biloba best in class for AI coding agents - that means:
- speed, determinism, and a fluent DSL that plays nicely with tokens
- improvements to the feedback channel so agents can "see" failures more easily
- closing coverage gaps to reduce the need to drop to raw `chromedp`

All without losing the human-usability side of the equation.

**Claude Code plugin.** Biloba now ships a set of Claude Code skills as a plugin, with the repo doubling as the marketplace (`/plugin marketplace add onsi/biloba` then `/plugin install biloba@biloba`). The `biloba:*` skills cover the mental model, suite setup, the dual immediate/matcher API, the XPath DSL, a full API reference, orienting to an unfamiliar page, and debugging failures — so an agent writing tests against *your* app has Biloba's idioms on hand. (The repo's own `.claude/skills/` remain contributor-facing.)

> ⚠️ **This is a non-backward-compatible release.** Biloba is pre-1.0 and makes no API-stability guarantees. Several existing signatures changed and one default behavior changed. See the [Migration Guide](#migration-guide) below.

Here are all the details, as generated by Claude:

---

## Features

### Feedback channel (so agents can "see" failures)
- **`b.Outline()`** — returns a pruned, indented text snapshot of the DOM (scripts/styles/svg stripped, whitespace collapsed) so an agent can understand *why a selector didn't match* without vision. Attached on failure automatically under CI/agent (see below); force it with `BilobaConfigFailureOutlines()`.
- **Environment-aware failure artifacts** — Biloba now tailors on-failure output to where it runs. An interactive human gets a screenshot (inline where the terminal supports it) and no DOM outline. **Under CI or an AI agent (auto-detected via [`agentdetection`](https://github.com/jehiah/agentdetection) — `CLAUDECODE`, `AI_AGENT`, Cursor, Gemini, Codex, … — plus `CI`), Biloba flips to text-friendly artifacts**: DOM outlines on, inline blobs off, and screenshots written to disk (`./biloba-screenshots` by default, or `BILOBA_SCREENSHOTS_DIR`) so they can be inspected or uploaded as CI artifacts. So an agent/CI run needs **zero configuration**. Explicit `ConnectToChrome` options always win, per knob.
- **`b.A11yOutline()`** — a compact accessibility-tree snapshot (roles/names) built on CDP's `Accessibility.getFullAXTree`; often more useful to a model than raw HTML.
- **Screenshots to files** — `BilobaConfigScreenshotsToDir(dir)` config and `b.CaptureScreenshotToFile(path)` write PNGs to disk and print the path, so an agent can `Read` the image and literally see the page.
- **Portable inline screenshots** — failure/progress screenshots now auto-detect the terminal and emit the best inline-image protocol it supports: **Kitty**, **iTerm2** (`OSC 1337`, also VS Code/WezTerm/Konsole), or **Sixel** (via [`rasterm`](https://github.com/BourgeoisBear/rasterm)). Detection is env-var based with an opt-in live probe (`BILOBA_PROBE_TERMINAL=true`) for Sixel terminals that don't advertise themselves; `BILOBA_INLINE_SCREENSHOTS=iterm|kitty|sixel` forces a protocol and `=none` disables it (so does `BilobaConfigInlineScreenshots(false)`). Stops dumping ~70 KB of base64 noise into terminals that can't render it. issue #3)*

### New coverage (previously required dropping to chromedp)
- **Cookies** — `b.SetCookie(...)`, `b.GetCookies()`, `b.ClearCookies()`, plus chainable matchers `b.HaveCookie(name).WithValue/WithPath/WithDomain/WithSameSite/WithSecure/WithHTTPOnly(...)`, `b.HaveNumCookies(...)`, and `Cookies.Find/Filter` via `b.CookieMatching(...)`.
- **Web storage** — `b.LocalStorage()` / `b.SessionStorage()` returning a typed `*Storage` (`.Set/.Get/.GetAll/.Remove/.Clear/.Length`), plus matchers `b.HaveLocalStorageItem`, `b.HaveSessionStorageItem`, `b.HaveNumLocalStorageItems`, `b.HaveNumSessionStorageItems`.
- **Real keyboard input** — `b.Type(sel, "text")` (dual immediate/matcher) and `b.SendKeys(sel, biloba.Keys.Enter)` with a `biloba.Keys` namespace of named keys (Enter, Tab, Escape, arrows, Home/End/PageUp/PageDown, …).
- **Network stub & observe** — `b.StubRequest(url, StubResponse{Status, Body, Headers})`, request observation via `b.AllRequests()` / `b.RequestMatching(url)` / `b.HaveMadeRequest(url).WithMethod(...)`, and `b.BeNetworkIdle()` for wait-for-idle. Built on the CDP `Fetch`/`Network` domains.
- **File upload** — `b.SetUpload(sel, paths...)` via `DOM.setFileInputFiles`.
- **First-class hover/focus/scroll** — `b.Hover`, `b.Focus`, `b.ScrollIntoView` (all dual immediate/matcher).
- **iframe + Shadow-DOM piercing** — selectors can now cross same-origin iframe and shadow boundaries via the `>>>` combinator.

### Async
- **`b.RunAsync` / `b.RunErrAsync`** — `await`/Promise support in browser-side JS (wraps in an async IIFE and uses CDP `awaitPromise:true`).

### New matchers & selector sugar
- **`b.HaveText(...)`** — whitespace-normalized text match (trims + collapses), avoiding spurious failures from templating whitespace.
- **`b.HaveAttribute(name, expected...)`** — assert on HTML attributes (distinct from DOM properties).
- **`b.BeChecked()`**, **`b.BeFocused()`** — sugar for common assertions.
- **`b.HaveComputedStyle(prop, expected)`** — assert on `getComputedStyle` values.
- **`b.HaveURL(...)`** / **`b.HaveTitle(...)`** — tab matchers for `Eventually(b).Should(...)`.
- **`b.WithText("Submit")`** / **`b.WithTextContains("Sav")`** — top-level text-selector shortcuts over `b.XPath().WithText(...)`.

### Chrome lifecycle & config
- **`chrome-headless-shell` is now the default** headless mode (the new full `--headless` is ~6.6× slower per CDP op and serializes multi-window work, collapsing Biloba's parallelism). New opt-ins: `HighFidelityHeadless()` (use full headless), `AutoInstallHeadlessShell()`, `HeadlessShellPath(path)`.
- **`StartingWindowSize(w, h)`** and **`ChromeFlags(...chromedp.ExecAllocatorOption)`** spin-up options.
- **`b.Prepare()` now clears cookies and web storage** in addition to closing non-root tabs — for true inter-spec isolation.

### Tooling / docs
- New skills for Claude.  Install with:
```
/plugin marketplace add onsi/biloba
/plugin install biloba@biloba
```
- Expanded `docs/index.md` with details for all the new stuff (storage, keyboard, network, upload, shadow, iframe, outline, interactions)

---

## Migration Guide

This release changes a handful of **existing** signatures and one default. New-in-this-release APIs are listed above and need no migration.

### 1. `SpinUpChrome` no longer takes raw chromedp options

`SpinUpChrome` now takes Biloba `SpinUpOption`s instead of `chromedp.ExecAllocatorOption`s. Wrap raw chromedp options in `ChromeFlags(...)`.

```go
// Before
SpinUpChrome(GinkgoT(), chromedp.WindowSize(1024, 768), chromedp.Flag("hide-scrollbars", true))

// After
SpinUpChrome(GinkgoT(), biloba.ChromeFlags(chromedp.WindowSize(1024, 768), chromedp.Flag("hide-scrollbars", true)))
// (or use the new StartingWindowSize / HighFidelityHeadless options directly)
```

### 2. Default headless changed → `chrome-headless-shell`

Biloba now drives `chrome-headless-shell` by default rather than Chrome's full `--headless`. This is faster and restores parallel suite performance, but the shell binary must be available.

- To have Biloba fetch it for you: `SpinUpChrome(GinkgoT(), biloba.AutoInstallHeadlessShell())`.
- To point at an existing binary: `biloba.HeadlessShellPath("/path/to/chrome-headless-shell")`.
- To keep the old behavior (full headless Chrome): `biloba.HighFidelityHeadless()`.

### 3. Tab matchers/filters → chainable query builder

`HaveSpawnedTab` / `HaveTab` no longer take a `TabFilter`; the `TabWith*` filter constructors are gone. Use the chainable builder returned by `HaveSpawnedTab()` / `HaveTab()` / `TabMatching()`.

```go
// Before
Eventually(b).Should(b.HaveSpawnedTab(b.TabWithURL("/foo")))
Eventually(b).Should(b.HaveTab(b.TabWithTitle("Foo")))
b.AllSpawnedTabs().Find(b.TabWithDOMElement("#x"))

// After
Eventually(b).Should(b.HaveSpawnedTab().WithURL("/foo"))
Eventually(b).Should(b.HaveTab().WithTitle("Foo"))
b.AllSpawnedTabs().Find(b.TabMatching().WithDOMElement("#x"))
```

Builder refinements: `.WithURL(...)`, `.WithTitle(...)`, `.WithDOMElement(...)`.

### 4. Download matchers/filters → chainable query builder

`HaveCompleteDownload(DownloadFilter)` and the `DownloadWith*` filter constructors are gone, replaced by `HaveDownloaded(...)` / `DownloadMatching(...)`.

```go
// Before
Eventually(b).Should(b.HaveCompleteDownload(b.DownloadWithFilename("report.csv")))
b.AllDownloads().Find(b.DownloadWithURL("/report"))

// After
Eventually(b).Should(b.HaveDownloaded("report.csv"))               // filename is the positional arg
b.AllDownloads().Find(b.DownloadMatching().WithURL("/report"))
```

Builder refinements: `.WithURL(...)`, `.WithContent(...)` (and the positional filename arg on `HaveDownloaded(...)` / `DownloadMatching(...)`).

### 5. `Tabs.Find` / `Tabs.Filter` / `Downloads.Find` / `Downloads.Filter` signatures

These now take the new `*TabQuery` / `*DownloadQuery` builders instead of the old `TabFilter` / `DownloadFilter` func types — see #3 and #4 for the replacement call sites.

### 6. Boolean `BilobaConfig` options renamed and made variadic

The boolean `ConnectToChrome` options now share one positive-sense, variadic shape: call with no argument for `true`, or pass `false` to disable.

```go
// Before
biloba.ConnectToChrome(GinkgoT(), biloba.BilobaConfigEnableDebugLogging())
biloba.ConnectToChrome(GinkgoT(), biloba.BilobaConfigDisableFailureScreenshots())
biloba.ConnectToChrome(GinkgoT(), biloba.BilobaConfigDisableProgressReportScreenshots())

// After
biloba.ConnectToChrome(GinkgoT(), biloba.BilobaConfigDebugLogging())
biloba.ConnectToChrome(GinkgoT(), biloba.BilobaConfigFailureScreenshots(false))
biloba.ConnectToChrome(GinkgoT(), biloba.BilobaConfigProgressReportScreenshots(false))
```

The two artifact options added during 0.2.0 development follow the same shape: `BilobaConfigFailureOutlines(...bool)` and `BilobaConfigInlineScreenshots(...bool)` (the latter replaces the never-released `BilobaConfigDisableInlineScreenshots`). The inline-protocol environment variable is now `BILOBA_INLINE_SCREENSHOTS=iterm|kitty|sixel|none` (was `BILOBA_IMGCAT` / `BILOBA_NO_IMGCAT`).

---

## 0.1.6

### Fixes
- catch edge case where the _biloba object isn't available because the browser is in the middle of a redirect [d9df233]

### Maintenance
- bump ginkgo [09da081]

## 0.1.5

### Fixes
- Correctly escape quote characters when constructing XPath queries [7ef2785]

## 0.1.4

### Features
- emit failure message when running with BILOBA_INTERACTIVE=true [777f184]

## 0.1.3

### Features
- add ability to specify a default screnshot size for autogenerated screenshots [7670a24]

## 0.1.2

### Maintenance
- bump ginkgo and gomega [37c6e75]

## 0.1.1

### Fixes
- add focus/blur events when setting value [f8963b6]

### Maintenance
- Minor typos found when learning about awesome Biloba and chromedp [47ef44f]

## 0.1.0

- First release!


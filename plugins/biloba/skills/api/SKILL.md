---
name: api
description: One-line reference for every Biloba method and matcher, grouped by area — lifecycle, navigation, cookies/storage, tabs, DOM existence/visibility/contents/properties/forms, clicking and interactions, keyboard, uploads, element JS, dialogs, downloads, arbitrary JS, network stubbing/observing, and screenshots/outline/window. Use to look up the exact method or matcher name and shape. Methods marked (dual) act immediately when fully applied and return a pollable matcher when under-applied.
---

# Biloba API reference

Terse lookup. **(dual)** = acts immediately when fully applied, returns a Gomega matcher when under-applied (poll with `Eventually`). **(matcher)** = always returns a matcher. **first** = acts on the first match; **each** = acts on all matches (empty slice when none). Selectors are CSS strings or `XPath` (see `biloba:xpath`). Full docs: <https://onsi.github.io/biloba/>.

## Lifecycle / config
- `biloba.SpinUpChrome(GinkgoT(), ...SpinUpOption)` — start Chrome (process 1). Options: `HighFidelityHeadless()`, `AutoInstallHeadlessShell()`, `HeadlessShellPath(p)`, `StartingWindowSize(w,h)`, `ChromeFlags(...)`. See `biloba:setup`.
- `biloba.ConnectToChrome(GinkgoT(), ...BilobaConfig)` — open this process's root tab `b`. Config: see `biloba:debug-failures`.
- `b.Prepare()` — reset the root tab between specs (BeforeEach, `OncePerOrdered`).
- `b.Context` — the tab's `chromedp` context (escape hatch).

## Navigation
- `b.Navigate(url)` — navigate, assert `200`.
- `b.NavigateWithStatus(url, code)` — navigate, assert a specific status.
- `b.Location()` / `b.Title()` — current URL / title (pollable).
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
- `b.BeVisible()` (matcher) — non-zero `offsetWidth`/`offsetHeight`.
- `b.BeEnabled()` (matcher) — `!el.disabled`.

## Contents, classes, attributes, state
- `b.InnerText(selector)` → string (first) / `b.HaveInnerText(string|matcher)` (matcher, exact).
- `b.HaveText(string|matcher)` (matcher) — trims & collapses whitespace before matching.
- `b.InnerTextForEach(selector)` → []string (each) / `b.EachHaveInnerText(...)` (matcher).
- `b.HaveClass(string|matcher)` (matcher) — string ⇒ "list contains"; matcher receives `[]string`.
- `b.HaveAttribute(name[, string|matcher])` (matcher) — HTML attribute via `getAttribute`.
- `b.HaveComputedStyle(prop, string|matcher)` (matcher) — via `getComputedStyle`.
- `b.BeChecked()` (matcher) — checkbox/radio checked.
- `b.BeFocused()` (matcher) — is `document.activeElement`.

## Properties  (`.` paths like `dataset.name`; JS types preserved — numbers are `float64`)
- `b.GetProperty(selector, name)` → any (first) / `b.SetProperty(selector, name, value)` (dual) / `b.HaveProperty(name[, value|matcher])` (matcher).
- `b.GetPropertyForEach(selector, name)` → []any / `b.SetPropertyForEach(selector, name, value)` (no matcher) / `b.EachHaveProperty(name[, ...])` (matcher).
- `b.GetProperties(selector, ...names)` → `Properties` (first); getters `GetString/GetInt/GetFloat64/GetBool/GetStringSlice`.
- `b.GetPropertiesForEach(selector, ...names)` → `SliceOfProperties` (each); same getters return slices; `.Get(key)`, `.Find(key, val|matcher)`, `.Filter(key, val|matcher)`.

## Form values  (rationalizes text/checkbox/radio/multi-select)
- `b.GetValue(selector)` → any (first; bool for checkbox, checked radio's `value`, `[]string` for multi-select).
- `b.SetValue(selector, value)` (dual) — requires visible+enabled; focuses, sets, blurs, fires `input`+`change`. Does **not** type real keys.
- `b.HaveValue(value|matcher)` (matcher).

## Clicking & interactions  (pragmatic simulations)
- `b.Click(selector)` (dual) — visible+enabled, then `el.click()`.
- `b.DblClick(selector)` (dual) — two clicks + `dblclick`. `b.RightClick(selector)` (dual) — `mousedown`/`mouseup`/`contextmenu`.
- `b.DragTo(source, target)` — pointer-based drag (`pointerdown`/`move`/`up`); drives @dnd-kit-style DnD, not native HTML5 `draggable` (no matcher).
- `b.ScrollWheel(selector, deltaX, deltaY)` — `wheel` event then scrolls nearest scrollable ancestor (realistic: real CDP wheel); +deltaY=down, +deltaX=right (no matcher).
- `b.ClickEach(selector)` — click all visible+enabled matches (no matcher).
- `b.Focus(selector)` (dual) / `b.Hover(selector)` (dual; fires pointer/mouse events, not CSS `:hover`) / `b.ScrollIntoView(selector)` (dual).

## Keyboard  (real key events, via chromedp)
- `b.Type(selector, text)` (dual) — focus, then genuine keystrokes; **appends**.
- `b.SendKeys([selector,] ...parts)` — send text + named keys; selector optional (else focused element).
- `biloba.Keys.{Enter,Tab,Escape,Backspace,Delete,Arrow{Up,Down,Left,Right},Home,End,PageUp,PageDown}`.

## Uploads
- `b.SetUpload(selector, ...paths)` — set `<input type=file>` files via CDP (paths must exist on Chrome's machine); fires `change`.

## Run JS on selected elements
- `b.InvokeOn(selector, method, ...args)` → any (first) — `el[method](...args)`.
- `b.InvokeOnEach(selector, method, ...args)` (each).
- `b.InvokeWith(selector, jsFn, ...args)` → any (first) — `jsFn(el, ...args)`.
- `b.InvokeWithEach(selector, jsFn, ...args)` (each).

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
- `b.Run(script[, &ptr])` → any — synchronous expression.
- `b.RunAsync(script[, &ptr])` / `b.RunErrAsync(...)` — body of an async fn; you `return` the awaited value.
- `b.EvaluateTo(value|matcher)` (matcher) — assert a JS expression's result.
- `b.JSFunc(script)` → `.Invoke(...args)` string — JSON-encodes args into an invocable snippet.
- `b.JSVar(nameOrExpr)` — reference a JS variable/expression as a `JSFunc` argument (don't quote it).

## Network  (per-tab; reset by Prepare)
- `b.StubRequest(url string|matcher, biloba.StubResponse{Status,Body,Headers})` — first stub enables interception; unmatched requests pass through.
- `b.HaveMadeRequest(url string|matcher)` (matcher) — chain `.WithMethod(m)`.
- `b.AllRequests()` → `Requests` (each `*Request` has `.URL/.Method/.Headers/.ResourceType`); `b.RequestMatching(...)` predicate for `.Find/.Filter`.
- `b.BeNetworkIdle()` (matcher) — zero in-flight requests.

## Screenshots, outline, window  (details in biloba:debug-failures)
- `b.Outline()` → string — indented DOM text.
- `b.A11yOutline()` → string — accessibility tree (role + name).
- `b.CaptureScreenshot()` → []byte (PNG) / `b.CaptureImgcatScreenshot()` → string / `b.CaptureScreenshotToFile(path)` → abs path.
- `b.SetWindowSize(w, h, ...opt)` (auto-resets via DeferCleanup) / `b.WindowSize()`.

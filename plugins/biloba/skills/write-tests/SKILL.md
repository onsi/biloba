---
name: write-tests
description: Author good Biloba specs in your own Ginkgo/Gomega suite — the dual immediate/matcher API (act now vs. return a matcher you poll with Eventually), first-vs-all naming, the navigate-then-readiness-anchor shape, selecting elements (semantic role/text/label locators as the default, CSS, the >>> piercing combinator, XPath), the interaction vocabulary (click variants, drag, scroll, tap), realistic mode for occlusion/hover smoke tests, hermetic tests via request stubbing/aborting/modifying, multi-tab flows, and seeding state. Use when writing or reviewing Biloba browser tests.
---

# Writing Biloba specs

Assumes Biloba is already wired into the suite (`biloba:setup`) and you know the principles (`biloba:overview`). For the full method list see `biloba:api`; for XPath see `biloba:xpath`. Docs: <https://onsi.github.io/biloba/#working-with-the-dom>.

## The one pattern to internalize: dual immediate/matcher

Most DOM methods have **two forms keyed on argument count**:

- **Fully-applied → acts immediately, fails the spec on error.**
  ```go
  b.Click("#go")
  b.SetValue("#name", "Jane")
  text := b.InnerText("#title")
  ```
- **Under-applied → returns a Gomega matcher that *you* poll.** Biloba never polls itself.
  ```go
  Eventually("#go").Should(b.Click())            // poll until clickable, then click once
  Eventually("#name").Should(b.SetValue("Jane")) // poll until settable
  Eventually("#title").Should(b.HaveInnerText("Welcome"))
  ```

The matcher form lets you fold readiness-waiting into the action — no separate "is it there yet" poll. `b.Click("#login")` right after `b.Navigate` may race the page load; `Eventually("#login").Should(b.Click())` won't.

**First-vs-all naming.** A bare method acts on the **first** match; the `ForEach`/`Each` sibling acts on **all** matches (returning/asserting slices, empty when nothing matches): `InnerText` vs `InnerTextForEach`/`EachHaveInnerText`; `GetProperty` vs `GetPropertyForEach`/`EachHaveProperty`; `Click` vs `ClickEach`. The name tells you which.

## The spec shape

Navigate, gate on a **readiness anchor**, then exercise behavior:

```go
var _ = Describe("the search page", func() {
	BeforeEach(func() {
		b.Navigate("http://localhost:8080/search")
		Eventually("#results").Should(b.Exist()) // page is ready once this appears
	})

	It("finds matches", func() {
		b.SetValue("#q", "biloba")
		Eventually(b.ByRole("button").WithName("Search")).Should(b.Click())
		Eventually(".result").Should(b.HaveCount(BeNumerically(">", 0)))
	})
})
```

- `b.Navigate(url)` also asserts the response was `200` (use `NavigateWithStatus` for other codes).
- Pick a **stable, meaningful** anchor (a heading, a key container) — `b.Exist()` or `b.BeVisible()`.
- **Assert on observable outcomes**, not implementation: visible text (`HaveInnerText`/`HaveText`), counts (`HaveCount`), URL/title (`HaveURL`/`HaveTitle`), or network effects (`HaveMadeRequest`).

## Selecting elements — three pathways, CSS first

A `selector` is a **CSS string**, a **semantic `Locator`** (`b.By*`), or an **`XPath`** value. Pick by this guide:

- **CSS — the default.** For an app you own, target **stable, intentional hooks**: an `#id` or a `[data-testid]` you add on purpose. *Don't* couple tests to styling classes (`.btn-primary`) — they get renamed in redesigns. Fastest pathway; supports `:has()`; pierces shadow/iframe via `>>>`.
- **Locators — reach second**, in two cases: (a) you *want* to assert the user-perceivable thing (a button's accessible name, a heading's level) — a free a11y-regression guard; (b) a hook isn't worth it and the visible label/text is the natural identifier (`b.ByText("Sign in")`). Most resilient/readable for user-facing elements, slowest engine (full-document ARIA scan).
- **XPath — rare power tool** (`biloba:xpath`) for axis/relationship/ordinal queries CSS can't express, or exact `text()` matching. Fast but verbose; does **not** pierce shadow/iframe.

```go
b.Click("#go")                                    // CSS by id — stable hook (preferred default)
b.Click("[data-testid=save]")                     // CSS by intentional test-id
Eventually("tr:has(td.overdue)").Should(b.Exist())// CSS :has() — "the row that contains X"
b.Click(b.ByRole("button").WithName("Save"))      // Locator — role + accessible name (a11y guard)
b.Click(b.ByText("Submit"))                       // Locator — visible text (b.ByTextContains for substring)
b.SetValue(b.ByLabel("Email"), "jane@acme.com")   // Locator — a form control by its label
b.Click(b.XPath("li").WithText("OK").Ancestor("ul"))// XPath — axis query no CSS/locator expresses
```

**Locator constructors** (each text-valued one has a `*Contains` variant): `b.ByRole`, `b.ByText`, `b.ByLabel`, `b.ByPlaceholder`, `b.ByAltText`, `b.ByTitle`, `b.ByTestID` (attr = `biloba.TestIDAttribute`, default `data-testid`). Refine a role with `.WithName(n)`, `.Level(n)` (heading), or ARIA states `.Checked()`/`.Disabled()`/`.Expanded()`/`.Pressed()`/`.Selected()`.

Locators **compose** — and the filters/combinators accept **any** selector (CSS/XPath/Locator), so pathways mix:

```go
b.ByRole("listitem").ContainingText("Product 2")             // .ContainingText / .NotContainingText
b.ByRole("listitem").Containing(b.ByText("Delete"))          // .Containing / .NotContaining (a descendant)
b.ByRole("button").And(".primary")                           // .And / .Or — set intersection / union
b.ByRole("button").WithName("Delete").Within("#dialog")      // .Within(scope)
b.ByText("Item").Nth(2)                                      // .Nth(i)/.First()/.Last() — ordinal
```

Locators **pierce open shadow roots automatically** (no `>>>`); CSS needs the `>>>` combinator (one boundary each, open shadow / same-origin iframe only); XPath crosses neither.

```go
b.Click("my-widget >>> button.submit")
Eventually("#editor-frame >>> .toolbar .save").Should(b.Click())
```

Never fetch-then-act — always pass the selector *into* the action so find-and-act is one atomic JS snippet.

## The interaction vocabulary

`b.Click` is the everyday verb (dual: `b.Click(sel)` acts; `Eventually(sel).Should(b.Click())` polls). The fuller set — all dual unless noted, all working on both the fast and realistic tracks:

```go
b.DblClick(sel); b.RightClick(sel); b.MiddleClick(sel)       // dual
b.Click(sel, b.At(x, y))                                     // click at top-left-origin offset; canvas/map/slider
b.Click(sel, b.Shift(), b.Meta())                            // modifiers held; composes with b.At(...)
b.DragTo(source, target)                                     // pointer-based drag; dual — Eventually(src).Should(b.DragTo(tgt))
b.ScrollWheel(sel, deltaX, deltaY)                           // wheel/scroll, +Y down +X right (immediate-only)
b.Tap(sel)                                                   // touch tap (dual); takes b.At(...), ignores modifiers
b.Type(sel, "abc"); b.SendKeys(biloba.Keys.Enter)            // real keystrokes (SetValue does NOT type)
```

`b.At(x,y)` / `b.Shift()` / `b.Ctrl()` / `b.Alt()` / `b.Meta()` (⌘/Win) are **pointer options** accepted by `Click`/`DblClick`/`RightClick`/`MiddleClick`/`Tap` — after the selector (immediate) or in place of it (matcher: `Eventually(sel).Should(b.Click(b.At(x,y), b.Shift()))`). In fast mode any option switches a click off native `el.click()` to a synthetic event carrying the coords/flags. `ScrollWheel` is immediate-only.

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

`b.Run` is synchronous; use `b.RunAsync` (which `return`s an awaited value) for `fetch`/`await`. `b.EvaluateTo` asserts on a JS expression directly.

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

For realism (occlusion, scroll-into-view, real CSS `:hover`) reach for `b.Realistic()` (above) before chromedp. For everything else — cross-origin frames, geolocation, any CDP feature without a wrapper — drop to chromedp via `b.Context` (the escape hatch in `biloba:overview`). For real keystrokes use `b.Type`/`b.SendKeys` rather than `SetValue`.

Propose opening an issue if a common pattern is missing.

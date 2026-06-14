---
name: write-tests
description: Author good Biloba specs in your own Ginkgo/Gomega suite — the dual immediate/matcher API (act now vs. return a matcher you poll with Eventually), first-vs-all naming, the navigate-then-readiness-anchor shape, selecting elements (CSS, the >>> piercing combinator, text/XPath), hermetic tests via request stubbing, multi-tab flows, and seeding state. Use when writing or reviewing Biloba browser tests.
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
		Eventually(b.WithText("Search")).Should(b.Click())
		Eventually(".result").Should(b.HaveCount(BeNumerically(">", 0)))
	})
})
```

- `b.Navigate(url)` also asserts the response was `200` (use `NavigateWithStatus` for other codes).
- Pick a **stable, meaningful** anchor (a heading, a key container) — `b.Exist()` or `b.BeVisible()`.
- **Assert on observable outcomes**, not implementation: visible text (`HaveInnerText`/`HaveText`), counts (`HaveCount`), URL/title (`HaveURL`/`HaveTitle`), or network effects (`HaveMadeRequest`).

## Selecting elements

A `selector` is either a **CSS string** or an **`XPath`** value:

```go
b.Click("button.submit")                 // CSS — first matching element
b.Click(b.XPath("button").WithText("OK")) // XPath via the DSL → biloba:xpath
b.Click(b.WithText("Submit"))            // sugar for b.XPath().WithText(...) — any element by exact text
b.Click(b.WithTextContains("Sub"))       // ...by substring
```

Prefer text/role selectors for things a user names by label; fall back to ids/`data-*` for the rest. Never fetch-then-act — always pass the selector *into* the action so find-and-act is one atomic JS snippet.

**Piercing shadow DOM / iframes** with the CSS-only `>>>` combinator (one boundary per `>>>`, open shadow roots and same-origin iframes only):

```go
b.Click("my-widget >>> button.submit")
Eventually("#editor-frame >>> .toolbar .save").Should(b.Click())
```

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

Stubs are per-tab and reset by `Prepare()`. Observe requests with `Eventually(b).Should(b.HaveMadeRequest(...))` and wait for quiet with `Eventually(b).Should(b.BeNetworkIdle())`.

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

Drop to chromedp via `b.Context` (real `:hover`, cross-origin frames, geolocation, anything CDP). See the escape hatch in `biloba:overview`. For real keystrokes use `b.Type`/`b.SendKeys` rather than `SetValue`.

Propose opening an issue if a common pattern is missing.

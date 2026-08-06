---
name: overview
description: The Biloba mental model for writing browser tests in your own Ginkgo/Gomega suite — the three principles and the consequences they have for how you write specs (pragmatic simulation, poll-by-default, drop-to-chromedp). Use this first when you start working with Biloba in a project, or to decide whether Biloba fits a testing task. Routes to the other biloba:* skills.
---

# Biloba: the mental model

Biloba is a browser-testing framework for Go, built on [chromedp](https://github.com/chromedp/chromedp), for **Ginkgo** and **Gomega**. You don't `. import` it — you drive everything through a `*biloba.Biloba` value (conventionally `b`). Canonical docs: <https://onsi.github.io/biloba/> — pin to the version you `go get`'d (the API may shift pre-1.0).

## The three principles — and what they mean for your specs

**1. Performance via parallelization.** One shared Chrome process drives one isolated *root tab* (`b`) per Ginkgo parallel process, reused between specs via `b.Prepare()`.

- Run with `ginkgo -p`. Specs must be independent; Biloba's per-tab isolation makes that cheap.
- `b` is special: never closed, reused spec-to-spec. `b.NewTab()` tabs and spawned tabs are closed by `Prepare()`. → `biloba:setup`

**2. Stability via pragmatism.** Biloba prefers a good-enough *simulation* run atomically in the browser over a realistic emulation across async round-trips. A click is `element.click()` after synchronous visibility/enabled checks — no scroll-into-view, no centroid, no occlusion test. This **fast track** is the default `b`. Consequences to internalize:

- **Visibility = non-zero `offsetWidth`/`offsetHeight`.** The fast track won't catch an element hidden *behind* another or off-screen. Use `b.HaveComputedStyle` for explicit style assertions, or `b.BeClickable()` to assert topmost-at-its-center.
- **`b.SetValue` sets the value and fires `input`/`change` — it does not type.** Apps wired to real key events (search-as-you-type, rich text, hotkeys) need `b.Type`.
- **`b.Hover` fires pointer/mouse events but does not activate CSS `:hover`.**
- **There are two interaction tracks.** For the handful of specs that need real clicks through occlusion, scroll-into-view, CSS `:hover`, or drags, opt into `b.Realistic()` — a view of the *same tab* routed through real Chrome DevTools Protocol input. Per-spec opt-in (it costs round-trips and can reintroduce timing flake). → `biloba:realistic-mode`

**3. Conciseness via Ginkgo and Gomega.**

- **Most methods don't return errors** — errors become Ginkgo test failures for you.
- **Biloba polls by default.** A fully-applied call (`b.Click("#go")`, `b.GetProperty(sel, "href")`) polls until the element is ready, acts/reads once, then stops. The under-applied form returns a Gomega matcher *you* wrap in `Eventually`/`Consistently`. This dual API is the core pattern — learn it in `biloba:write-tests`. (`b.Immediate()` opts back into act-once/fail-fast; rarely needed.)
- `console.log` streams to the `GinkgoWriter`; a failing `console.assert` fails the spec.

## The one habit that keeps suites non-flaky

**Never assert on a value you read exactly once.** A browser is a pile of async settles (a WS frame, a layout pass, an rAF-injected node, an optimistic→server reconciliation), and any single read can land before the thing you care about settles. Biloba's poll-by-default actions and getters handle this — the residual flake sources are the few things that *don't* poll. Four reflexes:

- **`b.Run` reads don't poll — wrap them.** `b.Run(expr, &x); Expect(x)` is a single-shot read (`Run` stays fail-fast on purpose — a thrown JS error is usually a real bug). Write `Eventually(b.Run).WithArguments(expr).Should(matcher)` instead; numbers decode to `float64`, so use `BeNumerically`.
- **Don't hand-roll geometry in `b.Run` either.** Layout/`getBoundingClientRect`/computed-style reads settle *after* an element exists, so they must be polled even once it's there — and Biloba has layout-aware natives for nearly all of them (`b.GetBoundingBox`/`GetScrollOffset`/`GetOffsetTopWithin` + `Have*` matchers, the pairwise `b.BeAbove`/`Encloses`/`Overlaps`, `b.BeInViewport()`, `b.BePrecededBy`/`BeFollowedBy`, `b.GetComputedStyle`). → `biloba:api`
- **Don't reach for `b.Immediate()`.** The default already polls; reaching for it reintroduces the classic "raced a frame, failed downstream" flake. The few methods that genuinely can't poll (`SendKeysToWindowImmediately`, the `*Immediately` plural verbs) carry the smell in their names — gate them by hand. → `biloba:flaky-specs`
- **Don't assert with a matcher and then re-read the value with a getter** — two reads of a page that may have changed in between. Every value-reading matcher takes `.Capture(&x)`: `Eventually(sel).Should(b.HaveAttribute("data-id", Not(BeEmpty())).Capture(&id))` hands you the value from the read that actually satisfied the assertion, decoded into your Go type. → `biloba:write-tests`

**If your app renders optimistically, both obvious signals lie.** The DOM shows the pre-confirmation state (the click handler wrote it synchronously), so `Eventually` on it just re-reads the optimistic copy; a Go-side HTTP read bypasses the browser's event loop and proves only that the *server* persisted something. Barrier on **app state** — `b.GetJSValue("window.__storeLog", &log)` polls a path the app writes, which can only become true if the renderer applied the response — and force the arrival order with `b.HoldResponse(url)` instead of hoping to hit a 1%-natural race. (`GetJSValue` gates on *definedness*, so it only barriers on a path the app creates **lazily**, in the handler you're waiting for; against an eagerly-created log poll the predicate: `Eventually(expr).Should(b.EvaluateTo(...))`.) → `biloba:flaky-specs`

When a spec is flaky, order-dependent, or only fails under `-p`/CI, go straight to `biloba:flaky-specs`.

## Selectors are first-class — three pathways

Every action/matcher takes:

- a **CSS string** — the default; target stable `#id`/`[data-testid]` hooks, not styling classes. `>>>` pierces open shadow roots / same-origin iframes.
- a **semantic `Locator`** — describes an element as a user perceives it (`b.ByRole("button").WithName("Save")`, `b.ByText`, `b.ByLabel`, `b.ByTestID`). Reach for these to assert a11y or when the visible label is the natural identifier. They compose (`.ContainingText`/`.Containing`/`.And`/`.Or`/`.Within`/`.Nth`, each accepting any selector) and pierce open shadow roots automatically. `b.ByCSS(sel)` takes raw CSS *into* that algebra (`b.ByCSS(".story").Nth(1)` for the 2nd match, instead of `:nth-of-type`).
- an **`XPath`** — the rare power tool for axis/ordinal queries.

→ `biloba:write-tests`, `biloba:xpath`

## The escape hatch

Biloba deliberately does not hide chromedp. Every tab exposes `b.Context`:

```go
chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
    return emulation.SetGeolocationOverride().WithLatitude(48.8584).WithLongitude(2.2945).Do(ctx)
}))
```

Use it for geolocation, cross-origin frames, or any CDP feature without a native wrapper. (For real `:hover`/occlusion/scroll, prefer `b.Realistic()`.)

## Where to go next

| Task | Skill |
|---|---|
| Wiring Biloba into a project (bootstrap, `chrome-headless-shell`) | `biloba:setup` |
| Authoring specs (dual API, locators, interactions, hermetic tests, multi-tab) | `biloba:write-tests` |
| Looking up a method or matcher | `biloba:api` |
| Realistic interactions (occlusion, `:hover`, drag, scroll, touch) | `biloba:realistic-mode` |
| Building XPath selectors | `biloba:xpath` |
| Testing a page/app you haven't seen | `biloba:explore-unfamiliar-page` |
| A spec failed and you want to see why | `biloba:debug-failures` |
| A spec is flaky / order-dependent / only fails under `-p` or CI | `biloba:flaky-specs` |

---
name: overview
description: The Biloba mental model for writing browser tests in your own Ginkgo/Gomega suite — the three principles and the consequences they have for how you write specs (pragmatic simulation, never-polls, drop-to-chromedp). Use this first when you start working with Biloba in a project, or to decide whether Biloba fits a testing task. Routes to the other biloba:* skills.
---

# Biloba: the mental model

Biloba is a browser-testing framework for Go that builds on [chromedp](https://github.com/chromedp/chromedp) to bring fast, stable, automated browser testing to **Ginkgo** and **Gomega**. It is unapologetically Ginkgo/Gomega-native: you don't `. import` it — you drive everything through a `*biloba.Biloba` value (conventionally `b`).

Read the canonical narrative docs at <https://onsi.github.io/biloba/> for the full story; pin to the version you `go get`'d (the API may shift pre-1.0). This skill is the orientation; the other skills go deep.

## The three principles — and what they mean for your specs

**1. Performance via parallelization.** One shared Chrome process drives one isolated *root tab* (`b`) per Ginkgo parallel process, reused between specs via `b.Prepare()`. The practical upshots:
- Run your suite with `ginkgo -p`. Specs must be independent (Ginkgo's model) — Biloba's per-tab isolation makes that cheap.
- `b` is special: never closed, reused spec-to-spec. New tabs (`b.NewTab()`) and spawned tabs are closed by `Prepare()`. → `biloba:setup`.

**2. Stability via pragmatism.** Biloba favors a good-enough *simulation* run atomically in the browser over a realistic emulation across async round-trips. A click is `element.click()` after synchronous visibility/enabled checks — no scroll-into-view, no centroid, no occlusion test. This **fast track** is the default `b`. The consequences you must internalize:
- **Visibility = non-zero `offsetWidth`/`offsetHeight`.** The fast track won't catch an element hidden *behind* another or off-screen. Use `HaveComputedStyle` for explicit style assertions, or `BeClickable()` to assert topmost-at-its-center.
- **`SetValue` sets the value and fires `input`/`change` — it does *not* type.** Apps wired to real key events (search-as-you-type, rich text, hotkeys) need `b.Type`/`b.SendKeys`. → `biloba:write-tests`.
- **`Hover` fires pointer/mouse events but does not activate CSS `:hover`.**
- **There are two interaction tracks.** When a handful of specs genuinely need realism (real clicks through occlusion, scroll-into-view, CSS `:hover`, drags), opt into the **realistic track** with `b.Realistic()` — a view of the *same tab* that routes interactions through real Chrome DevTools Protocol input. It's per-spec opt-in (it costs round-trips and can reintroduce timing flake), so the bulk of your suite stays on the fast track. → `biloba:write-tests`. For cross-origin frames / geolocation / any other CDP feature, drop to chromedp (escape hatch below).

**3. Conciseness via Ginkgo and Gomega.**
- **Most methods don't return errors** — errors become Ginkgo test failures for you.
- **Biloba never polls.** Methods either act immediately *or* return a Gomega matcher that *you* wrap in `Eventually`/`Consistently`. This dual immediate/matcher API is the single most important pattern — learn it in `biloba:write-tests`.
- `console.log` streams to the `GinkgoWriter`; a failing `console.assert` fails the spec.

## The one habit that keeps suites non-flaky

**Never assert on a value you read exactly once.** A browser is a pile of async settles (a WS frame, a layout/measure pass, an rAF-injected node, an optimistic→server reconciliation); any single read can land before the thing you care about settles. This is the root of almost every Biloba flake. Three reflexes follow:

- **Poll reads, don't snapshot them.** `b.Run(expr, &x); Expect(x)` is a single-shot read — wrap it: `Eventually(b.Run).WithArguments(expr).Should(matcher)` (numbers decode to `float64` → `BeNumerically`). Geometry / `getBoundingClientRect` / computed-style reads settle *after* an element exists, so they must be polled even once it's there.
- **Immediate interactions race silently and fail *later*.** A fully-applied `b.Click(sel)`/`b.Tap(sel)` fired a frame too early no-ops — and the spec fails downstream at the dependent assertion, not at the interaction. Gate on a readiness anchor or use the matcher form `Eventually(sel).Should(b.Click())`.
- **If your app renders optimistically, the DOM lies.** It shows the pre-confirmation state, and `Eventually` on it just re-reads the optimistic copy — wait on a server-authoritative signal instead.

When a spec is flaky, order-dependent, or only fails under `-p`/CI, go straight to `biloba:flaky-specs`.

**Selectors are first-class — three pathways.** Any action/matcher takes a **CSS string** (the default — target stable `#id`/`[data-testid]` hooks, not styling classes), a **semantic `Locator`** that describes an element as a user perceives it (`b.ByRole("button").WithName("Save")`, `b.ByText(...)`, `b.ByLabel(...)`, `b.ByTestID(...)` — reach for these to assert a11y or when the visible label is the natural identifier), or an **`XPath`** (the rare power tool for axis/ordinal queries). Locators compose (`.ContainingText`/`.Containing`/`.And`/`.Or`/`.Within`/`.Nth`, accepting any selector) and pierce open shadow roots automatically. → `biloba:write-tests`, `biloba:xpath`.

## The escape hatch

Biloba deliberately does not hide chromedp. Every tab exposes `b.Context` (a `chromedp` context), so anything Biloba doesn't wrap natively you can do directly:

```go
chromedp.Run(b.Context, chromedp.ActionFunc(func(ctx context.Context) error {
    return emulation.SetGeolocationOverride().WithLatitude(48.8584).WithLongitude(2.2945).Do(ctx)
}))
```

Reach for it for geolocation, cross-origin frames, or any CDP feature without a native wrapper. (For real `:hover`/occlusion/scroll, prefer `b.Realistic()` — see principle 2.)

## Where to go next

- **Wiring Biloba into a project** (bootstrap, `chrome-headless-shell`, bootstrap variations) → `biloba:setup`
- **Authoring specs** (the dual API, semantic locators, the interaction vocabulary, hermetic tests, multi-tab) → `biloba:write-tests`
- **Realistic interactions** (occlusion, CSS `:hover`, drag, scroll, touch — the `b.Realistic()` track) → `biloba:realistic-mode`
- **Building XPath selectors** with the DSL → `biloba:xpath`
- **Looking up a method or matcher** → `biloba:api`
- **Testing a page/app you haven't seen** → `biloba:explore-unfamiliar-page`
- **A spec failed and you want to see why** → `biloba:debug-failures`
- **A spec is flaky / order-dependent / only fails under `-p` or CI** → `biloba:flaky-specs`

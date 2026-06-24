---
name: realistic-mode
description: Use Biloba's realistic interaction track (b.Realistic()) when a spec must exercise the realism the fast default trades away — clicking through/around an occluding overlay, a menu that opens on CSS :hover, scroll-into-view, a pointer drag (@dnd-kit/Sortable), real wheel scrolling, or touch. Covers what each interaction track actually does (the fast-vs-realistic capability matrix), the inline/per-spec/per-suite (Label) patterns, when NOT to use it, and BeClickable() as a cheaper occlusion guard. Use when testing occlusion/hover/drag/scroll-sensitive flows or deciding fast vs realistic.
---

# Realistic interactions

Biloba has **two interaction tracks** on the same tab:

- **Fast track (default `b`)** — atomic JavaScript *simulations*: a click is `element.click()` after synchronous visibility/enabled checks. No scroll, no occlusion test, no real `:hover`. Fast and stable — what you want for the overwhelming bulk of specs (see `biloba:overview` principle 2).
- **Realistic track (`b.Realistic()`)** — a `*Biloba` view of the **same tab** whose interactions run through **real Chrome DevTools Protocol input**.

```go
rb := b.Realistic()
rb.Click("#submit")              // scrolls into view, waits for stability, refuses to click through an overlay, real mouse click
Eventually(".menu").Should(rb.Hover())  // moves the real pointer → CSS :hover activates
```

`b.Realistic()` shares the tab's connection and state — it's the *same tab*, just routed through CDP. The default `b` is untouched. Full story: <https://onsi.github.io/biloba/#realistic-interactions>.

## When to reach for it

Quarantine it to a **handful of smoke tests** that guard realism the fast track can't see. It costs real round-trips and can reintroduce the timing flake the atomic model avoids — that's the deliberate, opt-in cost.

| Symptom you want to test | Why the fast track misses it |
|---|---|
| A click must route **around an occluding overlay** | Fast `Click` calls `el.click()` and clicks straight through overlays |
| A menu/tooltip opens on **CSS `:hover`** | Fast `Hover` fires JS pointer events but does not activate CSS `:hover` |
| Element is **off-screen / below the fold** and must scroll into view | Fast track never scrolls |
| A **pointer drag** (@dnd-kit, Sortable, custom DnD) | needs real `pointerdown`/`move`/`up` |
| Real **wheel scrolling** of the page, or **touch** | needs trusted CDP input |

If you only need to *assert* an element isn't occluded (not drive a realistic interaction), prefer the cheaper deterministic matcher `Eventually(sel).Should(b.BeClickable())` (visible + enabled + topmost-at-its-center) — no realistic round-trips.

Realistic mode does **not** help with cross-origin frames or geolocation — drop to chromedp via `b.Context` for those (`biloba:overview`).

## What each track does (capability matrix)

Selection is track-agnostic (`b.ByRole`/`ByText`/`ByLabel`, CSS, `>>>`, XPath work identically through either handle). The interactions differ:

| Interaction | Fast track (`b`) | Realistic track (`b.Realistic()`) |
|---|---|---|
| `Click` | `el.click()`, no scroll/occlusion test | scroll to center, wait for stability, verify enabled + **topmost** (no click-through), real mouse press/release |
| `DblClick`/`RightClick`/`MiddleClick` | synthetic events (`dblclick`/`contextmenu`/`auxclick`) | scroll + stability + occlusion + real button input (native context menu fires) |
| `Hover` | JS pointer/mouse events; **no** CSS `:hover` | moves the **real pointer** → CSS `:hover` activates |
| `SetValue` | sets value, fires `input`/`change` (no typing) | text inputs: real click → clear → real keystrokes → blur; checkboxes: real click. Native pickers (radio/`<select>`/multi) fall back to fast JS |
| `Type`/`SendKeys` | real CDP key events already | additionally scrolls into view first |
| pointer options `b.At(x,y)`/`b.Shift()`… | any option switches a click off native `el.click()` to a synthetic event carrying coords+modifier flags | real CDP input honoring the offset (translated, bounds-checked) + modifier bitmask |
| `DragTo` | `pointerdown`/`move`/`up` events | real CDP mouse drag (scrolls + checks both ends) |
| `ScrollWheel` | synthetic `wheel` + manual ancestor scroll | real CDP wheel — genuine trusted input, scrolls the page |
| `Tap` | synthetic touch/pointer + `click` | real CDP `touchStart`/`touchEnd` |

The whole vocabulary (`DblClick`, `RightClick`, `MiddleClick`, pointer options `b.At`/`b.Shift`/`b.Ctrl`/`b.Alt`/`b.Meta`, `DragTo`, `ScrollWheel`, `Tap`, `Type`/`SendKeys`) is in `biloba:write-tests` and `biloba:api`.

**Scroll-into-view lives only on this track** (plus the focus-bearing `SetValue`/`Type`/`SendKeys`, whose `.focus()` scrolls). A *fast* `Click`/`Tap` never moves the page — so if a scroll/layout spec needs the viewport held still, stay on the fast track; and if a scroll position shifts around a fast click, the cause is app-side, not Biloba (a real diagnosis trap — see `biloba:flaky-specs`).

## The three composition patterns

There is deliberately **no per-call decorator** (Biloba's dual API keys on argument count; a realism flag would muddy that). The `b.Realistic()` handle is the one seam, and because it's just a `*Biloba` view it flows through helpers and `Eventually` exactly like `b`:

```go
// 1. Inline — the handle is cheap to make
b.Realistic().DragTo("#card", "#done-column")

// 2. Per-spec — grab one handle, use it throughout
It("opens the hover menu", func() {
    rb := b.Realistic()
    Eventually(".nav-item").Should(rb.Hover())
    Eventually(".nav-item .submenu").Should(b.BeVisible())
    Eventually(b.ByRole("menuitem").WithName("Settings")).Should(rb.Click())
})

// 3. Per-suite — swap the tab in a BeforeEach, gated on a Ginkgo Label
var _ = Describe("checkout (realistic smoke)", Label("realistic"), func() {
    var rb *biloba.Biloba
    BeforeEach(func() { rb = b.Realistic() })

    It("won't submit through the consent overlay", func() {
        Consistently(".overlay").Should(b.BeVisible())
        // rb.Click fails (occluded) until the overlay is dismissed — exactly what we want to guard
    })
})
```

With the label, `ginkgo --label-filter='realistic'` runs only the realistic lane and `--label-filter='!realistic'` keeps the slow/flake-prone realism checks out of the fast inner loop.

## Pitfalls

- **Don't realistic-mode the whole suite.** It defeats Biloba's performance and stability story; reserve it for smoke tests.
- A realistic interaction on an occluded/off-screen element **polls and fails** like a real interaction — that's the feature, but it means realistic specs are more timing-sensitive. Lean on the matcher form (`Eventually(sel).Should(rb.Click())`) so readiness-waiting is built in.
- `DragTo` drives **pointer-based** DnD libraries (@dnd-kit, Sortable), **not** native HTML5 `draggable` (which uses a separate drag-event model).
- `Focus` stays a plain JS focus even on the realistic track (matching how real engines focus without a side-effecting click).

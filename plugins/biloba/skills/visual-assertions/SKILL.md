---
name: visual-assertions
description: Assert that a page or element still looks right with Biloba's visual regression matcher (b.HaveScreenshot) — writing the assertion, the golden-master workflow for creating and updating committed baselines with BILOBA_UPDATE_SCREENSHOTS=1 (review the .actual.png, update mode settles to three consecutive equal captures before writing so a write is not instantaneous, the actionable "never settled" warning, commit the baselines dir, never set the var in CI, nothing prunes orphaned baselines), reading the text diagnosis of a failed comparison without opening an image, and the determinism tools (b.Mask for timestamps/avatars, the automatic animation freeze and b.Animated() to opt out, b.Tolerance/b.ChannelTolerance, b.InColorSchemes for light+dark). Also covers the two directories (committed baselines vs gitignored actual/diff artifacts), the ways a visual assertion can go silently vacuous (a subject clipped out of its own capture by an inner scroll container, two colour schemes that render identically), and the hazards Biloba does not solve (scrollbars, cross-platform font rendering, closed shadow roots, a JS pulse with only two or three renderings, a late one-shot change that is not a web font). Use when a spec needs to assert appearance, when a HaveScreenshot comparison failed, or when setting up visual regression in a suite.
---

# Visual assertions

`b.HaveScreenshot(name)` compares the subject against a **committed baseline PNG**. Always under `Eventually` — it is a bare matcher and every attempt captures and compares afresh.

```go
Eventually(".librarian").Should(b.HaveScreenshot("librarian-rail"))  // one element, clipped to its box
Eventually(b).Should(b.HaveScreenshot("home-desktop"))               // the whole page
Eventually(".card").Should(SatisfyAll(b.BeInViewport(b.Fully()), b.HaveScreenshot("card")))
```

Docs: <https://onsi.github.io/biloba/#visual-assertions>.

In TypeScript, use `session.expectScreenshot(...)` or `locator.expectScreenshot(...)`. The baseline, update, masking, tolerance, freeze, color-scheme, artifact, and diagnosis behavior is the same; see `biloba:typescript` for TypeScript option names.

**Give it a real deadline.** Gomega's default `Eventually` is 1s and a capture-and-compare is not free:

```go
Eventually(b).WithTimeout(10 * time.Second).Should(b.HaveScreenshot("home-desktop"))
```

Configuring the matcher (`b.WithTimeout(d).HaveScreenshot(...)`) is a hard error — configure the `Eventually`.

**Why the poll is the whole design.** A screenshot is the most layout-sensitive assertion in a suite (fonts loading, images decoding, a `ResizeObserver` re-applying, an rAF loop settling). Polling the *comparison* means the first passing attempt is by construction the settled one — don't hand-roll a barrier before it.

## The baseline workflow

A baseline is a golden master. The workflow exists so that a human approves it once, deliberately.

**Creating one**

1. Write the assertion, run the suite normally. **The first run FAILS** — a missing baseline is a loud failure that writes the captured image to the artifacts dir and prints its path. Biloba never writes-and-passes (a spec that has never compared anything reads green in CI).
2. **`Read` that `.actual.png` and decide whether it is correct.** This is the sign-off on the golden master, and the one moment anyone is guaranteed to look at the image. Don't skip it on the user's behalf — look, then say what you saw.
3. Write the baseline:
   ```bash
   BILOBA_UPDATE_SCREENSHOTS=1 ginkgo -r -p
   ```
   **This is not instantaneous** — see *Update mode settles first* below. Budget ~0.5s per assertion, up to ~2.2s of waiting when a page is slow to settle. Don't treat a slow update run as a hang.
4. `git add` the baselines directory and commit it. An uncommitted baseline regenerates from the current build on every machine, so it always matches and the assertion asserts nothing.

**Updating one after an intentional change**

1. The normal run fails with the diagnosis. Read it (and the `.diff.png`) and confirm the change is the one that was intended.
2. Re-run with `BILOBA_UPDATE_SCREENSHOTS=1`.
3. It prints one line saying what changed. Put that line in the PR description — git shows a changed PNG as a binary blob, so the sentence is the only reviewable part:
   ```
   screenshot "home-desktop" updated — 38,160 of 1,017,600 pixels changed (3.75%), one box at (0,14)-(1272,44)
   ```

**Three operational facts**

- **Update mode is suite-wide.** It rewrites every baseline the run touches, not just the one you were looking at. Scope an intentional update with `ginkgo --focus="…"` or a label instead of rewriting everything.
- **Never set `BILOBA_UPDATE_SCREENSHOTS` in CI.** With it set every visual assertion captures and passes, so the whole visual suite goes green without comparing anything. → `biloba:flaky-specs` §7
- **Nothing prunes orphaned baselines.** Biloba reads and writes baselines by name and never deletes one. A removed spec leaves its PNG in the repo forever, and a rename produces a *second* baseline rather than moving the first. Delete the old file by hand.

**Spelling the variable.** `1/t/true/y/yes/on` turn it on; `0/f/false/n/no/off` and unset turn it off. Case-insensitive, whitespace trimmed. Anything else is treated as off **and warns** — `BILOBA_UPDATE_SCREENSHOTS is set to "sure", which Biloba does not recognise as a boolean - treating it as off.` If an update run wrote nothing, check that line before anything else.

**Never pre-write baselines blindly** — no scripted "just run update until green", no committing whatever the first run produced without opening it. That recreates the vacuous assertion the first-run failure exists to prevent.

### Update mode settles first

Ordinary assertions settle because the `Eventually` polls. Update mode has no `Eventually` behind it — it passes on the first attempt — so it does its own settling: it captures repeatedly until **three captures in a row compare equal** (under the configured tolerance) and writes that one. Otherwise a baseline caught mid font-load gets baked in, and every later run polls for a rendering that never returns.

The pauses between captures grow and are all different lengths (100ms, 140, 200, 280, 380, 500, 640), bounded at 8 captures. A *fixed* cadence is the problem: sample a 160ms animation every 160ms and every pair matches. Same "consecutive equal reads" rule Biloba uses to decide an element has stopped moving in realistic mode.

**A "never settled" warning is actionable, not noise.** If the streak never happens Biloba writes the last capture anyway and prints:

```
The screenshot for home-desktop never settled: no 3 captures in a row matched, across 8 captures over 2.6s.
Biloba wrote the last one, but a baseline captured from a page that is still changing will fail on every later run.
Mask the changing region with b.Mask(...), or track down what is still moving.
```

Do what it says — add a `b.Mask(...)` for the moving region (or fix the page) and re-run the update. **Do not just re-run the update hoping for a clean pass**: the baseline it wrote is unsettled, and the next normal run will fail against it. It's a warning rather than a failure because that next run is the right place to enforce it; the warning is the part that explains *why* it will fail.

## Two directories

| | Where | Lifecycle |
|---|---|---|
| **Baselines** `<name>.png` | `./biloba-baselines` (`BilobaConfigScreenshotBaselinesDir` / `BILOBA_SCREENSHOT_BASELINES_DIR`) | few, small, reviewed, **committed** |
| **Artifacts** `<name>.actual.png`, `<name>.diff.png` | the failure-screenshots dir, `./biloba-screenshots` (`BILOBA_SCREENSHOTS_DIR`) | written on failure, **gitignored** |

```
# .gitignore
biloba-screenshots/
```

Baseline names may contain `/` (`"checkout/step-2"` → `biloba-baselines/checkout/step-2.png`); artifact filenames are flat (`checkout_step-2.actual.png`).

## Reading a failure without opening an image

```
screenshot "home-desktop" differs from baseline
  38,160 of 1,017,600 pixels differ (3.75%), max channel delta 221
  changed region: one box, (0,14)-(1272,44)  [100% of the width, 4% of the height, at its top edge]
  unchanged: everything below y=44
  baseline: /Users/you/app/biloba-baselines/home-desktop.png
  actual:   /Users/you/app/biloba-screenshots/home-desktop.actual.png
  diff:     /Users/you/app/biloba-screenshots/home-desktop.diff.png
```

| Shape line | Means | Do |
|---|---|---|
| `one box`, full width, top edge | the header/banner changed, nothing else | look at the header markup, not the component under test |
| `one box`, small, mid-image | the component you actually touched | read the `.diff.png`; likely a real, intended change |
| `changed regions: N boxes` (each listed, largest first, capped at 5 + `… and N more`) | several independent changes | check whether they share a container |
| `scattered — N regions spread across the image` | every text run moved a little — a web font failed to load or rendered differently | check font loading; if it's Mac↔Linux, see the platform rule below |
| `uniform shift of the whole image, 1px down (dx=0, dy=1)` | nothing changed *inside* the subject; something above/before it grew or moved | fix the thing above; don't re-baseline |
| `baseline is 800x600, actual is 800x640 (40px taller)` | the box itself resized — no per-pixel story | a layout change, not a paint change |

- `unchanged: everything below y=44` is the complement and often the faster read. Printed only when a clear majority is untouched; silent on a shift or a scattered change.
- `max channel delta` counts **all** pixels, including ones the channel tolerance absorbed — it tells you whether the tolerance is doing its job or is one notch from hiding a real change.
- **When that number is in the low single digits Biloba says so in words**: `every differing pixel differs by <= 3 — a rasterisation or compositing difference, not a content change`. That decides your whole response. Don't go hunting for an element that moved; nothing moved. Look for a composited shadow or a dithered gradient bleeding into the capture, and absorb it with `b.ChannelTolerance` at that call site with the measured amplitude written there.
- The `.diff.png` is the actual, washed out, with every differing pixel in magenta. `Read` it when the words aren't enough. A human running this in a terminal that renders images gets that diff drawn under the diagnosis; you get the path, because inline images are off under an agent. The words above are printed to both.
- Every verdict is deliberately conservative — when no signature holds you get the plain box list. The **shift** verdict additionally never fires on an image thinner than ~16px on either axis (a thin rule, a focus ring, a slim progress bar captured as an element), because at that size the search describes itself rather than the page. Those get the box reading.

Same idea as the [poll trajectory](https://onsi.github.io/biloba/#outline): the words are the diagnosis, the image is the evidence. → `biloba:debug-failures`

## Determinism: which tool for which hazard

| Hazard | Tool |
|---|---|
| A region that legitimately changes every run (relative timestamp, avatar, ad slot, build hash) | `b.Mask(sel...)` |
| CSS animation / transition / blinking caret / smooth scroll | **automatic** — Biloba freezes rendering for the capture, inside open shadow roots too |
| A page still moving when a baseline is written | **automatic in update mode** — three consecutive equal captures before the write |
| A web font swapping in late | **automatic** — every capture awaits `document.fonts.ready` |
| Unstable pixels that belong to no element (a composited shadow, a dithered gradient) | `b.ChannelTolerance(n)` — `b.Mask` takes selectors and cannot express this |
| You are asserting *on* an animation and made it deterministic yourself | `b.Animated()` opts out of the freeze |
| Antialiasing wobble between runs on the same machine | `b.ChannelTolerance(n)` (suite-wide: `BilobaConfigScreenshotChannelTolerance`) |
| A handful of pixels anywhere | `b.Tolerance(fraction)` (suite-wide: `BilobaConfigScreenshotTolerance`) |
| Light and dark themes | `b.InColorSchemes("light", "dark")` |
| A JS pulse with only 2–3 distinct renderings | **not solved** — see below |
| A subject scrolled out of an inner `overflow:auto` pane | **refused** — scroll the pane first, see below |
| A late one-shot change that is *not* a font (lazy image decode, late `ResizeObserver`) | **not solved** — gate it yourself before generating |
| Closed shadow roots | **not solved** — unreachable from script by design |
| Scrollbars | **not solved** — see below |
| Fonts across machines | **not solved** — see below |

```go
Eventually(b).Should(b.HaveScreenshot("dashboard", b.Mask(".relative-timestamp", b.ByTestID("avatar"))))
```

Masking paints the matched elements flat gray **before the baseline is written and before every comparison**, so both sides are masked identically. A mask selector that matches nothing is a no-op. Any Biloba selector works (CSS, `XPath`, `Locator`).

The freeze injects `animation: none; transition: none; caret-color: transparent; scroll-behavior: auto` for the duration of the capture and removes it after — `none`, not paused, because pausing freezes each animation at an arbitrary frame. It goes into **every open shadow root** as well as the document (a `*` rule in a document stylesheet doesn't cross a shadow boundary, so web components would otherwise animate straight through the capture) and is removed from all of them. **Closed** shadow roots can't be reached from script; a component using one is left animating, which is a likely cause when its capture won't settle.

**A JS pulse with 2–3 renderings (not solved).** The three-consecutive-equal-captures rule defeats anything animating on a fixed period, but a page alternating between two frames can match three times by chance, and no sampling schedule fixes that. CSS animations, transitions, and the caret are already frozen, so what's left is a hand-rolled `setInterval`/canvas redraw. The backstop is the never-settled warning plus the next run failing — mask the region, don't re-run the update.

**A capture expands the viewport only when it must — keep it that way.** Reaching outside the viewport means resizing it, and a responsive page observes that: `matchMedia` flips, an app re-renders on its breakpoint, and the subtree being captured can unmount mid-capture. Biloba skips the expansion for a subject already fully in view, and for a full-page capture of a document that fits the viewport (the app-shell case). **Prefer captures of things already on screen** — scroll first, gate on `b.BeInViewport(b.Fully())`, then capture. If a subject vanishes across a capture Biloba says so; treat that message as pointing at the capture, not at the assertion that failed after it.

**A subject inside an inner scroller (refused, with instructions).** An element capture works below the **document** fold — Biloba expands the main frame's viewport. It does nothing for an **inner** `overflow: auto` pane, which is how most app shells scroll. An element outside that pane's visible band was never painted, so the capture is a flat rectangle of pane background — and blank is *stable*, so as a baseline it would pass forever while comparing nothing. Structural gates don't catch it: `[data-rendered]` and `svg` are both present in a blank capture. Biloba refuses the comparison (and refuses to write the baseline), naming the clipping ancestor:

```go
b.ScrollIntoView(".figure", b.WithinScroller("#reader-pane"))
Eventually(".figure").Should(b.BeInViewport(b.Fully()))
Eventually(".figure").Should(b.HaveScreenshot("figure"))
```

A *partly* clipped subject warns and still compares — capturing something that straddles a pane edge is occasionally deliberate. An `overflow: hidden` ancestor that isn't cutting the subject off (a card clipping its own rounded corners) is not reported.

**Triaging that partial-clip warning.** It reports how much was painted, not whether the rest could ever have been — and that's the difference between a fact about the design and a bug. Compare the subject's height against the pane's visible band (`b.GetBoundingBox` on each; `b.GetScrollOffset` on the pane says where it currently sits):
- **Subject taller than the band** → no scroll position paints all of it. The warning is permanent and descriptive; capture a smaller subject, or accept the crop knowing what the baseline covers.
- **Subject fits the band** → the missing part is reachable and the pane is just scrolled elsewhere. Fix it: `b.ScrollIntoView(subject, b.WithinScroller(pane))`, gate on `b.BeInViewport(b.Fully())`, and the warning goes away.

**The poll protects the comparison, not the write.** An assertion re-captures every attempt and rides out a late change; a baseline write passes on the first settled capture and has no second chance. The settle defeats anything *periodic* but cannot see a one-shot change that hasn't started — three captures agree because nothing has happened yet. Fonts are handled (`document.fonts.ready` is awaited before every capture, on both paths); anything else that lands late is yours to gate before generating.

**Scrollbars (not solved).** Overlay vs classic scrollbars change layout width, and that varies by platform, by OS setting, and on macOS by whether a mouse is attached. Prefer **element** captures over whole-page ones. If you need the page, hide them yourself: `::-webkit-scrollbar { display: none }` in the page, or `emulation.SetScrollbarsHidden(true)` via `b.Context`.

**Fonts across machines (not solved).** Subpixel antialiasing, hinting, and fallback differ between macOS and Linux — the same page produces a scattered diff in every glyph. **A baseline belongs to the platform that produced it.** Either generate baselines on the platform CI runs on, or split the directory:

```go
b = biloba.ConnectToChrome(GinkgoT(),
	biloba.BilobaConfigScreenshotBaselinesDir(filepath.Join("baselines", runtime.GOOS)),
)
```

Tolerance absorbs same-platform antialiasing. It does **not** absorb Mac↔Linux — the tolerance that would is large enough to absorb real changes.

## Tolerance: two independent axes

- `b.ChannelTolerance(delta)` — a pixel counts as differing only when an R/G/B/A channel differs by **more than** `delta`. The antialiasing absorber.
- `b.Tolerance(fraction)` — at most `fraction` (0..1) of the compared pixels may differ.

Both default to `0` (exact). Channel tolerance filters; fraction tolerance counts what's left.

**Set the default suite-wide, override per assertion only where one comparison genuinely needs different slack.** A number repeated at two hundred call sites is a suite nobody can reason about — and a tolerance widened until nothing can fail is an assertion that cannot fail (`biloba:flaky-specs` §7).

```go
b = biloba.ConnectToChrome(GinkgoT(),
	biloba.BilobaConfigScreenshotTolerance(0.001),
	biloba.BilobaConfigScreenshotChannelTolerance(4),
)
Eventually(".prose").Should(b.HaveScreenshot("prose", b.ChannelTolerance(8)))  // this one only
```

## Light + dark in one assertion

```go
Eventually(b).Should(b.HaveScreenshot("home", b.InColorSchemes("light", "dark")))
```

Captures and compares once per scheme with `prefers-color-scheme` emulated, and **all** must match. Baselines are `home-light.png` and `home-dark.png`; a failure names the scheme (`home (prefers-color-scheme: dark)`). Without the option Biloba doesn't emulate `prefers-color-scheme` at all and stores one `home.png`. The emulation is always reset, including on the error path — don't hand-roll a shoot-both-themes helper.

**The emulation drives the media query — your app has to be listening.** An app with a manual theme override (a `system`/`light`/`dark` toggle that sets `data-theme` on `<html>`) only follows `prefers-color-scheme` while it is in its follow-the-system state. A spec that pinned the theme — directly, through a helper, or through a leftover stored preference — captures the same rendering twice and writes it to both baselines. Both look right, both pass, and the dark assertion cannot fail. Biloba warns when two schemes capture byte-identical images; treat it as a finding. → `biloba:flaky-specs` §7

The override is **target-level and survives navigation**, so a dropped teardown would silently leave every later spec rendering in that scheme. Biloba warns when a reset fails (naming the stuck scheme) and `b.Prepare()` clears a leaked override before the next spec. If you see that warning, the leak is already handled — but it means the tab that produced it finished the spec in the emulated scheme.

## Pitfalls

- **Don't over-use it.** A visual assertion is a wide net: it fails on every change, intended or not. Reach for it where appearance *is* the contract (a chart, a themed rail, a print layout) and keep asserting text/counts/state with the ordinary matchers.
- **Do reach for it where the pixels are the only witness.** Every other matcher reads what the page reports about itself; this one reads what it drew. The gap between those is a real class of bug: text clipped by a box measured before the font loaded (the DOM has the whole string), content that isn't in the DOM at all (`<canvas>`, WebGL), a recolour to within a shade of the background, an element painted over another by a stacking-context change. A DOM-only suite passes all of them.
- **Whole-page baselines are brittle** — any change anywhere fails them all. Prefer element captures.
- **A `uniform shift` failure is not a re-baseline.** Something above the subject moved; fix that.
- The matcher does no scrolling and no viewport changes: an element capture is clipped to the element's box and works below the **document** fold. It does *not* reach inside an inner `overflow: auto` pane — see the determinism table above.

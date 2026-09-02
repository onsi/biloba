---
name: debug-failures
description: See why a Biloba spec failed or flaked — the on-failure artifacts (DOM outline + screenshots + poll trajectory of the timed-out read + the visual-regression diagnosis with its .actual.png/.diff.png, the "never settled" warning an update run prints + the detached-node "matched then stopped matching" signal + the occluded-click diagnosis naming what covered the target), how Biloba auto-adapts to humans vs CI vs AI agents, the env vars and config knobs that surface them (BILOBA_SCREENSHOTS_DIR, BILOBA_SCREENSHOT_BASELINES_DIR, BILOBA_UPDATE_SCREENSHOTS, BILOBA_INLINE_SCREENSHOTS, BILOBA_OUTLINE_MAX, BILOBA_INTERACTIVE, BilobaConfig*), attaching app/store state to a failure, headless quirks (stale innerText, unscheduled requestAnimationFrame), and using b.Outline()/b.A11yOutline() to understand why a selector didn't match. Use when a browser spec is failing or flaky and you need visibility, or to configure failure output for CI/agents. For *preventing* flakes (single-shot reads, avoiding b.Immediate(), optimistic-UI) see biloba:flaky-specs.
---

# Debugging Biloba failures

Reading artifacts after a spec failed. To *prevent* flakes → `biloba:flaky-specs`. Docs: <https://onsi.github.io/biloba/#failure-artifacts>.

## Zero config: what you already get

Biloba detects the environment in `ConnectToChrome`. "Automation" = `CI` is set **or** an AI coding agent is detected (`CLAUDECODE`/`AI_AGENT`/Cursor/Gemini CLI/Codex/…).

| | Interactive human | CI **or** AI agent |
|---|---|---|
| Failure screenshot | inline in the terminal | written to `./biloba-screenshots` |
| DOM outline on failure | no | yes |
| Inline image blob | yes (if the terminal supports it) | no |

So an agent or CI run needs nothing: `ginkgo -r -p`, then read the outline and the screenshot files. `BILOBA_SCREENSHOTS_DIR=./artifacts` points the directory elsewhere.

## Read the artifacts in this order

1. **Console errors** — any `console.error`/`console.assert` before the failure, replayed under "Console errors logged before this failure" at the **top** of the failure block. On a JS crash (a React error boundary) this is the root cause.
2. **`⚠` diagnostic notes** — they name the cause outright.
3. **Poll trajectory** — what the read you failed on did over its whole deadline (absent when the failure had no value read behind it).
4. **Visual diagnosis** — only on a failed `b.HaveScreenshot`: pixel counts plus the *shape* of the change, in words, before you open any image.
5. **Screenshot** — `Read` the printed PNG path.
6. **DOM outline** — "DOM Outline for: '<title>'": indented DOM, `<script>/<style>/<svg>` bodies pruned, whitespace collapsed, capped at 32 KB. Past `... [truncated]`? Raise the cap with **`BILOBA_OUTLINE_MAX`** — a byte count (`=131072`), or `0`/`off` for the whole DOM.

### Poll trajectory

When an `Eventually` over a read that observes and compares a value (`b.HaveInnerText`/`b.HaveCount`/any value matcher, a geometry matcher, `b.EvaluateTo`, `b.GetJSValue`) times out, Biloba attaches the `(elapsed, value)` series **of that assertion**. Gomega's `Timed out … Expected <120>` shows only the final value; the shape is the diagnosis:

| Shape | Means | Do |
|---|---|---|
| **flat** (one row, `held ×N`) | the product computed the value once and never reconciled | fix the product (`biloba:flaky-specs` §4) — a wider timeout won't help |
| **monotone staircase** | latency; it nearly made it | widen the timeout |
| **dip-then-rebound** | a late reflow shoved it back | settle layout before asserting |

**No entry is a normal outcome, not a bug.** The series is claimed by the matcher Gomega asked for a failure message, so an entry always describes the read you failed on. Reads that passed, `b.Run` setup lines, and failures with no value read underneath (a `b.Click` whose selector never matched, a getter whose value was never there — that one gets the `AllowMissing` enrichment instead) simply produce nothing. Don't read a missing trajectory as a signal; go to the outline and the screenshot. To get a trajectory for an arbitrary expression, poll it with `b.EvaluateTo`/`b.GetJSValue` rather than wrapping `b.Run` in your own `Eventually` — Biloba only records reads it owns.

On by default; `BilobaConfigPollTrajectory(false)` disables it (and the detached-node signal).

### Visual diagnosis (a failed `b.HaveScreenshot`) → `biloba:visual-assertions`

Two extra PNGs land in the screenshots dir — `<name>.actual.png` (what Biloba saw) and `<name>.diff.png` (the actual, washed out, differing pixels in magenta) — and the failure message says what moved:

```
screenshot "home-desktop" differs from baseline
  38,160 of 1,017,600 pixels differ (3.75%), max channel delta 221
  changed region: one box, (0,14)-(1272,44)  [100% of the width, 4% of the height, at its top edge]
  unchanged: everything below y=44
  baseline: /Users/you/app/biloba-baselines/home-desktop.png
  actual:   /Users/you/app/biloba-screenshots/home-desktop.actual.png
  diff:     /Users/you/app/biloba-screenshots/home-desktop.diff.png
```

| Shape line | Means |
|---|---|
| `one box`, full width, top edge | the header/banner changed |
| `one box`, small, mid-image | the component you touched |
| `changed regions: N boxes` (largest first, capped at 5) | several independent changes |
| `scattered — N regions spread across the image` | a web font failed to load or rendered differently |
| `uniform shift of the whole image, 1px down` | something *above* the subject grew or moved — fix that, don't re-baseline. Never reported for an image thinner than ~16px on either axis (thin rule, focus ring, progress bar) — those get the box reading |
| `baseline is 800x600, actual is 800x640 (40px taller)` | the box resized; no per-pixel story |

What to do about each → `biloba:visual-assertions`.

`unchanged: everything below y=N` is the complement and usually the faster read. `max channel delta` counts every pixel, including those the channel tolerance absorbed — and when it is in the low single digits Biloba adds `every differing pixel differs by <= N — a rasterisation or compositing difference, not a content change`. Believe it: nothing moved, so look for a shadow or gradient compositing into the capture rather than for an element. A **missing** baseline is a different failure — it says to re-run with `BILOBA_UPDATE_SCREENSHOTS=1`; never script your way past it.

`Read` the `.diff.png` when the words aren't enough. A human at a terminal that renders images also gets it drawn under the diagnosis; you get the path instead, since inline images are off under an agent.

**"never settled" — printed during an update run, not on a failure.** Update mode captures until three in a row match before writing; when that never happens it writes the last capture anyway and says so. The run stays green, so this is easy to scroll past — don't. The baseline just written is unsettled and the next normal run will fail against it. Add a `b.Mask(...)` for the moving region (or stop the page moving), then re-run the update; re-running the update alone changes nothing. → `biloba:visual-assertions`

**"Failed to clear the emulated prefers-color-scheme"** — a dropped `b.InColorSchemes` teardown. The override is target-level and survives navigation, so `b.Prepare()` clears the leak before the next spec; the spec that printed the warning, though, finished rendering in the emulated scheme. Read any odd-looking screenshot from that spec with that in mind.

### The `⚠` diagnostic notes

**"Selector matched, then stopped matching"** — the detached node. The selector resolved, then its node was replaced (list re-key, portal migration) or its identifying attribute swapped in place. Silent when the selector genuinely never matched.

```
⚠ Selector "#row-4" matched 6× during this poll (+0.00s to +0.41s) then stopped matching
  — the node was likely replaced, or its identifying attribute changed in place.
```

**"Click dispatched onto a covered element"** — fast `Click` stays occlusion-blind by design and still succeeds through an overlay, but records a hit-test so the downstream failure points somewhere. Diagnostic only; never changes whether a spec passes.

```
⚠ Click on "#submit" was dispatched while <div#overlay.modal-scrim> was the topmost
  element at its centre — the click may have been swallowed.
  Consider Eventually("#submit").Should(b.BeClickable()) or b.Realistic().Click("#submit").
```

**"Network handler never ran (shadowed by an earlier handler)"** — handlers are first-match-wins, so one registered for a URL an earlier handler claims is dead code. Deadly across an `Ordered` container, where `Prepare()` doesn't run between `It`s. Both call sites are named.

```
⚠ A ModifyResponse handler registered at network_test.go:231 never ran — an earlier ModifyResponse
  handler (registered at network_test.go:223) claimed 1 matching response(s) first.
```

Reported only when a handler **never fired** *and* was shadowed at least once. **Limit:** it's a failure artifact, so it cannot surface shadowing's other presentation — a leftover *stateful* handler claiming the response, passing it through untouched, spec **green**. Only your own `Eventually(hold.Count).Should(Equal(1))` catches that (`biloba:flaky-specs` §6).

### When the failure is Chrome itself

Every command Biloba sends Chrome runs under a deadline, so an unresponsive browser produces a failing spec instead of a hung suite. Three shapes, each naming its cause on the first line:

| First line | Means | Do |
|---|---|---|
| `deadline_exceeded: Chrome did not <command> within 30s` | Chrome accepted the command and never answered | Chrome is wedged or the box is badly overloaded — look for a long synchronous script in the page, or too many parallel processes for the machine |
| `page_crashed: this tab's renderer crashed` | Chrome reported the crash — it does so on macOS but not on Linux, where the same crash reads as `deadline_exceeded` against a tab that stopped answering | usually the page itself (OOM, a bad WASM/canvas path). Recoverable: navigate the tab to get a fresh renderer, or move to `b.NewTab()` |
| `browser_gone: the connection to Chrome is closed` | the browser process exited — crashed, OOM-killed, or reaped | not recoverable; the rest of the suite will fail too. Check the machine's memory and whether anything is killing Chrome |

The deadline is generous on purpose (a healthy command answers in milliseconds), so hitting it is a real signal, not a tight-timeout artifact. `WithTimeout` doesn't move it: that knob bounds how long Biloba keeps *retrying*, which is a different question from whether Chrome is alive.

**A suite that ends on Ginkgo's `--timeout` with no failing spec** used to be this class — a command blocked inside a poll's callback, where Gomega can't interrupt it, so the poll deadline never fired. If you still see that shape, it is not this: look for a `chromedp` call of your own on `b.Context` without a deadline.

### Two failure *messages* that self-explain

- A two-axis getter timing out because the **property** never became defined says the element was present, names the property, and prints the `b.AllowMissing("disabled")` to paste (`biloba:flaky-specs` §5).
- A failed `BePrecededBy`/`BeFollowedBy` reports the order actually observed (`Actually: #o-first comes BEFORE #o-second.`) — enough to spot an inverted assertion.

## Look at the page yourself, any time

```go
fmt.Println(b.Outline())     // indented DOM
fmt.Println(b.A11yOutline()) // accessibility tree: role + accessible name per node
AddReportEntry("DOM before click", b.Outline(), ReportEntryVisibilityFailureOrVerbose)
b.Run("document.querySelectorAll('.card').length")   // quick count probe
```

`b.A11yOutline()` is **not** auto-attached — call it explicitly. It's often more useful than raw HTML for reasoning about what a page *means*.

**Attach app/store state to every failure.** For a state-heavy or optimistic-UI app the store beats the DOM (which may be the pre-confirmation copy):

```go
ReportAfterEach(func(report SpecReport) {
    if !report.Failed() { return }
    AddReportEntry("app state", b.Run(`JSON.stringify(window.__APP_STATE__ ?? null)`))
})
```

Keep the `?? null` so a crashed page doesn't turn the snapshot itself into a failure. (That's a *snapshot*. To **wait** on app state as part of a spec, use `b.GetJSValue` — `biloba:flaky-specs` §3.)

**Page `console.*` streams to the `GinkgoWriter`**, each argument rendered space-separated. Objects come from CDP's **shallow** preview, so nested/large objects log lossily. Build the string yourself when you need the whole value: `console.log('state ' + JSON.stringify(obj))`.

## Two headless quirks that look like Biloba bugs

- **`HaveInnerText`/`GetInnerText` timing out on text that's plainly in the outline.** `innerText` is computed from layout and can return a stale/partial value before a paint settles. Switch to `HaveTextContent`/`GetTextContent` (reads `textContent` off the tree) or to a plain existence assertion.
- **`requestAnimationFrame` never firing.** On a **fully static page**, `chrome-headless-shell` can leave rAF unscheduled after the first scroll (nothing animating ⇒ no frames), wedging app code driven off an rAF loop. Shape: a hang/timeout only in the default headless lane. Confirm with a counter read through a **coalescing `b.Run`** — `b.Run("window.__rafTicks ?? 0")`, **not** `b.GetJSValue`, which would sit waiting for a counter that never appears. Fix: drive the work off a real event, or run that spec under `HighFidelityHeadless()`/`BILOBA_INTERACTIVE=true`.

## Env vars

| Var | Effect |
|---|---|
| `BILOBA_SCREENSHOTS_DIR=./artifacts` | where failure screenshots — and the visual `.actual.png`/`.diff.png` artifacts — are written |
| `BILOBA_SCREENSHOT_BASELINES_DIR=./baselines` | where `b.HaveScreenshot`'s **committed** baselines live (default `./biloba-baselines`) |
| `BILOBA_UPDATE_SCREENSHOTS=1` | capture and (re)write every visual baseline the run touches instead of comparing; prints what changed. Accepts `1/t/true/y/yes/on` (off: `0/f/false/n/no/off`), case-insensitive; **any other value warns and is treated as off**. Suite-wide — scope it with `--focus`. **Never set it in CI**: every visual assertion then passes unconditionally |
| `BILOBA_OUTLINE_MAX=131072` | raise the outline byte cap; `0`/`off` = no truncation |
| `BILOBA_INLINE_SCREENSHOTS=iterm\|kitty\|sixel\|none` | force an inline-image protocol, or `none` to disable the blob (the file path is still printed — use `none` in CI and in Claude Code, where base64 is noise) |
| `BILOBA_PROBE_TERMINAL=true` | actively query the TTY for Sixel support when env detection finds nothing |
| `BILOBA_INTERACTIVE=true` | headful high-fidelity run that pauses on failure until `^C` |

Inline images are auto-detected (Kitty, iTerm2, Sixel/VS Code) and only emitted when the terminal supports them.

## Config knobs (`ConnectToChrome`)

Each boolean takes an optional bool (no arg = `true`). **Explicit settings win, per knob** — automation only fills knobs you left untouched.

- `BilobaConfigScreenshotsToDir(dir)` — write each tab's failure screenshot there (prints the absolute path).
- `BilobaConfigFailureOutlines(...bool)` / `BilobaConfigInlineScreenshots(...bool)` — force on/off.
- `BilobaConfigFailureScreenshots(...bool)` (default on) / `BilobaConfigPollTrajectory(...bool)` (default on) / `BilobaConfigProgressReportScreenshots(...bool)` (default on).
- `BilobaConfigFailureScreenshotsSize(w,h)` / `BilobaConfigProgressReportScreenshotSize(w,h)`.
- `BilobaConfigDebugLogging(...bool)` — stream all CDP traffic to the `GinkgoWriter` (verbose).
- `BilobaConfigScreenshotBaselinesDir(dir)` — where `b.HaveScreenshot` reads/writes committed baselines (default `./biloba-baselines`; commit that dir, gitignore the screenshots dir).
- `BilobaConfigScreenshotTolerance(fraction)` / `BilobaConfigScreenshotChannelTolerance(delta)` — suite-wide visual-comparison defaults, both `0` (exact) by default; `b.Tolerance(...)`/`b.ChannelTolerance(...)` override per assertion. → `biloba:visual-assertions`

```go
// CI that only redirects the directory still keeps the automation default of outlines-on:
b = biloba.ConnectToChrome(GinkgoT(), biloba.BilobaConfigScreenshotsToDir("./artifacts"))
```

## Watch it live

```bash
BILOBA_INTERACTIVE=true ginkgo --focus="..."
```

Headful, high fidelity, prints the failure and waits for `^C`. Use a small handful of focused specs, in serial. (`SpinUpChrome(GinkgoT(), biloba.ChromeFlags(chromedp.Flag("headless", false)))` does the same in code.)

## A hang, not a failure

Biloba screenshots Ginkgo [progress reports](https://onsi.github.io/ginkgo/#getting-visibility-into-long-running-specs) — on a spec timeout, a `PollProgressAfter` spec, or on demand: `^T` (SIGINFO) on macOS, `SIGUSR2` on Linux.

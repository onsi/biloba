---
name: debug-failures
description: See why a Biloba spec failed or flaked — the on-failure artifacts (DOM outline + screenshots + poll trajectory of the timed-out read + the detached-node "matched then stopped matching" signal + the occluded-click diagnosis naming what covered the target), how Biloba auto-adapts to humans vs CI vs AI agents, the env vars and config knobs that surface them (BILOBA_SCREENSHOTS_DIR, BILOBA_INLINE_SCREENSHOTS, BILOBA_OUTLINE_MAX, BILOBA_INTERACTIVE, BilobaConfig*), attaching app/store state to a failure, headless quirks (stale innerText, unscheduled requestAnimationFrame), and using b.Outline()/b.A11yOutline() to understand why a selector didn't match. Use when a browser spec is failing or flaky and you need visibility, or to configure failure output for CI/agents. For *preventing* flakes (single-shot reads, avoiding b.Immediate(), optimistic-UI) see biloba:flaky-specs.
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
3. **Poll trajectory** — what the timed-out read did over the whole deadline.
4. **Screenshot** — `Read` the printed PNG path.
5. **DOM outline** — "DOM Outline for: '<title>'": indented DOM, `<script>/<style>/<svg>` bodies pruned, whitespace collapsed, capped at 32 KB. Past `... [truncated]`? Raise the cap with **`BILOBA_OUTLINE_MAX`** — a byte count (`=131072`), or `0`/`off` for the whole DOM.

### Poll trajectory

When an `Eventually` over a polled read (`b.Run`/`b.RunAsync`, a value getter, a geometry getter) times out, Biloba attaches the `(elapsed, value)` series. Gomega's `Timed out … Expected <120>` shows only the final value; the shape is the diagnosis:

| Shape | Means | Do |
|---|---|---|
| **flat** (one row, `held ×N`) | the product computed the value once and never reconciled | fix the product (`biloba:flaky-specs` §4) — a wider timeout won't help |
| **monotone staircase** | latency; it nearly made it | widen the timeout |
| **dip-then-rebound** | a late reflow shoved it back | settle layout before asserting |

On by default; `BilobaConfigPollTrajectory(false)` disables it (and the detached-node signal).

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
| `BILOBA_SCREENSHOTS_DIR=./artifacts` | where failure screenshots are written |
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

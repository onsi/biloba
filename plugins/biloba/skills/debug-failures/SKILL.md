---
name: debug-failures
description: See why a Biloba spec failed or flaked — the on-failure artifacts (DOM outline + screenshots), how Biloba auto-adapts to humans vs CI vs AI agents, the env vars and config knobs that surface them (BILOBA_SCREENSHOTS_DIR, BILOBA_INLINE_SCREENSHOTS, BILOBA_OUTLINE_MAX, BILOBA_INTERACTIVE, BilobaConfig*), attaching app/store state to a failure, and using b.Outline()/b.A11yOutline() to understand why a selector didn't match. Use when a browser spec is failing or flaky and you need visibility, or to configure failure output for CI/agents. For *preventing* flakes (single-shot reads, racing interactions, optimistic-UI) see biloba:flaky-specs.
---

# Debugging Biloba failures

Biloba adapts failure output to *who's looking* and lets you override any piece. Docs: <https://onsi.github.io/biloba/#failure-artifacts>. This skill is about *seeing why a spec failed*; if a spec is **flaky** (passes locally, fails under `-p`/CI, fails intermittently) the cause is usually a single-shot read or a racing interaction — fix those with `biloba:flaky-specs`, then come back here to read the artifacts.

## What you get on failure, by environment

Biloba detects the environment automatically (in `ConnectToChrome`). With **zero config**:

| | Interactive human | CI **or** AI agent |
|---|---|---|
| Screenshot on failure | yes, inline | yes, written to a directory |
| DOM outline on failure | no | yes |
| Inline image blob | yes (if terminal supports) | no |

"Automation" = `CI` is set **or** an AI coding agent is detected (Claude Code, Cursor, Gemini CLI, Codex, … via signals like `CLAUDECODE`/`AI_AGENT`). Under automation, screenshots go to **`./biloba-screenshots`** by default — so a typical agent or CI run needs nothing: just run the suite and read the outline + screenshot files.

```bash
ginkgo -r -p   # under CI/agent: DOM outlines + screenshot files on disk, automatically
```

Point the directory elsewhere (e.g. a CI artifact path) with `BILOBA_SCREENSHOTS_DIR=./artifacts`.

## Reading the artifacts as an agent

- **Console errors** — if the page logged any `console.error`/`console.assert` before the failure, Biloba replays them under "Console errors logged before this failure" at the **top** of the failure block. On a JS crash (e.g. a React error boundary) this is usually the root cause — read it first, before the outline.
- **Screenshot files** — `Read` the printed PNG path to see the rendered page at failure.
- **DOM outline** — attached under "DOM Outline for: '<title>'" in the Ginkgo report. This is the primary tool for *why a selector didn't match*: it's the indented DOM (`<script>/<style>/<svg>` bodies pruned, whitespace collapsed, capped ~32 KB). If the region you need is past the cap (`... [truncated]`), raise or remove it with **`BILOBA_OUTLINE_MAX`**: a byte count (e.g. `BILOBA_OUTLINE_MAX=131072`) raises the cap; `0`/`off` disables truncation and dumps the whole DOM.

Call them yourself at any point, not just on failure:

```go
fmt.Println(b.Outline())     // indented DOM
fmt.Println(b.A11yOutline()) // accessibility tree: role + accessible name per node
AddReportEntry("DOM before click", b.Outline(), ReportEntryVisibilityFailureOrVerbose)
```

`b.A11yOutline()` (the role/name view a screen reader works from) is often *more* useful than raw HTML for reasoning about what a page *means*; it's not auto-attached — call it explicitly.

**Attach app/store state to a failure.** For an optimistic-UI or state-heavy app the *store* is far more diagnostic than the DOM (the DOM may be the pre-confirmation copy — see `biloba:flaky-specs`). Snapshot it on every failure with a `ReportAfterEach` that introspects via `b.Run`:

```go
ReportAfterEach(func(report SpecReport) {
    if !report.Failed() { return }
    AddReportEntry("app state", b.Run(`JSON.stringify(window.__APP_STATE__ ?? null)`))
})
```

Guard the read (`?? null`) so a crashed/half-loaded page doesn't turn the snapshot itself into a failure.

**Page-side `console.log` for live debugging.** All page `console.*` output is forwarded to the `GinkgoWriter` (each argument rendered, space-separated). Objects are rendered from CDP's **shallow** preview, so a nested/large object logs lossily (deep fields collapse). When you're logging a state object to chase a DOM/React timing bug, build one string yourself — `console.log('state ' + JSON.stringify(obj))` — to get the full value instead of the truncated preview. Same idea for a quick count probe: `b.Run("document.querySelectorAll('.card').length")` returns the number directly (no need to reach into the outline).

**`HaveInnerText`/`InnerText` timing out on content that's clearly there.** If an `InnerText`/`HaveInnerText` assertion on freshly-changed or dynamically-added content spins until timeout in headless even though the text is plainly in the DOM (and in the outline), it's almost certainly `innerText` returning a stale/partial value — it's computed from layout, which can lag a DOM change before a paint settles. Switch to the layout-independent `HaveTextContent`/`TextContent` (reads `textContent` straight off the tree) or to a plain existence assertion.

## Inline images (interactive terminals)

Biloba emits inline images only when the terminal supports them — Kitty, iTerm2, or Sixel (VS Code's terminal), auto-detected. Control it with `BILOBA_INLINE_SCREENSHOTS=iterm|kitty|sixel|none`:

- `none` — disable the inline blob entirely (use in CI or in Claude Code, where the base64 is pure noise; the screenshot *file* path is still printed).
- a protocol name — force it regardless of detected terminal.
- `BILOBA_PROBE_TERMINAL=true` — actively query the TTY for Sixel support when env detection finds nothing.

## Config knobs (`ConnectToChrome`)  — explicit settings win, per knob

Each boolean takes an optional bool (no arg = `true`); automation only fills knobs you left untouched.

- `BilobaConfigScreenshotsToDir(dir)` — write each tab's failure screenshot to `dir` (prints the absolute path).
- `BilobaConfigFailureOutlines(...bool)` — force the DOM outline on/off.
- `BilobaConfigInlineScreenshots(...bool)` — force the inline blob on/off.
- `BilobaConfigFailureScreenshots(...bool)` — failure screenshots on/off (default on).
- `BilobaConfigProgressReportScreenshots(...bool)` — screenshots on Ginkgo progress reports (default on).
- `BilobaConfigFailureScreenshotsSize(w,h)` / `BilobaConfigProgressReportScreenshotSize(w,h)`.
- `BilobaConfigDebugLogging(...bool)` — stream all CDP traffic to the `GinkgoWriter` (verbose).

Example — CI that only redirects the directory still keeps the automation default of outlines-on:

```go
b = biloba.ConnectToChrome(GinkgoT(), biloba.BilobaConfigScreenshotsToDir("./artifacts"))
```

## Interactive debugging

Watch a focused failing spec in a real browser and pause on failure:

```bash
BILOBA_INTERACTIVE=true ginkgo --focus="..."
```

Runs headful (high fidelity), prints the failure, and waits until you `^C`. Use a small handful of focused specs, in serial. (`SpinUpChrome(GinkgoT(), biloba.ChromeFlags(chromedp.Flag("headless", false)))` does the same in code.)

## Progress reports (a hang, not a failure)

Biloba emits a screenshot on Ginkgo [progress reports](https://onsi.github.io/ginkgo/#getting-visibility-into-long-running-specs) — on a spec timeout, a `PollProgressAfter` spec, or on demand: `^T` (SIGINFO) on macOS, `SIGUSR2` on Linux. Handy when a spec is stuck rather than failing.

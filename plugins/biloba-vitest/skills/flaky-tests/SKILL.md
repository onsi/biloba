---
name: flaky-tests
description: Diagnose and prevent flaky Biloba TypeScript/Vitest tests — redundant client-side retry loops, immediate mode, single-shot evaluate reads, optimistic UI, async layout, first-match-wins network handlers, held responses, vacuous assertions, visual update/tolerance hazards, lifecycle leakage, never waiting out a clock for a fact you can observe (durations clustered at one timer's period give it away), and timer-driven UI (debounces, countdowns, expiries) taken off real time with the page clock, including why polling never advances a paused clock. Use when a Biloba Vitest test is intermittent, order-dependent, load-sensitive, or only fails in CI/parallel workers.
---

# Flaky Biloba Vitest tests

Biloba actions and assertions already poll inside `bilobad`. Start by deleting outer retries:

```ts
// Good: one request; the daemon owns the retry loop.
await session.getByRole("button", {name: "Save"}).click({timeoutMs: 5_000});
await session.getByTestId("status").expectText("Saved");
```

Do not wrap these calls in `expect.poll`, `waitFor`, sleeps, or a loop. Outer polling hides the useful inner failure and adds a process round trip per attempt. Avoid `{mode: "immediate"}` and `{immediate: true}` unless the test genuinely requires a single observation.

## Common residual races

- **Single-shot application reads:** `evaluate()` reads once. Poll with `expectEvaluation()` when the value must become true.
- **Gate then re-read:** use the `AssertionResult.observed` value from the successful assertion when available instead of taking a second page read.
- **Optimistic UI:** a changed DOM proves the click handler ran, not that the renderer applied the server response. Poll a lazily-written application-state marker or control response arrival with `holdResponse()`.
- **Exists isn't interactive:** a page that hydrates (React, Astro islands, anything that renders server HTML first) shows a clickable button before its handlers are attached, and a component that reads state after mounting (auth, say) first renders a default twin with the same label. Wait on a marker the app sets when it's ready (a `data-ready` from the mount effect, or a state-specific attribute), not on the element or its text.
- **Async geometry:** existence does not imply settled layout. Use Biloba's geometry/style assertions rather than one-shot `getBoundingClientRect()` evaluation.
- **Network shadowing:** handlers are ordered and first-match-wins. Inspect `count()`, `stats()`, or `networkShadowDiagnostics()` to prove the intended handler fired; remove handlers when their scope ends.
- **Held responses:** always release in `finally`. A leaked hold makes the next failure look unrelated.
- **Lifecycle leakage:** reuse one root session but call `prepare()` before every test, and close worker browsers in `afterAll`.
- **Vacuous negatives:** anchor scopes and assert expected counts before negative collection assertions so an empty selection cannot pass accidentally.
- **Waiting out a clock:** never wait out a clock for a fact you can observe. A sleep, or a timeout sized to outlast some timer, spends real time on something the test could check directly: a request went out, a response arrived, an element rendered, an app-state flag flipped. Poll for that. If the test needs the timer itself to fire, use the page clock (next item). Tests that wait out a timer show up in a hunt's timing data, with durations clustered at the timer's period (`biloba-vitest:flake-hunt`).
- **Real timers:** a test that waits for the page's own `setTimeout` (a debounce, a toast that hides after 3s, a countdown, a session expiry) is slow when it works and flaky when the timer races the poll deadline. Don't widen the timeout: `installClock(t0)` before `navigate()`, `pauseAt(t0)`, then `runFor(ms)` or `fastForward(ms)` (`biloba-vitest:write-tests` §8). `installClock` alone keeps time flowing at the real rate, so without the `pauseAt` a chain of timers can finish before the first poll looks.
- **A trailing debounce on a page that never goes quiet:** an observer behind `clearTimeout(t); t = setTimeout(sync, 120)` is reset by every DOM change, so on a page that streams or animates the derived UI never updates. Whether a quiet gap appears depends on load, so the test can pass alone and fail in every full run. It's a product bug: throttle instead (`if (!t) t = setTimeout(() => { t = 0; sync() }, 120)`), and add a test that churns the DOM while it acts.
- **A paused clock:** polling never advances it. A timeout ending in `This tab's page clock is paused at <t>; ...` means the page's timers never ran — call `runFor`, `fastForward`, or `resume` before the assertion.

## Visual traps

- A missing baseline must fail until a human reviews and writes it.
- Never enable screenshot update mode (`BILOBA_UPDATE_SCREENSHOTS`, `updateScreenshots`) in CI.
- Mask deterministic dynamic regions; do not widen tolerances until the assertion cannot fail.
- Treat byte-identical light/dark captures as suspicious when the test intended two schemes.

On failure, read `BilobaError.trajectory` before increasing timeouts. Flat means waiting longer will not help. Artifacts → `biloba-vitest:debug-failures`. Measuring a flake's rate, or confirming a fix → `biloba-vitest:flake-hunt`. Visual workflow → `biloba-vitest:visual-assertions`. Docs: <https://onsi.github.io/biloba/vitest.html>.

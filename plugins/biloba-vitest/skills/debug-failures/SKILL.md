---
name: debug-failures
description: Diagnose Biloba failures in TypeScript/Vitest — BilobaError codes and fields, polling trajectories, DOM outlines, screenshots and visual artifacts, context-wide session.captureDiagnostics(), console/warning streams, debug logs, Vitest hooks, and renderer/browser/daemon crash distinctions. Use when a Biloba Vitest test fails, hangs, flakes, or needs CI/agent artifact configuration.
---

# Debugging Biloba Vitest failures

Start with the rejected `BilobaError`, not a wider timeout:

```ts
try {
  await session.locator("#never").expectText("ready", {timeoutMs: 500});
} catch (error) {
  const failure = error as BilobaError;
  console.error(failure.code, failure.locator, failure.expected);
  console.error(failure.trajectory, failure.domOutline, failure.artifactPaths);
}
```

The trajectory tells you what the daemon observed on every attempt. A flat trajectory is usually a stable product/test bug; a monotone approach suggests latency; a match followed by failures points to a detached or replaced node. Widen a timeout only when the evidence shows genuine slow progress.

## Failure codes

| Code | Meaning |
|---|---|
| `TIMEOUT` | A polling action or assertion exhausted its deadline. |
| `TARGET_NOT_FOUND` | No element matched the locator. |
| `TARGET_NOT_READY` | The element exists but is hidden, disabled, or otherwise not actionable yet. |
| `NAVIGATION` | The response status did not match the requested navigation status. |
| `JAVASCRIPT_ERROR` | Page evaluation threw. |
| `INVALID_ARGUMENT` | The request itself is malformed. |
| `PAGE_CRASHED` | This session's renderer died; navigate again to recover. |
| `BROWSER_GONE` | The shared Chrome exited or crashed. |
| `DRIVER_CLOSED` | This worker's daemon died; inspect `daemonDetail` and stderr. |

Do not turn crash codes into assertion timeouts. They identify which layer died and whether recovery is possible.

## Runner-level diagnostics

Install `installBilobaVitestHooks` from `@onsi/biloba-vitest-prototype/vitest` in a Vitest setup file. It captures live tabs after failures, can capture slow-test progress, replays browser errors, and reports `console.assert` at the test boundary.

- Call `session.captureDiagnostics()` for an explicit context-wide snapshot.
- Use `consoleMessages()`/`onConsoleMessage()` and `warnings()`/`onWarning()` for history and live events.
- Pass `debugLog` to `connect` for bounded structured daemon/CDP records.
- Configure screenshots, outlines, artifact paths, inline output, viewport, byte limits, and trajectories under `diagnostics`.

Preventing recurrence → `biloba-vitest:flaky-tests`. Visual mismatch details → `biloba-vitest:visual-assertions`. Docs: <https://onsi.github.io/biloba/vitest.html#when-something-fails>.

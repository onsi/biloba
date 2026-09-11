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
| `TIMEOUT` | A polling action or assertion exhausted its deadline. The ordinary assertion failure. |
| `TARGET_NOT_FOUND` | No element matched the locator. |
| `TARGET_NOT_READY` | The element exists but is hidden, disabled, or otherwise not actionable yet. A retry may succeed. |
| `NAVIGATION` | The response status did not match the requested navigation status. Waiting will not fix it; use `navigateWithStatus` when the error page is the page under test. |
| `JAVASCRIPT_ERROR` | Page evaluation threw. |
| `INVALID_ARGUMENT` | The request itself is malformed (e.g. a cookie with no domain and no navigated origin). |
| `PAGE_CRASHED` | This session's renderer died; the browser is fine. Navigate again to recover. |
| `BROWSER_GONE` | The shared Chrome exited or crashed. |
| `DRIVER_CLOSED` | This worker's daemon died; inspect `daemonDetail` and stderr. |

The crash codes exist because Chrome just stops answering calls to a dead renderer or browser; without them a crash would read as an assertion that never came true. Do not turn crash codes into assertion timeouts. They identify which layer died and whether recovery is possible.

## Setup-time failures

These happen before any test runs, so look at the error text, not `BilobaError.code`:

- **Missing platform package** — `resolveDaemonExecutable` throws when the per-platform `bilobad` package (`biloba-darwin-arm64`, etc.) isn't installed. The message names likely causes: an `--omit=optional`/`--no-optional` install, or `node_modules` built on a different OS/arch than it's running on (a lockfile from macOS reused in a Linux container, say). Reinstall with optional dependencies, or set `daemonExecutable`/`BILOBA_DAEMON_EXECUTABLE`.
- **Chrome not found** — no `chrome-headless-shell` was given explicitly (`chromePath`, `BILOBA_CHROME_HEADLESS_SHELL`), on `PATH`, or cached. The error names `npx biloba install-chrome` as the fix.
- **"No usable sandbox"** — on Linux, `bilobad` auto-adds `--no-sandbox` in a headless mode when it's running as root or AppArmor is restricting unprivileged user namespaces (the default on Ubuntu 23.10+, including `ubuntu-latest`), so this should only happen with `chromeSandbox: true` forcing the sandbox on. Drop that override, or set `chromeSandbox: false` to force it off yourself.
- **`BILOBA_VERSION_MISMATCH`** — a `process.emitWarning`, not a thrown error, raised by `connect` when an overridden daemon's version doesn't match the `biloba` package's. It means the two were mixed by accident (an old `BILOBA_DAEMON_EXECUTABLE` after an upgrade, say); it doesn't fail the run.

## Runner-level diagnostics

Install `installBilobaVitestHooks` from `biloba/vitest` in a Vitest setup file. It captures live tabs after failures, can capture slow-test progress, replays browser errors, and reports `console.assert` at the test boundary.

- Call `session.captureDiagnostics()` for an explicit context-wide snapshot.
- Use `consoleMessages()`/`onConsoleMessage()` and `warnings()`/`onWarning()` for history and live events.
- Pass `debugLog` to `connect` for bounded structured daemon/CDP records.
- Configure screenshots, outlines, artifact paths, inline output, viewport, byte limits, and trajectories under `diagnostics`.

Preventing recurrence → `biloba-vitest:flaky-tests`. Visual mismatch details → `biloba-vitest:visual-assertions`. Docs: <https://onsi.github.io/biloba/vitest.html#when-something-fails>.

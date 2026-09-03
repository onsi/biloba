---
name: overview
description: Explain Biloba's TypeScript/Vitest mental model — one shared Chrome, one bilobad process per Vitest worker, isolated reusable root sessions, server-side polling, fast versus realistic input, and structured diagnostics. Use first when adopting @onsi/biloba-vitest-prototype or deciding how to structure a Vitest browser suite. Route to the other biloba-vitest:* skills.
---

# Biloba for Vitest

Biloba's TypeScript client drives Chrome through `bilobad`. Each Vitest worker owns a daemon; all daemons attach to one Chrome started by global setup:

```
vitest worker 1  ──▶  bilobad  ──┐
vitest worker 2  ──▶  bilobad  ──┼──▶  one shared Chrome
vitest worker 3  ──▶  bilobad  ──┘
```

This preserves parallelism without launching a browser per test file. Each root `Session` owns an isolated browser context. Reuse it across tests with `session.prepare()`; open sibling tabs only for flows that need them.

## Principles

- **Poll next to Chrome.** Actions and assertions poll inside the daemon and make one client request. Never wrap them in `expect.poll`, `waitFor`, sleeps, or a retry loop.
- **Use the fast path by default.** Locator actions are atomic page-runtime operations. Add `.realistic()` only for behavior that depends on real pointer/keyboard input, occlusion, hover, scrolling, drag, or touch.
- **Assert observable outcomes.** Prefer semantic locators, visible state, URL/title, network effects, and application state over styling classes or implementation structure.
- **Reuse isolated sessions.** A fresh browser per test destroys the topology's performance advantage. `prepare()` resets cheaply; root sessions isolate cookies and storage between workers.
- **Read structured failures.** `BilobaError` carries codes, locator/expected/observed values, polling trajectory, outlines, screenshots, and context-wide diagnostics.

## Route by task

| Task | Skill |
|---|---|
| Wire the daemon, shared browser, and Vitest lifecycle | `biloba-vitest:setup` |
| Write locators, actions, assertions, tabs, frames, or network tests | `biloba-vitest:write-tests` |
| Create or diagnose screenshot baselines | `biloba-vitest:visual-assertions` |
| Read a failure, console output, artifacts, or crash code | `biloba-vitest:debug-failures` |
| Remove races, order dependence, or redundant polling | `biloba-vitest:flaky-tests` |

Canonical docs: <https://onsi.github.io/biloba/vitest.html>. The package is a prototype and is not yet published to npm; pin the plugin and client to the same Biloba version.

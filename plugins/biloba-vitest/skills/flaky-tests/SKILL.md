---
name: flaky-tests
description: Diagnose and prevent flaky Biloba TypeScript/Vitest tests — redundant client-side retry loops, immediate mode, single-shot evaluate reads, optimistic UI, async layout, first-match-wins network handlers, held responses, vacuous assertions, visual update/tolerance hazards, and lifecycle leakage. Use when a Biloba Vitest test is intermittent, order-dependent, load-sensitive, or only fails in CI/parallel workers.
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
- **Async geometry:** existence does not imply settled layout. Use Biloba's geometry/style assertions rather than one-shot `getBoundingClientRect()` evaluation.
- **Network shadowing:** handlers are ordered and first-match-wins. Inspect `count()`, `stats()`, or `networkShadowDiagnostics()` to prove the intended handler fired; remove handlers when their scope ends.
- **Held responses:** always release in `finally`. A leaked hold makes the next failure look unrelated.
- **Lifecycle leakage:** reuse one root session but call `prepare()` before every test, and close worker browsers in `afterAll`.
- **Vacuous negatives:** anchor scopes and assert expected counts before negative collection assertions so an empty selection cannot pass accidentally.

## Visual traps

- A missing baseline must fail until a human reviews and writes it.
- Never enable screenshot update mode in CI.
- Mask deterministic dynamic regions; do not widen tolerances until the assertion cannot fail.
- Treat byte-identical light/dark captures as suspicious when the test intended two schemes.

On failure, read `BilobaError.trajectory` before increasing timeouts. Flat means waiting longer will not help. Artifacts → `biloba-vitest:debug-failures`. Visual workflow → `biloba-vitest:visual-assertions`. Docs: <https://onsi.github.io/biloba/vitest.html>.

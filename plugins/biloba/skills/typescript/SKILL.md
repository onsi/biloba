---
name: typescript
description: Drive Biloba from a TypeScript/vitest suite instead of Go — the bilobad daemon topology (one daemon per vitest worker, one shared Chrome), connect/openSession/prepare, locators (locator/getByTestId/getByText/getByRole/first), actions and assertions (click, setValue, expectVisible/Text/Count/Attribute/Value, expectUrl, expectEvaluation), navigate vs navigateWithStatus, evaluate's args-array rule, server-side polling via timeoutMs/intervalMs, and reading a BilobaError (code, trajectory, domOutline, screenshotPath). Use when writing or debugging browser tests in TypeScript/vitest against Biloba, or when deciding between the Go and TypeScript clients. For the Go API use biloba:write-tests.
---

# Biloba from TypeScript

The vitest client. Go authoring → `biloba:write-tests`. Mental model → `biloba:overview`. Docs: <https://onsi.github.io/biloba/#biloba-from-typescript>.

**Prototype.** The package (`@onsi/biloba-vitest-prototype`) is not on npm; it is built from the Biloba repo. Its API moves faster than the Go one.

## 1. The topology — know this before writing anything

Chrome DevTools plumbing lives in Go, so TypeScript does not reimplement it. Each vitest worker spawns a Go daemon (`bilobad`) and speaks framed JSON over its stdio. All daemons share one Chrome.

```
vitest worker 1  ──▶  bilobad  ──┐
vitest worker 2  ──▶  bilobad  ──┼──▶  one shared Chrome
vitest worker 3  ──▶  bilobad  ──┘
```

**Polling runs on the daemon, not in your test.** `expectText("ready", {timeoutMs: 1000, intervalMs: 5})` is **one** request — the retry loop runs next to Chrome and answers once with the outcome plus the trajectory it took.

- Never write a client-side retry loop (`await expect.poll(...)` around a Biloba assertion, `waitFor`, a `for` loop with sleeps). You are re-polling a poll; the failure you get is the inner one, and you pay a round trip per attempt.
- There is **no** `Immediate()` and no matcher form. The Go [dual API](https://onsi.github.io/biloba/#interacting-with-elements) is a Gomega idiom with no TypeScript equivalent. Polling is the only mode.
- Tune with `{timeoutMs, intervalMs, signal}` on any action or assertion.

## 2. Setup

Build the daemon (from the Biloba repo):

```bash
go build -o .bin/bilobad ./cmd/bilobad
```

One Chrome per run, in vitest's global setup:

```ts
// global-setup.ts
import {startSharedBrowser, type SharedBrowserProcess} from "@onsi/biloba-vitest-prototype";
import type {TestProject} from "vitest/node";

const daemonExecutable = process.env.BILOBA_DAEMON_EXECUTABLE;
if (!daemonExecutable) throw new Error("BILOBA_DAEMON_EXECUTABLE is not set");

let browser: SharedBrowserProcess | undefined;

export async function setup(project: TestProject): Promise<void> {
  browser = await startSharedBrowser({executable: daemonExecutable});
  project.provide("chromeWsUrl", browser.wsURL);
}

export async function teardown(): Promise<void> {
  await browser?.stop();
}
```

A daemon and session per test file:

```ts
import {inject} from "vitest";
import {connect, type Browser, type Session} from "@onsi/biloba-vitest-prototype";

let browser: Browser;
let session: Session;

beforeAll(async () => {
  browser = await connect({chromeWsUrl: inject("chromeWsUrl")});
  session = await browser.openSession();
});
beforeEach(async () => { await session.prepare(); });
afterAll(async () => { await browser.close(); });
```

- `connect` falls back to `BILOBA_DAEMON_EXECUTABLE` when `daemonExecutable` is omitted.
- Omit `chromeWsUrl` and the daemon launches its own Chrome — fine for one file, wasteful for a suite.
- Pass `artifactDir` to `connect` to get failure screenshots written to disk.
- A `Session` is the analogue of the Go root tab `b`: its own browser context, so cookies and storage are isolated. `session.prepare()` is `b.Prepare()` — reset between tests rather than opening a new session.

## 3. Locators

Lazy; building one talks to nobody.

| Call | Selects |
|---|---|
| `session.locator("#content")` | CSS |
| `session.getByTestId("name")` | `[data-testid="name"]` |
| `session.getByText("Save", {exact: true})` | by text content |
| `session.getByRole("button", {name: "Increment"})` | by ARIA role, optionally accessible name |
| `.first()` | narrow to the first match |

Prefer `getByRole`/`getByTestId` over brittle CSS, same as in Go.

## 4. Actions and assertions

```ts
await session.getByTestId("name").setValue("Ada");
await session.getByRole("button", {name: "Increment"}).click();

await session.locator("#count").expectText("1");            // {exact?: boolean}
await session.locator("#spinner").expectCount(0);
await session.getByTestId("name").expectValue("Ada");
await session.locator("a.home").expectAttribute("href", "/"); // {exact?: boolean}
await session.getByRole("heading", {name: "Dashboard"}).expectVisible();
```

Page-level, so they live on the session:

```ts
await session.expectUrl("/dashboard", {pathname: true});     // {exact?, pathname?}
await session.expectEvaluation("window.app.ready", true);
```

Assertions resolve to an `AssertionResult`: `observed`, `attemptCount`, `trajectory`, `rpcRequestCount`, `rpcResponseCount`, `elapsedMs`.

## 5. Navigation

```ts
await session.navigate("http://localhost:8080/search?q=foo");        // asserts HTTP 200
await session.navigateWithStatus("http://localhost:8080/nope", 404); // asserts 404
```

`navigate`'s 200 check is an assertion, not a transport rule — an unexpected error page is usually a broken fixture, and letting it through fails confusingly three lines later. Use `navigateWithStatus` when the 4xx/5xx page *is* the page under test. A mismatch is code `NAVIGATION`.

## 6. evaluate

```ts
const title = await session.evaluate<string>("document.title");     // reads an expression
const sum   = await session.evaluate<number>("(a, b) => a + b", [40, 2]); // calls a function
const now   = await session.evaluate<number>("() => Date.now()", []);     // still calls it
```

The **presence** of the args array means "call this", not its length. `[]` calls; omitting it reads.

Cookies accept a `Date` for expiry:

```ts
await session.setCookies([{name: "session", value: "abc123", path: "/"}]);
```

## 7. Failures

Everything rejects with a `BilobaError` carrying `code`, `locator`, `expected`, `observed`, `trajectory`, `domOutline`, `screenshotPath`, `daemonDetail`.

`trajectory` is the [poll trajectory](https://onsi.github.io/biloba/#outline) as data — every attempt with what it observed and why it retried. Read it before widening a timeout: a flat line is a product bug, a monotone approach is latency.

Narrow on `code` — several are specific and actionable:

| Code | Meaning |
|---|---|
| `TIMEOUT` | Poll ran out of time. The ordinary assertion failure. |
| `TARGET_NOT_FOUND` | Nothing matched the locator. |
| `TARGET_NOT_READY` | Matched but refused — hidden, disabled. Means *not yet*; a retry may succeed. |
| `NAVIGATION` | Page loaded with a status you did not ask for. Waiting will not fix it — see `navigateWithStatus`. |
| `JAVASCRIPT_ERROR` | Your expression threw in the page. |
| `INVALID_ARGUMENT` | Malformed request — e.g. a cookie with no domain and no navigated origin. |
| `PAGE_CRASHED` | This session's renderer died; browser is fine. Navigate again to recover. |
| `BROWSER_GONE` | The shared Chrome exited or crashed. |
| `DRIVER_CLOSED` | This worker's daemon died. `daemonDetail` holds its stderr. |

The last three exist so a crash reports itself as a crash: Chrome does not fail calls to a dead renderer, it stops answering them, so without a dedicated code a crashed page is indistinguishable from an assertion that never came true.

## 8. Not implemented in TypeScript

Dialogs, downloads, network stubbing/observation, `HaveScreenshot` visual assertions, the XPath DSL, tab management, and realistic mode are **Go-only**. If a task needs one, use the Go client — do not attempt a workaround through `evaluate`.

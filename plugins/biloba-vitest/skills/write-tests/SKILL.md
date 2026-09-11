---
name: write-tests
description: Author Biloba browser tests in TypeScript/Vitest — sessions, tabs, frames, CSS/semantic/XPath locators, polling actions and assertions, realistic input, cookies/storage, dialogs/downloads/network, screenshots, and structured failures. Use when writing or reviewing tests against biloba. For wiring the daemon and shared Chrome, use biloba-vitest:setup.
---

# Writing Biloba Vitest tests

Assume the suite is wired with `biloba-vitest:setup` and apply the topology in `biloba-vitest:overview`. Visual assertions → `biloba-vitest:visual-assertions`. Failures → `biloba-vitest:debug-failures`. Flakes → `biloba-vitest:flaky-tests`. Docs: <https://onsi.github.io/biloba/vitest.html>.

**Prototype.** The `biloba` package is published in lockstep with the Go release and its API will continue to shift before 1.0.

## 1. The topology — know this before writing anything

Chrome DevTools plumbing lives in Go, so TypeScript does not reimplement it. Each vitest worker spawns a Go daemon (`bilobad`) and speaks framed JSON over its stdio. All daemons share one Chrome.

```
vitest worker 1  ──▶  bilobad  ──┐
vitest worker 2  ──▶  bilobad  ──┼──▶  one shared Chrome
vitest worker 3  ──▶  bilobad  ──┘
```

**Polling runs on the daemon, not in your test.** `expectText("ready", {timeoutMs: 1000, intervalMs: 5})` is **one** request — the retry loop runs next to Chrome and answers once with the outcome plus the trajectory it took.

- Never write a client-side retry loop (`await expect.poll(...)` around a Biloba assertion, `waitFor`, a `for` loop with sleeps). You are re-polling a poll; the failure you get is the inner one, and you pay a round trip per attempt.
- The default mode is `"eventually"`. Use `{mode: "immediate"}` for one attempt or `{mode: "consistently"}` to require the condition to remain true for the timeout. On actions, `{immediate: true}` is shorthand for `{mode: "immediate"}`.
- Tune with `{timeoutMs, intervalMs, signal, mode}` on actions and assertions.

## 2. Sessions and tabs

Wiring (install, `npx biloba install-chrome`, global setup, launch options) → `biloba-vitest:setup`. Each test file holds one daemon and one reusable root session:

```ts
import {inject} from "vitest";
import {connect, type Browser, type Session} from "biloba";

let browser: Browser;
let session: Session;

beforeAll(async () => {
  browser = await connect({chromeConnection: inject("chromeConnection")});
  session = await browser.openSession();
});
beforeEach(async () => { await session.prepare(); });
afterAll(async () => { await browser.close(); });
```

- A root `Session` has its own browser context, so cookies and storage are isolated. `session.prepare()` resets it cheaply — call it between tests rather than opening a new session.
- `await session.newTab()` opens a sibling tab in the same context. Use `tabs()`/`spawnedTabs()` for snapshots, `findTab()`/`waitForTab()` for popup workflows, and `frames()`/`waitForFrame()` for cross-origin frame targets.

## 3. Locators

Lazy; building one talks to nobody.

| Call | Selects |
|---|---|
| `session.locator("#content")` | CSS |
| `session.getByTestId("name")` | `[data-testid="name"]` |
| `session.getByText("Save", {exact: true})` | by text content |
| `session.getByRole("button", {name: "Increment"})` | by ARIA role, optionally accessible name |
| `session.getByLabel("Email")` | associated label |
| `session.getByPlaceholder("Search")` | placeholder |
| `session.getByAltText("Profile")` | image alt text |
| `session.getByTitle("Close")` | title |
| `session.xpath("//button[@name='save']")` | raw XPath |
| `.first()` | narrow to the first match |

Prefer `getByRole`/`getByTestId` over brittle styling classes.

The XPath DSL is client-side. Build the expression, then bind it to the session:

```ts
import {relativeXPath, xpath} from "biloba";

const save = xpath("button").withClass("primary").withText("Save");
await session.xpath(save).click();

const list = xpath("ul").withChildMatching(relativeXPath("li").withText("Francais"));
await session.xpath(list).expectVisible();
```

The builder includes attribute/text predicates, boolean predicates (`xPredicate()`), tree and sibling axes, and XPath's one-based `first()`/`nth(position)`/`last()`. Locator `.nth(index)` is zero-based after binding.

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

Opt into browser-faithful CDP input when realism matters:

```ts
await session.getByRole("button", {name: "Pay"}).realistic().click();
await session.getByTestId("card-number").realistic().setValue("4242");
await session.getByTestId("card-number").realistic().type(" 4242");
await session.getByTestId("card").realistic().dragTo(session.getByTestId("column"));
await session.getByTestId("menu").realistic().hover();
```

The realistic track also covers click variants, tap, and wheel input. The fast default keeps find-and-act atomic in the page runtime.

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

## 7. Dialogs, downloads, and network

```ts
await session.expectRequest(endsWith("/orders"), {method: "POST"});

const stub = await session.stubRequest(endsWith("/feature-flags"), {
  status: 200,
  body: new TextEncoder().encode('{"available":true}'),
});

const hold = await session.holdResponse(endsWith("/inventory"));
try {
  await session.getByRole("button", {name: "Refresh"}).click();
  await hold.await();
} finally {
  await hold.release();
}
```

Always release a hold. Network handlers are first-match-wins, so assert `count()` or inspect `stats()` when the registration itself is part of the test.

Use `requests()`/`responses()` for history and `waitForRequest()` for an atomic winning observation. `stubRequest()`, `abortRequest()`, `modifyRequest()`, `modifyResponse()`, and `routeResponse()` return first-match-wins handlers with `count()`, `stats()`, and `remove()`. Network-state methods cover cache, offline mode, latency, throughput, and connection type.

`modifyResponse()` patches the response: what you leave out is inherited from the original. `routeResponse()` *replaces* it - an unset status means 200, and unset headers or body mean none - so hand back anything you want kept, including headers you read off the intercepted response.

```ts
const handler = await session.handleDialogs("prompt", {message: "Name?", promptText: "Ada"});
const download = await session.expectDownload({filename: endsWith(".csv")});
const bytes = await download.content();
await handler.remove();
```

Dialog history includes safely auto-handled dialogs and warnings. Download snapshots preserve active, complete, and cancelled state; content is bounded binary data.

## 8. Screenshots and visual assertions

`captureScreenshot()` returns bounded `Uint8Array` data by default or a daemon-owned path with `{output: "path", name}`. It works on a session or locator.

`expectScreenshot(name, options)` supports masks, pixel and channel tolerances, automatic animation freezing, animated opt-out, light/dark schemes, baseline update mode, diff artifacts, and structured diagnosis.

```ts
const result = await session.getByTestId("chart").expectScreenshot("revenue-chart", {
  mask: [session.getByTestId("timestamp")],
  colorSchemes: ["light", "dark"],
});
expect(result.match).toBe(true);
```

## 9. Failures and diagnostics

Everything rejects with a `BilobaError` carrying `code`, `locator`, `expected`, `observed`, `trajectory`, `domOutline`, `screenshotPath`, `diagnostics`, `visual`, `artifactPaths`, and `daemonDetail` as applicable. Narrow on `code`: `NAVIGATION` and the crash codes (`PAGE_CRASHED`, `BROWSER_GONE`, `DRIVER_CLOSED`) mean something specific that waiting will not fix.

`trajectory` records every polling attempt, what it observed, and why it retried. Read it before widening a timeout: a flat line is a product bug, while a monotone approach suggests latency.

The full code table, `installBilobaVitestHooks`, `session.captureDiagnostics()`, and the console/warning streams → `biloba-vitest:debug-failures`.

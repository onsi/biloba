---
name: write-tests
description: Author Biloba browser tests in TypeScript/Vitest — sessions, tabs, frames, CSS/semantic/XPath locators, polling actions and assertions, realistic input, cookies/storage, dialogs/downloads/network, time-dependent UI with the page clock (installClock + pauseAt/runFor/fastForward), screenshots, and structured failures. Use when writing or reviewing tests against biloba. For wiring the daemon and shared Chrome, use biloba-vitest:setup.
---

# Writing Biloba Vitest tests

Assume the suite is wired with `biloba-vitest:setup` and apply the topology in `biloba-vitest:overview`. Visual assertions → `biloba-vitest:visual-assertions`. Failures → `biloba-vitest:debug-failures`. Flakes → `biloba-vitest:flaky-tests`. Docs: <https://onsi.github.io/biloba/vitest.html>.

**Pre-1.0.** The `biloba` package is published in lockstep with the Go release and its API will continue to shift before 1.0.

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
- `await session.newTab()` opens a sibling tab in the same context. Use `tabs()`/`spawnedTabs()` for snapshots and `findTab()`/`waitForTab()` for popup workflows.
- Emulation lives on the session and `prepare()` clears it: `setDeviceMetrics({width, height, deviceScaleFactor?, mobile?, touch?, landscape?})`, `setUserAgent(ua)` (both `navigator.userAgent` and the `User-Agent` header), `setMedia({type?, colorScheme?, reducedMotion?})`, `setLocale()`, `setTimezone()`, `setGeolocation()`, `setPermissions(origin, {camera: "granted"})` (the names `navigator.permissions.query` takes: `"camera"`, `"microphone"`, `"clipboard-read"`, ...), each with a `clear...()` (`resetPermissions()`).
- `frames()` snapshots cross-origin iframe documents and `waitForFrame({url, title, has})` polls for one. It covers nested OOPIFs and same-site cross-origin frames (for example, two localhost ports) through CDP frame scoping. Use the returned session for locators, trusted/fast actions, uploads, assertions, and JavaScript evaluation; `frame.evaluate()` can read globals created by the frame's own scripts. Use `>>>` for same-origin iframe or open-shadow piercing. Removal, iframe/parent navigation, or replacement makes the old handle fail with `TARGET_NOT_FOUND`; closing a non-owning frame leaves independently acquired nested handles live, while owner `prepare()`/`close()` invalidates them with `DRIVER_CLOSED`.
- Time is per session too: `installClock(t0)` fakes the page's timers and `Date` so a test moves time instead of waiting for it (§8); `prepare()` removes it.
- A frame handle is a document, not a tab: `navigate()`, `prepare()`, `addInitScript()`, `setWindowSize()`, emulation, the page clock, `setCookies()`/`clearCookies()`, request stubbing/holding, network state, and `handleDialogs()` fail on it with `INVALID_ARGUMENT` — call them on the owning session. Dialogs from any frame arrive on the owning tab; the frame handle records its own console messages, page errors, and requests. `frame.captureScreenshot()` captures the frame's viewport; an out-of-process frame can't be screenshotted directly, so capture its `iframe` element from the owning session.

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

For a gesture of your own (a pen stroke, a slider scrubbed while recording), drive a real pointer one event at a time:

```ts
const pen = session.pointer({type: "pen"});           // default type: "mouse"
await pen.moveToElement("#canvas", {position: {x: 10, y: 10}});
await pen.down();
for (let x = 20; x <= 200; x += 10) await pen.moveTo(x, 40);
await pen.up();
```

- `moveTo(x, y)`, `down()`, and `up()` dispatch at once — one round trip, no scroll, no stability wait, no polling; they take only `{signal}`. `moveToElement(target, {position, timeoutMs, ...})` is the one call that polls (until the element exists and is visible); it never scrolls, so a point outside the viewport fails — `scrollIntoView()` first.
- The pointer remembers its position (starting at 0, 0) and button: moves while pressed carry `buttons: 1`; a pen reports pressure 0.5 while down. `down()` twice or `up()` while released is `INVALID_ARGUMENT`. On a frame handle, coordinates are in the frame's viewport.
- `prepare()` releases a pointer left pressed and returns it to (0, 0); closing a frame handle releases its pointers.

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

Use `requests()`/`responses()` for history and `waitForRequest()` for an atomic winning observation. A request carries its outcome - `status` (0 until the response arrives) and `failure` (`""` unless it failed) - and the request query filters on both: `waitForRequest({url: endsWith("/orders"), status: 201})`, `requests({failed: true})`. `stubRequest()`, `abortRequest()`, `modifyRequest()`, `modifyResponse()`, and `routeResponse()` return first-match-wins handlers with `count()`, `stats()`, and `remove()`. Network-state methods cover cache, offline mode, latency, throughput, and connection type.

`routeResponse()` starts its callback timeout after the body is read. The body has a separate timeout of five seconds, or `timeoutMs` if longer. When Chrome refuses to provide a body (for example, on a redirect), Biloba continues the response without calling the callback and records the error in `stats()`.

`modifyResponse()` patches the response: what you leave out is inherited from the original. `routeResponse()` *replaces* it - an unset status means 200, and unset headers or body mean none - so hand back anything you want kept, including headers you read off the intercepted response.

```ts
const handler = await session.handleDialogs("prompt", {message: "Name?", promptText: "Ada"});
const download = await session.expectDownload({filename: endsWith(".csv")});
const bytes = await download.content();
await handler.remove();
```

Dialog history includes safely auto-handled dialogs and warnings. Download snapshots preserve active, complete, and cancelled state; content is bounded binary data.

## 8. Time-dependent UI: the page clock

Don't wait real seconds for a timer, and don't race one. Install the page clock before `navigate()`, pause it, and move time yourself:

```ts
const t0 = new Date("2024-02-02T08:00:00Z");
const clock = await session.installClock(t0);   // times: a Date or epoch ms; durations: ms
await clock.pauseAt(t0);
await session.navigate(`${baseUrl}/checkout`);
await session.locator("#hold-timer").expectText("10:00");

await clock.runFor(9 * 60_000);                  // every timer and frame that falls due, in order
await session.locator("#hold-timer").expectText("1:00");
await clock.fastForward(3_600_000);              // like waking a laptop: each due timer fires once
await session.locator("#hold-expired").expectVisible();
```

- Biloba itself stays on real time, so **polling never moves a paused clock**: `expectText("9")` on a countdown with the clock paused times out, and the failure ends with `This tab's page clock is paused at <t>; timers and animation frames won't run until you call runFor, fastForward, or resume.` Advance first, then assert.
- `runFor(ms, {timeoutMs})` gives up after 10s of real time by default (`TIMEOUT`, saying how far it got); a page that keeps scheduling work makes a long `runFor` slow — raise `timeoutMs` or `fastForward` instead.
- `setFixedTime(t)` pins `Date` while timers run; `setSystemTime(t)` moves the time without firing timers; `resume()` lets it flow again. `session.clock()` returns the installed clock (its methods fail with `INVALID_ARGUMENT` without one).
- Installed after `navigate()`, the clock misses timers the page already scheduled. CSS animations and transitions aren't on the clock (the screenshot freeze handles those); out-of-process frames (full Chrome's cross-site iframes) and other tabs run on real time. One clock per tab; frame handles refuse it.

## 9. Screenshots and visual assertions

`captureScreenshot()` returns bounded `Uint8Array` data by default or a daemon-owned path with `{output: "path", name}`. It works on a session or locator. A capture takes the page as it is, animations included; `frozen: true` freezes it the way `expectScreenshot()` does, and `hideCaret: true` hides only the caret. `viewport: true` captures what is on screen without resizing the viewport (a locator must be entirely in view), and `clip: {x, y, width, height}` captures a region of the viewport (page captures only; outside the viewport is `INVALID_ARGUMENT`).

`expectScreenshot(name, options)` supports masks, pixel and channel tolerances, automatic animation freezing, animated opt-out, light/dark schemes, baseline update mode, diff artifacts, and structured diagnosis.

```ts
const result = await session.getByTestId("chart").expectScreenshot("revenue-chart", {
  mask: [session.getByTestId("timestamp")],
  colorSchemes: ["light", "dark"],
});
expect(result.match).toBe(true);
```

Record what Chrome draws with a screencast:

```ts
const recording = await session.startScreencast({format: "jpeg", quality: 92}); // png, maxWidth, maxHeight, everyNthFrame
// ... drive the page ...
const frames = await recording.stop(); // [{data, timestamp, deviceWidth, deviceHeight, pageScaleFactor, scrollOffsetX, scrollOffsetY, offsetTop}]
```

- `stop()` waits for Chrome, so no frame arrives after it; frames are sorted by `timestamp` (epoch ms, comparable with `Date.now()` on Chrome's machine); calling it again returns the same frames.
- One recording per tab at a time; frame handles refuse with `INVALID_ARGUMENT`. `prepare()`/`close()` stop and **discard** a running recording — stop it inside the test to keep the frames.
- Chrome sends frames only when the page changes. For timing you rely on, record in `chrome-headless-shell`: full Chrome can deliver frames late, out of order, and with gaps under load.

## 10. Failures and diagnostics

Everything rejects with a `BilobaError` carrying `code`, `locator`, `expected`, `observed`, `trajectory`, `domOutline`, `screenshotPath`, `diagnostics`, `visual`, `artifactPaths`, and `daemonDetail` as applicable. Narrow on `code`: `NAVIGATION` and the crash codes (`PAGE_CRASHED`, `BROWSER_GONE`, `DRIVER_CLOSED`) mean something specific that waiting will not fix.

`trajectory` records every polling attempt, what it observed, and why it retried. Read it before widening a timeout: a flat line is a product bug, while a monotone approach suggests latency.

The full code table, `installBilobaVitestHooks`, `session.captureDiagnostics()`, and the console/warning streams → `biloba-vitest:debug-failures`.

---
layout: default
title: Biloba for Vitest
---
{% raw  %}
# Biloba for Vitest

Biloba's TypeScript client lets a `vitest` suite drive Biloba browser automation through a worker-local daemon and one shared Chrome.

> **Status: prototype.**  The package (`@onsi/biloba-vitest-prototype`) is not published to npm yet - today you build it from this repo - and its API will continue to shift before 1.0.  See the [support policy](./#support-policy).  What follows describes what works today.

### Claude Code skills

Biloba ships a dedicated TypeScript/Vitest plugin for Claude Code. The Biloba repository doubles as its marketplace:

```
/plugin marketplace add onsi/biloba
/plugin install biloba-vitest@biloba
```

The former `biloba@biloba` plugin now aliases only the Go client skills for backward compatibility. TypeScript users should uninstall it and install `biloba-vitest@biloba` instead.

The installed `biloba-vitest:*` skills activate automatically and can also be invoked explicitly:

| Skill | What it covers |
|---|---|
| `biloba-vitest:overview` | The daemon, shared-Chrome, session, and polling mental model. |
| `biloba-vitest:setup` | Vitest global setup, worker daemons, reusable sessions, and launch modes. |
| `biloba-vitest:write-tests` | Locators, actions, assertions, tabs, frames, network control, and realistic input. |
| `biloba-vitest:visual-assertions` | Screenshot baselines, masks, tolerances, color schemes, and diagnosis. |
| `biloba-vitest:debug-failures` | Structured errors, trajectories, artifacts, console output, and crash codes. |
| `biloba-vitest:flaky-tests` | Redundant polling, order dependence, lifecycle leakage, and latent races. |

### Why there's a daemon

Biloba's performance model is parallelization: one shared Chrome, with each parallel process driving its own isolated tab. Vitest workers are Node processes while Biloba's Chrome DevTools Protocol plumbing lives in the `bilobad` executable. Each worker therefore spawns its own daemon and talks to it over a framed JSON protocol on stdin/stdout. Every daemon attaches to one shared Chrome:

```
vitest worker 1  ──▶  bilobad  ──┐
vitest worker 2  ──▶  bilobad  ──┼──▶  one shared Chrome
vitest worker 3  ──▶  bilobad  ──┘
```

Creating isolated tabs is much cheaper than creating browsers; the daemon preserves that advantage across worker processes.

This has a consequence worth internalizing early: **polling happens on the daemon, not in your test.**  When you write `expectText("ready", {timeoutMs: 1000})`, that is *one* request.  The daemon runs the whole retry loop in-process, next to Chrome, and answers once with the outcome and the trajectory it took to get there.  You are not paying a round trip per attempt, which is what makes a 5ms polling interval a reasonable thing to ask for.

### Getting set up

You need the daemon binary.  It builds out of this repo:

```bash
go build -o .bin/bilobad ./cmd/bilobad
```

Start one Chrome for the whole run in vitest's global setup, and hand its websocket url to the workers:

```ts
// global-setup.ts
import {startSharedBrowser, type SharedBrowserProcess} from "@onsi/biloba-vitest-prototype";
import type {TestProject} from "vitest/node";

const daemonExecutable = process.env.BILOBA_DAEMON_EXECUTABLE;
if (!daemonExecutable) throw new Error("BILOBA_DAEMON_EXECUTABLE is not set");

let browser: SharedBrowserProcess | undefined;

export async function setup(project: TestProject): Promise<void> {
  browser = await startSharedBrowser({
    executable: daemonExecutable,
    mode: "headless-shell",
    windowSize: {width: 1024, height: 768},
  });
  project.provide("chromeConnection", browser.connection);
}

export async function teardown(): Promise<void> {
  await browser?.stop();
}
```

Then, in each test file, connect a daemon of your own and open a session:

```ts
import {inject} from "vitest";
import {connect, type Browser, type Session} from "@onsi/biloba-vitest-prototype";

let browser: Browser;
let session: Session;

beforeAll(async () => {
  browser = await connect({chromeConnection: inject("chromeConnection")});
  session = await browser.openSession();
});

afterAll(async () => { await browser.close(); });
```

`connect` reads the daemon's path from `BILOBA_DAEMON_EXECUTABLE` when you don't pass `daemonExecutable`, which is usually how you'll wire it up; pass it explicitly when you'd rather not depend on the environment.  Omit `chromeConnection` and the daemon launches Chrome itself - fine for a single file, wasteful for a suite.  The older `chromeWsUrl` attachment remains available, but it cannot report how an external Chrome was launched; prefer `chromeConnection` so every worker receives the host's validated launch metadata.

Both `startSharedBrowser` and a self-launching `connect` accept `mode: "headless-shell" | "headless" | "headful"`, `chromePath`, `autoInstall`, ordered `chromeArgs`, and `windowSize`.  The default is the fast headless shell at 1024×768.  `browser.launch` reports the resolved executable, mode, arguments, size, and whether Biloba installed the shell.  Set `BILOBA_INTERACTIVE=true` for the headful interactive default, or select a mode explicitly.

A `Session` is the TypeScript analogue of a Biloba tab.  A session returned by `browser.openSession()` owns its own browser context, so its cookies and storage are isolated from every other root session.  `session.prepare()` is `b.Prepare()` - it resets the session between tests and is what makes reuse cheap:

```ts
beforeEach(async () => { await session.prepare(); });
```

Open a sibling tab with `newTab()`.  It shares the parent's browser context - including cookies and storage - but has its own document lifecycle:

```ts
const popup = await session.newTab();
await popup.navigate("http://example.com/checkout");
await popup.activate();
await popup.close();
```

Use `tabs()` and `spawnedTabs()` for snapshots, `findTab()` for an optional match, and `waitForTab()` when a popup is expected.  `frames()` and `waitForFrame()` expose cross-origin frame targets as typed sessions.  Closing or preparing an owning session invalidates all of its descendant handles.

### Navigating and selecting

Navigation insists on an expected HTTP status:

```ts
await session.navigate("http://example.com/search?q=foo");
await session.navigateWithStatus("http://example.com/not-found", 404);
```

`navigate` asserting `200` is a deliberate assertion, not a transport rule.  An unexpected error page is a broken fixture far more often than it's the subject of the test, and letting one through surfaces three lines later as a baffling assertion failure rather than at the navigation that caused it.  When the 4xx page *is* what you meant to load, `navigateWithStatus` says so.

Selecting elements is done with locators, which are lazy - building one talks to nobody:

```ts
session.locator("#content")                            // css
session.getByTestId("name")                            // [data-testid="name"]
session.getByText("Save", {exact: true})               // by text content
session.getByRole("button", {name: "Increment"})       // by ARIA role, optionally by accessible name
session.getByLabel("Email")                            // by associated label
session.getByPlaceholder("Search")                     // by placeholder
session.getByAltText("Profile photo")                  // by image alt text
session.getByTitle("Close")                            // by title
session.xpath("//button[@name='save']")                 // a raw XPath expression
session.locator(".row").first()                        // just the first match
```

Use semantic locators for user-facing behavior and stable CSS hooks for structural state.

The XPath DSL is a runner-independent string builder.  Build an expression, then bind it to a session:

```ts
import {relativeXPath, xpath} from "@onsi/biloba-vitest-prototype";

const save = xpath("button").withClass("primary").withText("Save");
await session.xpath(save).click();

const languageList = xpath("ul").withChildMatching(
  relativeXPath("li").withText("Francais"),
);
await session.xpath(languageList).expectVisible();
```

The builder includes attributes, text, boolean predicates, tree axes, sibling axes, and XPath's one-based `first()`/`nth(position)`/`last()` positions. Locator-level `.nth(index)` remains zero-based after the expression is bound with `session.xpath(...)`.

### Acting and asserting

Actions and assertions hang off a locator and poll by default:

```ts
await session.getByTestId("name").setValue("Ada");
await session.getByRole("button", {name: "Increment"}).click();

await session.locator("#count").expectText("1");
await session.locator("#spinner").expectCount(0);
await session.getByTestId("name").expectValue("Ada");
await session.locator("a.home").expectAttribute("href", "/");
await session.getByRole("heading", {name: "Dashboard"}).expectVisible();
```

Two assertions live on the session rather than a locator, because they're about the page as a whole:

```ts
await session.expectUrl("/dashboard", {pathname: true});
await session.expectEvaluation("window.app.ready", true);
```

Every one of these takes `{timeoutMs, intervalMs, signal, mode}`. The default mode is `"eventually"`; `"immediate"` makes one attempt, and `"consistently"` requires the condition to remain true for the timeout. On actions, `{immediate: true}` is shorthand for `{mode: "immediate"}`:

```ts
await session.getByRole("button", {name: "Save"}).click({immediate: true});
```

Actions and assertions are async methods. Call them directly instead of wrapping them in another polling abstraction.

For interactions where browser-faithful input matters, call `realistic()` on the locator:

```ts
await session.getByRole("button", {name: "Pay"}).realistic().click();
await session.getByTestId("card-number").realistic().setValue("4242 4242 4242 4242");
```

The realistic track covers clicks and click variants, tap, hover, typing and value changes, wheel input, and drag.  Biloba scrolls the target into view, checks actionability, and uses real CDP input.  The fast default keeps each action atomic in the page runtime.

An assertion resolves to an `AssertionResult` describing how it got there, which is occasionally useful and always available:

```ts
const result = await session.locator("#delayed").expectText("ready", {timeoutMs: 1_000, intervalMs: 5});
expect(result.attemptCount).toBeGreaterThan(1);
```

### Running JavaScript

`evaluate` reads an expression, or calls a function when you pass an arguments array:

```ts
const title = await session.evaluate<string>("document.title");
const sum = await session.evaluate<number>("(a, b) => a + b", [40, 2]);
const now = await session.evaluate<number>("() => Date.now()", []);
```

The distinction is the *presence* of the array, not its length - `[]` still means "call this".  That's deliberate: it means a zero-argument function doesn't have to be spelled differently from a two-argument one.

Cookies accept `Date` for expiry.  You can read, filter, wait for, count, and clear them as well as set them:

```ts
await session.setCookies([{name: "session", value: "abc123", path: "/"}]);
const cookie = await session.expectCookie({name: "session"});
await session.clearCookies();
```

`localStorage()` and `sessionStorage()` expose typed set/get/remove/clear, snapshot, length, and polling assertions.

TypeScript supports request and response history, stubbing, aborting, request and response modification, callback-based response routing, response holding, cache control, and network emulation:

```ts
await session.expectRequest(endsWith("/orders"), {method: "POST"});

const stub = await session.stubRequest(endsWith("/feature-flags"), {
  status: 200,
  headers: [{name: "content-type", value: "application/json"}],
  body: new TextEncoder().encode('{"available":true}'),
});

const hold = await session.holdResponse(endsWith("/inventory"));
try {
  await session.getByRole("button", {name: "Refresh"}).click();
  const response = await hold.await();
  expect(response.status).toBe(200);
} finally {
  await hold.release();
}
```

Import `endsWith` (or another Biloba expectation) from the package.  Always release a hold.  Network handlers are first-match-wins; use each handler's `count()` or `stats()` and `networkShadowDiagnostics()` to prove that the intended handler claimed the request.

Dialog handlers are newest-first and removable.  Dialog history records both explicitly handled and safely auto-handled dialogs.  Downloads expose lifecycle metadata, bounded binary content, cancellation, snapshot filters, and polling assertions.

```ts
const prompts = await session.handleDialogs("prompt", {message: "Name?", promptText: "Ada"});
const download = await session.expectDownload({filename: endsWith(".csv")});
const bytes = await download.content();
await prompts.remove();
```

### Screenshots and visual assertions

Capture a page or locator as bounded bytes, or ask the daemon to write a sanitized artifact path:

```ts
const png = await session.captureScreenshot();
const path = await session.getByTestId("chart").captureScreenshot({output: "path", name: "chart"});
```

`expectScreenshot()` supports page and element baselines, masks, exact or tolerant pixel comparison, animation freezing with an opt-out, light and dark color schemes, update mode, diff artifacts, and structured diagnosis.

```ts
const result = await session.getByTestId("chart").expectScreenshot("revenue-chart", {
  mask: [session.getByTestId("last-updated")],
  colorSchemes: ["light", "dark"],
  pixelTolerance: 0.001,
});
expect(result.match).toBe(true);
```

A mismatch throws, and the message carries the pixel counts, amplitude verdict, and baseline, actual, and diff paths, so a CI log tells you what changed without opening anything. Capture warnings (a baseline that never settled, two color schemes that rendered identically) go to stderr; pass `onScreenshotWarning` to `connect` to route them somewhere else.

When you need to absorb rendering noise, reach for `channelTolerance` before `pixelTolerance`: rasterization differences are a small delta spread over many pixels, while a pixel budget lets a handful of pixels change by any amount - which is exactly the one-pixel border or small glyph worth catching.

### When something fails

Every failure arrives as a `BilobaError`, and it carries the context you'd otherwise have to go find:

```ts
try {
  await session.locator("#never").expectText("ready", {timeoutMs: 500});
} catch (error) {
  const failure = error as BilobaError;
  failure.code;            // "TIMEOUT"
  failure.locator;         // 'locator("#never")'
  failure.expected;        // "ready"
  failure.trajectory;      // every attempt, with what it observed and why it retried
  failure.domOutline;      // the DOM at failure time
  failure.screenshotPath;  // the primary PNG path, when disk artifacts are enabled
  failure.diagnostics;     // context-wide tab artifacts, when available
  failure.visual;          // structured visual comparison, for expectScreenshot
  failure.artifactPaths;   // all associated artifact paths
}
```

That trajectory records every polling attempt as structured data instead of reducing the failure to its final observation.  Pass `artifactDir` to `connect` to get screenshots written to disk.

For runner-level capture, load `installBilobaVitestHooks` from `@onsi/biloba-vitest-prototype/vitest` in a Vitest setup file.  The hook captures every live tab after any failed test, can capture a slow test after `progressAfterMs`, replays browser errors, and turns `console.assert` into a test-boundary failure.  `session.captureDiagnostics()` provides the same context-wide capture on demand.  Configure screenshots, outlines, artifact paths, inline output, viewport, byte limits, and poll trajectories under `connect({diagnostics: {...}})`; explicit members override CI and interactive defaults independently.

Use `consoleMessages()` for bounded history and `onConsoleMessage()` for live delivery.  `warnings()` and `onWarning()` expose auto-handled dialog and dropped-event warnings.  Pass `debugLog` to `connect` for bounded structured daemon/CDP diagnostics; Biloba never mixes them into framed stdout.

`failure.code` is worth narrowing on, because a few of the codes mean something quite specific:

| Code | What happened |
|---|---|
| `TIMEOUT` | The poll ran out of time.  The ordinary assertion failure. |
| `TARGET_NOT_FOUND` | No element matched the locator. |
| `TARGET_NOT_READY` | The element matched but refused the operation - hidden, disabled.  Means *not yet*, so a retry might succeed. |
| `NAVIGATION` | The page loaded with a status you didn't ask for.  Waiting will never fix it; see `navigateWithStatus`. |
| `JAVASCRIPT_ERROR` | Your expression threw in the page. |
| `INVALID_ARGUMENT` | The request was malformed - a cookie with no domain, say. |
| `PAGE_CRASHED` | This session's renderer died.  The browser is fine; navigate again to recover. |
| `BROWSER_GONE` | The shared Chrome exited or crashed underneath this worker. |
| `DRIVER_CLOSED` | This worker's daemon died.  `daemonDetail` carries its stderr. |

The last three exist so that a crash reports itself as a crash.  Chrome doesn't fail calls to a dead renderer - it stops answering them - so without a dedicated signal a crashed page looks exactly like an assertion that never came true, and sends you off to debug a test that was fine.

{% endraw  %}

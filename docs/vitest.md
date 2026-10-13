---
layout: default
title: Biloba for Vitest
---
{% raw  %}
# Biloba for Vitest

Biloba's TypeScript client lets a `vitest` suite drive Biloba browser automation through a worker-local daemon and one shared Chrome.

Biloba's [main documentation](./) is written for its Go client, but much of the thinking behind it carries over to this one.  In particular, [Nurturing Maintainable Suites](./#nurturing-maintainable-suites) covers flake hunts and keeping track of a suite's performance.  The ideas apply to a Vitest suite too, and the `biloba-vitest:flake-hunt` skill has a Vitest version of the scripts.

> **Status: pre-1.0.** The `biloba` package has an API that will continue to shift before 1.0.
> See the [support policy](./#support-policy). What follows describes what works today.

### Claude Code skills

Biloba ships a dedicated TypeScript/Vitest plugin for Claude Code. The Biloba repository doubles as its marketplace:

```
/plugin marketplace add onsi/biloba
/plugin install biloba-vitest@biloba
```

The former `biloba@biloba` plugin has been removed; it only ever carried the Go client skills. If you still have it installed, uninstall it and install `biloba-vitest@biloba` instead.

The installed `biloba-vitest:*` skills activate automatically and can also be invoked explicitly:

| Skill | What it covers |
|---|---|
| `biloba-vitest:overview` | The daemon, shared-Chrome, session, and polling mental model. |
| `biloba-vitest:setup` | Vitest global setup, worker daemons, reusable sessions, and launch modes. |
| `biloba-vitest:write-tests` | Locators, actions, assertions, tabs, frames, network control, and realistic input. |
| `biloba-vitest:visual-assertions` | Screenshot baselines, masks, tolerances, color schemes, and diagnosis. |
| `biloba-vitest:debug-failures` | Structured errors, trajectories, artifacts, console output, and crash codes. |
| `biloba-vitest:flaky-tests` | Redundant polling, order dependence, lifecycle leakage, and latent races. |
| `biloba-vitest:flake-hunt` | Running the suite many times to measure each test's failure rate, reading the JSON reports, and keeping a performance record. |

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

```bash
npm install -D vitest biloba
```

This pulls in `biloba` plus exactly one platform package - `biloba-darwin-arm64`, `biloba-darwin-x64`, `biloba-linux-x64`, or `biloba-linux-arm64` - chosen by npm for the machine running the install. That package carries the `bilobad` daemon binary (a few MB, no install scripts). Windows isn't supported yet: `biloba` has nothing to resolve there, and the error names the fallback, `go install github.com/onsi/biloba/cmd/bilobad@vX.Y.Z` plus `BILOBA_DAEMON_EXECUTABLE`.

Then, once per Chrome version, fetch the browser build the daemon drives:

```bash
npx biloba install-chrome
```

This runs `bilobad install-chrome`, which downloads Chrome for Testing's current Stable `chrome-headless-shell` into a per-user cache (`~/Library/Caches/biloba/…` on macOS, `~/.cache/biloba/…` on Linux) and prints the path it resolved to. Re-running it is a no-op once that version is already cached. Biloba never downloads Chrome on its own outside this command - `autoInstall: true` on `startSharedBrowser`/`connect` is the explicit opt-in for that - so run it once locally and again in CI (see below).

Chrome lookup, in order: an explicit `chromePath`, `BILOBA_CHROME_HEADLESS_SHELL`, `chrome-headless-shell` on `PATH`, then the newest build cached across the puppeteer and Biloba caches. When none of those find anything, the error names `npx biloba install-chrome`.

Linux on arm64 (including Docker on Apple silicon) is a partial case: `bilobad` installs and runs there, but Chrome for Testing does not publish a `chrome-headless-shell` build for that platform, so `install-chrome` can't help. Install your distro's Chromium package instead and point a full-Chrome launch at it:

```ts
await startSharedBrowser({mode: "headless", chromePath: "/usr/bin/chromium"});
```

The path varies by distro - check where your package manager put it. On Debian-based images, including the official `node` images, `apt-get install -y chromium` puts it at `/usr/bin/chromium`. Ubuntu's `chromium` package installs a snap, which doesn't run inside a container.

`biloba` supports Vitest 3, 4, and 5 (peer range `>=3 <6`). Vitest 5 requires Node 22.12 or later; `biloba` itself needs Node 20 or later. If you're on an older config with `poolOptions`/`minWorkers`, note those were removed in Vitest 4 - use top-level `maxWorkers`/`isolate` instead.

Wire the vitest config:

```ts
// vitest.config.ts
import {defineConfig} from "vitest/config";
export default defineConfig({
  test: {
    globalSetup: ["./test/global-setup.ts"],
    pool: "forks",
    testTimeout: 30_000,
    hookTimeout: 30_000,
  },
});
```

Start one Chrome for the whole run in vitest's global setup, and hand its connection to the workers:

```ts
// test/global-setup.ts
import {startSharedBrowser, type SharedBrowserConnection, type SharedBrowserProcess} from "biloba";
import type {TestProject} from "vitest/node";

declare module "vitest" {
  export interface ProvidedContext {
    chromeConnection: SharedBrowserConnection;
  }
}

let browser: SharedBrowserProcess | undefined;

export async function setup(project: TestProject): Promise<void> {
  browser = await startSharedBrowser({mode: "headless-shell"});
  project.provide("chromeConnection", browser.connection);
}

export async function teardown(): Promise<void> {
  await browser?.stop();
}
```

The `declare module "vitest"` block augments Vitest's `ProvidedContext` type; without it, `project.provide` and `inject` below don't type-check. Declare it once, in the global setup file - Vitest picks it up for every test file in the run.

Then, in each test file, connect a daemon of your own and open a session:

```ts
import {inject} from "vitest";
import {connect, type Browser, type Session} from "biloba";

let browser: Browser;
let session: Session;

beforeAll(async () => {
  browser = await connect({chromeConnection: inject("chromeConnection")});
  session = await browser.openSession();
});

afterAll(async () => { await browser.close(); });
```

`connect` and `startSharedBrowser` resolve the daemon executable the same way, in order: an explicit `daemonExecutable`/`executable` option, then `BILOBA_DAEMON_EXECUTABLE`, then the platform package installed alongside `biloba`. You normally don't pass either option - the platform package covers it. Reach for `BILOBA_DAEMON_EXECUTABLE` (or the option) to point at a daemon built from source, which is the path for Windows and for any platform npm doesn't ship a package for yet. If the platform package is missing at run time, the error calls out `--omit=optional`/`--no-optional` installs and a lockfile or `node_modules` moved between operating systems (a Mac install reused inside a Linux container, say) as the likely causes.

`bilobad version` reports the daemon's own version; when you've overridden the daemon and its version differs from the `biloba` package's, `connect` emits a `BILOBA_VERSION_MISMATCH` process warning (skipped for `dev` builds, which don't carry a comparable version).

Omit `chromeConnection` and the daemon launches Chrome itself - fine for a single file, wasteful for a suite.  The older `chromeWsUrl` attachment remains available, but it cannot report how an external Chrome was launched; prefer `chromeConnection` so every worker receives the host's validated launch metadata.

Both `startSharedBrowser` and a self-launching `connect` accept `mode: "headless-shell" | "headless" | "headful"`, `chromePath`, `autoInstall`, `chromeSandbox`, ordered `chromeArgs`, and `windowSize`.  The default is the fast headless shell at 1024×768.  `browser.launch` reports the resolved executable, mode, arguments, size, and whether Biloba installed the shell.  Set `BILOBA_INTERACTIVE=true` for the headful interactive default, or select a mode explicitly.

On Linux, in a headless mode, `bilobad` automatically launches Chrome with `--no-sandbox` when the process is running as root, or when the kernel reports AppArmor is restricting unprivileged user namespaces - the default on Ubuntu 23.10+, including GitHub's `ubuntu-latest` runner, which is otherwise where `chrome-headless-shell` fails to start with "No usable sandbox". Headful Chrome, and every other OS, are left alone. `chromeSandbox: true` forces the sandbox on, `chromeSandbox: false` forces it off (on any OS or mode); leave it unset for the automatic behavior. `browser.launch.chromeArgs` includes `--no-sandbox` whenever it was added, so you can see it happened. None of this applies when you attach to an existing browser via `chromeConnection` or `chromeWsUrl` - attaching launches nothing.

In CI, cache the Chrome download across runs and install it before the suite:

```yaml
- uses: actions/setup-node@v4
  with: {node-version: 22, cache: npm}
- uses: actions/cache@v4
  with: {path: ~/.cache/biloba, key: biloba-chrome-${{ runner.os }}}
- run: npm ci
- run: npx biloba install-chrome
- run: npx vitest run
```

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

Use `tabs()` and `spawnedTabs()` for snapshots, `findTab()` for an optional match, and `waitForTab()` when a popup is expected.

A session can emulate a device and its environment. Each setter has a matching `clear...()` (`resetPermissions()` for permissions), and `prepare()` clears them all:

```ts
await session.setDeviceMetrics({width: 390, height: 844, deviceScaleFactor: 3, mobile: true, touch: true, landscape: true});
await session.setUserAgent("Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) ...");
await session.setMedia({colorScheme: "dark", reducedMotion: "reduce"});
await session.setLocale("fr-FR");
await session.setTimezone("Europe/Paris");
```

`touch` makes `navigator.maxTouchPoints` positive and matches `(pointer: coarse)`; `landscape` reports a landscape `screen.orientation`. `setUserAgent()` changes both `navigator.userAgent` and the `User-Agent` request header. `setGeolocation()` and `setPermissions()` cover location and permission prompts.  Permissions use the names `navigator.permissions.query` takes - `"camera"`, `"microphone"`, `"geolocation"`, `"notifications"`, `"clipboard-read"`, `"clipboard-write"`, and the rest of the `Permission` type - and apply to an origin (`""` for every origin):

```ts
await session.setPermissions("http://localhost:3000", {camera: "granted", microphone: "denied"});
```

`frames()` snapshots the cross-origin iframe documents below a session; `waitForFrame()` polls for one by `url`, `title`, and/or a `has` locator. This covers both out-of-process frames (cross-site OOPIF targets) and same-site cross-origin frames such as two local servers on different ports. Biloba asks CDP for Chrome's frame tree and scopes operations directly to the selected frame; it does not read the iframe through parent-page JavaScript or relax the browser's same-origin policy:

```ts
const frame = await session.waitForFrame(
  {url: /child-form/, has: 'input[name="email"]'},
  {timeoutMs: 10_000},
);

await frame.locator('input[name="email"]').setValue("ada@example.com");
await frame.locator('button[type="submit"]').click();
await frame.locator("#success").expectVisible();
```

Frame handles expose the normal locator, action, assertion, JavaScript, upload, and frame-local storage APIs. Trusted pointer input is translated through the iframe owner's content geometry, including parent scrolling, borders, and CSS transforms. For a same-process frame, realistic actions also check the translated point against the whole tab, so an overlay in the embedding page that covers the frame fails the action instead of taking the click. `frame.evaluate()` runs in the selected frame's normal JavaScript environment, just like `session.evaluate()` does for a tab, so it can read and update globals created by the frame's own scripts. Same-origin iframe and open-shadow-root piercing remains `session.locator("outer >>> inner")`; `frames()` is the cross-origin boundary API.

A frame handle is a document, not a tab. Tab and browser-context controls fail on it with `INVALID_ARGUMENT`: `navigate()`, `prepare()`, `addInitScript()`, `activate()`, `newTab()`, `setWindowSize()`, the emulation setters (device metrics, geolocation, permissions, locale, timezone, media, user agent), `setCookies()`/`clearCookies()`, request stubbing, aborting, modifying, routing and holding, network state and cache, and `handleDialogs()`. Call them on the session that owns the frame. Reads that don't change the tab, such as `getCookies()` and `windowSize()`, still work.

A frame handle records its own document's console messages, page errors, and requests from the moment it is found, so `frame.expectConsoleMessage()` and `frame.waitForRequest()` work for either kind of frame. After navigation, request history keeps only the original document's requests; `waitForRequest()` and `expectNetworkIdle()` reject the stale handle. The owning tab also sees the console output and requests of its same-process frames, but not those of out-of-process frames. Dialogs from any frame are reported and handled on the owning tab. Network interception registered on the tab applies to its same-process frames' requests, not to an out-of-process frame's.

`frame.captureScreenshot()` and a page-level `frame.expectScreenshot()` capture the frame's viewport, and a `clip` is a region of the frame's viewport; element screenshots and masks work inside the frame. Chrome can only capture top-level targets, so screenshots on an out-of-process frame's handle fail with `INVALID_ARGUMENT`; capture the iframe element from the owning session instead.

An iframe with no `src`, or with a `srcdoc`, shares the page's origin, so it is not listed here: reach into it with `>>>`.

If several frames match a `waitForFrame()` query, it returns a ready match without waiting for other renderers. Use a specific URL, title, or `has` locator when you need a particular frame.

`frame.frameId` is the CDP frame identity, while `frame.targetId` is the renderer target that owns it; several same-process frames can therefore share a target. Discovery follows actual frame ancestry across nested renderer boundaries, so a root session includes same-process descendants inside an OOPIF while frame-local discovery excludes siblings. `frame.frameUrl` is the URL snapshot from discovery. Removing, replacing, or navigating the iframe—or navigating its parent tab—makes that document handle stale; its next operation fails with `TARGET_NOT_FOUND`, and `waitForFrame()` returns the replacement document's new handle. Closing a non-owning frame handle does not close independently acquired nested handles. Closing or preparing an owning session invalidates all descendant handles locally with `DRIVER_CLOSED`.

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
import {relativeXPath, xpath} from "biloba";

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

The realistic track covers clicks and click variants, tap, hover, typing and value changes, wheel input, and drag.  Biloba scrolls the target into view when it is not already fully on screen and topmost at its center, checks actionability, and uses real CDP input.  The fast default keeps each action atomic in the page runtime.

An assertion resolves to an `AssertionResult` describing how it got there, which is occasionally useful and always available:

```ts
const result = await session.locator("#delayed").expectText("ready", {timeoutMs: 1_000, intervalMs: 5});
expect(result.attemptCount).toBeGreaterThan(1);
```

### Driving the pointer directly

The realistic track measures, scrolls, and waits before every interaction, and `dragTo()` is a single gesture from one element to another.  When you need a gesture of your own - a pen stroke on a drawing canvas, a slider scrubbed at 60Hz while you record the screen - `session.pointer()` gives you a real pointer to drive one event at a time:

```ts
const pen = session.pointer({type: "pen"});
await pen.moveTo(100, 200);
await pen.down();
for (let i = 1; i <= 14; i++) {
  await new Promise((resolve) => setTimeout(resolve, 16));
  await pen.moveTo(100 + i * 10, 200);
}
await pen.up();
```

The pointer has four methods:

- `moveTo(x, y)` moves to a point in CSS pixels from the top-left of the viewport.
- `moveToElement(target, {position})` moves to the element's center, or to `position` from its top-left corner: `await p.moveToElement("#track", {position: {x: 0, y: 10}})`.  The target is a CSS selector or a locator from the same session.
- `down()` presses the primary button where the pointer is, and `up()` releases it.  Pressing a pressed pointer, or releasing a released one, is an `INVALID_ARGUMENT` error.

The pointer remembers its position (it starts at `0, 0`) and whether it is pressed.  Moves while it is pressed carry `buttons: 1`, so the page sees a drag.  `session.pointer()` is a mouse; `session.pointer({type: "pen"})` is a pen, and every event it dispatches reaches the page as a `PointerEvent` with `pointerType` `"pen"`, reporting a `pressure` of `0.5` while it touches and `0` while it hovers.  A pen works on a page emulating a tablet (`setDeviceMetrics({width: 820, height: 1180, mobile: true, touch: true})`).

Every call dispatches real CDP input immediately - `moveTo`, `down`, and `up` are one round trip each and take only a `signal`.  Nothing scrolls, nothing waits for the page to settle, and nothing is checked for occlusion, so the timing is yours.  `moveToElement` is the one call that polls: it waits for the element to exist and be visible, honoring `{timeoutMs, intervalMs, signal, mode, immediate}` like any action, then dispatches.  It reads the element where it is, so an element whose point is outside the viewport fails instead of receiving input it could never get: scroll it into view first with `scrollIntoView()`.

On a frame handle from `waitForFrame()`, `frame.pointer()` works in the frame's viewport: Biloba translates each point through the iframe's position (without scrolling the iframe into view), and the frame's page sees the coordinates you passed.

The daemon keeps each pointer's position and button state.  `prepare()` releases a pointer the test left pressed, so the next test does not start with a held button, and returns every pointer to `0, 0`.  Closing a frame handle releases its pressed pointers too.

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

Each request in `requests()` carries its outcome: `status` is the response's HTTP status (0 until the headers arrive, and 0 for a request that failed without a response), and `failure` is Chrome's error text (`net::ERR_CONNECTION_REFUSED`, `net::ERR_ABORTED`, ...) or `""`.  A redirect is one request per hop, and a hop that was redirected has the redirect's status.  An HTTP error status is not a failure.  `requests()` and `waitForRequest()` filter on both, and each request is a snapshot of what Biloba knew when you asked:

```ts
await session.waitForRequest({url: endsWith("/orders"), status: 201});
const broken = await session.requests({failed: true});
const serverErrors = await session.requests({url: contains("/api/"), status: numeric(">=", 500)});
```

`expectNetworkIdle()` waits for the session's in-flight requests to finish.  When it times out, its message names each request still in flight as of its last attempt - method, URL, resource type, and how long it had been in flight - oldest first.

Import `endsWith` (or another Biloba expectation) from the package.  Always release a hold.  Network handlers are first-match-wins; use each handler's `count()` or `stats()` and `networkShadowDiagnostics()` to prove that the intended handler claimed the request.

`routeResponse()` starts its callback timeout after reading the response body. Body reads have a separate timeout of five seconds, or `timeoutMs` if it is longer. For example, `{timeoutMs: 30_000}` allows up to 30 seconds to read the body, then another 30 seconds for the callback. If Chrome refuses to provide a body, as it does for redirects, Biloba continues the response without calling the callback and records the error in the handler's `stats()`.

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

A capture takes the page as it is, animations and all.  Pass `frozen: true` to capture it the way `expectScreenshot()` does: Biloba waits for web fonts, then turns off CSS animations and transitions and hides the text caret for the duration of the capture (animations on a scroll or view timeline keep running).  `hideCaret: true` hides only the caret.

A page capture takes the whole document, and when the document is bigger than the viewport Chrome lays the page out again at the full document size to do that, which a responsive page can notice.  `viewport: true` captures exactly what is on screen and never resizes the viewport; on a locator it refuses an element that is not entirely in view.  `clip` captures one region of the viewport, in CSS pixels from its top-left corner, and also never resizes it: a region that reaches outside the viewport is an `INVALID_ARGUMENT` error.  `clip` applies to page captures only (a locator capture is already clipped to its element).

```ts
const aboveTheFold = await session.captureScreenshot({viewport: true});
const header = await session.captureScreenshot({clip: {x: 0, y: 0, width: 800, height: 120}, frozen: true});
```

`expectScreenshot()` supports page and element baselines (an element's box is rounded outward to whole CSS pixels, so a fractionally sized element is captured with every pixel it touches), masks, exact or tolerant pixel comparison, animation freezing with an opt-out, light and dark color schemes, update mode, diff artifacts, and structured diagnosis.

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

### Recording a screencast

`session.startScreencast()` records the frames Chrome draws for a tab until you stop it:

```ts
const recording = await session.startScreencast({format: "jpeg", quality: 92});
// ... drive the page ...
const frames = await recording.stop(); // readonly ScreencastFrame[]
```

The options are a `format` (`"jpeg"`, the default, or `"png"`), a JPEG `quality` from 0 to 100 (0 leaves it to Chrome), `maxWidth` and `maxHeight` to shrink the frames (Chrome keeps the aspect ratio and never enlarges them), and `everyNthFrame` to keep only one frame in every n.

Each frame carries the encoded image in `data` (a `Uint8Array`) and a `timestamp`: the moment Chrome swapped that frame onto the screen, in milliseconds since the Unix epoch.  It is wall-clock time on the machine running Chrome, so on the same machine you can line frames up against `Date.now()` readings you took while driving the page.  The frame also carries the metadata that maps the image onto the page: `deviceWidth` and `deviceHeight` (the viewport the image shows, in CSS pixels), `pageScaleFactor`, `scrollOffsetX` and `scrollOffsetY`, and `offsetTop`.

`recording.stop()` stops the screencast and waits for Chrome to confirm it, so no frame arrives after it resolves.  It returns the frames in `timestamp` order, and calling it again returns the same frames.  A tab records one screencast at a time - starting a second before stopping the first is an `INVALID_ARGUMENT` error - and a frame handle refuses `startScreencast()` with `INVALID_ARGUMENT`.  `prepare()` and `close()` stop a recording that is still running and discard it, so stop a recording before the test ends to keep its frames; `stop()` afterwards fails with `TARGET_NOT_FOUND`.

The daemon holds the frames until `stop()` collects them, and sends them in several responses when there are more than fit in one, so a long recording reaches the test intact.

Some things to know:

- **Chrome sends frames only when the page changes.**  You get the frame already on screen when the recording starts (so its timestamp can be a little earlier than your call to `startScreencast()`), and then one frame each time the page draws a new one, up to the display's 60 frames a second.  A page that isn't changing yields one frame and then nothing.
- **Frames are the size of the window, in CSS pixels.**  An emulated device scale factor changes how the page renders but does not enlarge the frames.  Use `captureScreenshot()` for a full-resolution still.
- **Frames are held in memory** until `stop()` returns them.  At 1024x768 a JPEG frame is roughly 40-60KB, a PNG frame 500KB or more, so ten seconds of a busy page at 60 frames a second is tens of megabytes as JPEG.  Shrink the frames with `maxWidth`/`maxHeight`, or record fewer with `everyNthFrame`, for long recordings.
- **The two Chrome builds behave differently.**  `chrome-headless-shell` (the default) delivers frames steadily, in order, each timestamped within a few milliseconds of being drawn - even while other workers drive their own tabs.  Full Chrome (`mode: "headless"`) encodes frames on several threads and slows down when other tabs in the same browser are busy: frames can arrive out of order (which is why `stop()` sorts them), a recording can go a few hundred milliseconds without a frame, a frame can be timestamped hundreds of milliseconds after it was drawn, and with an emulated device scale factor the first frame or two can ignore `maxWidth`/`maxHeight`.  For frame timing you intend to rely on, record in `chrome-headless-shell`.

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

That trajectory records every polling attempt as structured data instead of reducing the failure to its final observation.  Pass `diagnostics: {artifactDir}` to `connect` to get screenshots written to disk.

For runner-level capture, load `installBilobaVitestHooks` from `biloba/vitest` in a Vitest setup file.  The hook captures every live tab after any failed test, can capture a slow test after `progressAfterMs`, replays browser errors, and turns `console.assert` into a test-boundary failure.  `session.captureDiagnostics()` provides the same context-wide capture on demand.  Configure screenshots, outlines, artifact paths, inline output, viewport, byte limits, and poll trajectories under `connect({diagnostics: {...}})`; explicit members override CI and interactive defaults independently.

Use `consoleMessages()` for bounded history, `onConsoleMessage()` for live delivery, and `expectConsoleMessage()` to wait for one.  Alongside the page's own console calls, the history holds the messages Chrome logs itself and DevTools shows in its console - a failed resource load, an intervention, a performance violation, a deprecation.  Their `source` names the origin (`"network"`, `"intervention"`, ...) and `type` is the level; the page's own messages have `source: ""`.  Chrome's messages are recorded on the tab, not on a frame handle.

Uncaught exceptions and unhandled promise rejections are page errors: `pageErrors()` returns them, `onPageError()` delivers them live, and `expectPageError(expected)` waits for one whose message matches.  A page error's `message` reads the way DevTools prints it (`"Uncaught TypeError: x is not a function"`), and `url`, `line`, and `column` locate where it was thrown.  The `line` and `column` fields of a page error and of a stack frame are zero-based, as Chrome reports them; when Biloba prints a location it prints `url:line:column` one-based, the way editors and DevTools show it.  `prepare()` clears both histories, and what the previous test's document reports while `prepare()` navigates it away (a fetch it still had open rejecting, say) is dropped rather than landing in the next test.

The Vitest hooks replay the test's console errors and page errors after it finishes (`[browser error] ...`, `[browser pageerror] Uncaught Error: boom (http://localhost:3000/app.js:12:5)`).  Chrome's own messages are not replayed, warnings and errors included: they stay in `consoleMessages()`.  `warnings()` and `onWarning()` expose auto-handled dialog and dropped-event warnings.  Pass `debugLog` to `connect` for bounded structured daemon/CDP diagnostics; Biloba never mixes them into framed stdout.

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

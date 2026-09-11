---
name: setup
description: Wire Biloba's TypeScript client into Vitest — install biloba and its platform package, install chrome-headless-shell, start one shared Chrome in global setup, provide its connection to workers, create one daemon and reusable root Session per test file, prepare between tests, close cleanly, and choose launch modes/options. Use when installing biloba or changing suite-level browser/daemon lifecycle.
---

# Setting up Biloba for Vitest

Install `biloba`; it pulls in the `bilobad` daemon binary via a per-platform npm package — no Go toolchain needed:

```bash
npm install -D vitest biloba
```

That resolves to `biloba` plus exactly one of `biloba-darwin-arm64`, `biloba-darwin-x64`, `biloba-linux-x64`, `biloba-linux-arm64`. Windows isn't supported yet — the daemon-resolution error there names building `bilobad` from source (`go install github.com/onsi/biloba/cmd/bilobad@vX.Y.Z`) plus `BILOBA_DAEMON_EXECUTABLE` as the workaround.

Then fetch the Chrome build the daemon drives, once per Chrome version:

```bash
npx biloba install-chrome
```

This runs `bilobad install-chrome`: downloads Chrome for Testing's current Stable `chrome-headless-shell` into a per-user cache (`~/Library/Caches/biloba` on macOS, `~/.cache/biloba` on Linux) and prints the path. A no-op once that version is cached. Biloba never downloads Chrome silently — `autoInstall: true` is the opt-in. Chrome lookup order: an explicit `chromePath`, `BILOBA_CHROME_HEADLESS_SHELL`, `chrome-headless-shell` on `PATH`, then the newest build across the puppeteer and Biloba caches. On Linux arm64, Chrome for Testing ships no `chrome-headless-shell` build at all; install a distro Chromium and launch it explicitly: `startSharedBrowser({mode: "headless", chromePath: "/usr/bin/chromium"})` (path varies by distro; on Debian-based images such as the official `node` images, `apt-get install -y chromium`; Ubuntu's `chromium` is a snap that doesn't run in containers).

Start one Chrome for the entire run:

```ts
// global-setup.ts
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

The `declare module "vitest"` augmentation is required — `project.provide`/`inject` don't type-check without it. Declare it once, in the global setup file.

Create one daemon and root session in each test file:

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

Register `globalSetup` in the Vitest config. Keep test files parallel: the architecture assumes one worker process and daemon per file.

## Launch choices

- `connect`/`startSharedBrowser` resolve the daemon executable in order: an explicit `daemonExecutable`/`executable` option, `BILOBA_DAEMON_EXECUTABLE`, then the platform package alongside `biloba`. Both overrides exist for a source build or an unsupported platform — you don't normally need either.
- If the platform package is missing at run time (`--omit=optional`, or `node_modules` installed on a different OS than it's running on), the error names both causes.
- `bilobad version` reports the daemon's own version. If an overridden daemon's version differs from the `biloba` package's, `connect` emits a `BILOBA_VERSION_MISMATCH` process warning (skipped for `dev` builds).
- Pass `chromeConnection` from global setup for a suite. Omitting it launches one Chrome per daemon and is suitable only for a single file or isolated debugging.
- Prefer `chromeConnection` over the legacy `chromeWsUrl`; the structured connection retains validated launch metadata.
- `startSharedBrowser` and a self-launching `connect` accept `mode`, `chromePath`, `autoInstall`, `chromeSandbox`, ordered `chromeArgs`, and `windowSize`.
- Modes are `"headless-shell"`, `"headless"`, and `"headful"`; the default viewport is 1024×768.
- **Linux sandbox:** on Linux, in a headless mode, `bilobad` auto-adds `--no-sandbox` when running as root or when the kernel reports AppArmor is restricting unprivileged user namespaces (the default on Ubuntu 23.10+/`ubuntu-latest`, where a cached `chrome-headless-shell` otherwise fails with "No usable sandbox"). Headful and every other OS are untouched. `chromeSandbox: true`/`false` overrides the automatic decision; unset it to leave it automatic. Doesn't apply when attaching via `chromeConnection`/`chromeWsUrl` — attaching launches nothing.
- Use `diagnostics` to configure failure, progress, and on-demand capture. `artifactDir` is a compatibility alias.
- `biloba` supports Vitest 3, 4, and 5 (peer range `>=3 <6`); Vitest 5 needs Node ≥22.12, `biloba` itself needs Node ≥20. `poolOptions`/`minWorkers` are gone as of Vitest 4 — use top-level `maxWorkers`/`isolate`.

## Lifecycle rules

- Reuse the root session and call `prepare()` before every test.
- Use `newTab()` for sibling tabs in the same browser context. Root sessions have isolated cookies and storage.
- Close `Browser` in `afterAll`; global teardown stops the shared Chrome.
- Do not hide a missing daemon path by skipping tests. Fail setup loudly.

Authoring → `biloba-vitest:write-tests`. Diagnostics → `biloba-vitest:debug-failures`. Docs: <https://onsi.github.io/biloba/vitest.html>.

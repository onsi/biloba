---
name: setup
description: Wire Biloba's TypeScript client into Vitest — build and locate bilobad, start one shared Chrome in global setup, provide its connection to workers, create one daemon and reusable root Session per test file, prepare between tests, close cleanly, and choose launch modes/options. Use when installing @onsi/biloba-vitest-prototype or changing suite-level browser/daemon lifecycle.
---

# Setting up Biloba for Vitest

Build `bilobad` from the Biloba repository and expose its absolute path:

```bash
go build -o .bin/bilobad ./cmd/bilobad
export BILOBA_DAEMON_EXECUTABLE="$PWD/.bin/bilobad"
```

Start one Chrome for the entire run:

```ts
// global-setup.ts
import {startSharedBrowser, type SharedBrowserProcess} from "@onsi/biloba-vitest-prototype";
import type {TestProject} from "vitest/node";

const executable = process.env.BILOBA_DAEMON_EXECUTABLE;
if (!executable) throw new Error("BILOBA_DAEMON_EXECUTABLE is not set");

let browser: SharedBrowserProcess | undefined;

export async function setup(project: TestProject): Promise<void> {
  browser = await startSharedBrowser({executable});
  project.provide("chromeConnection", browser.connection);
}

export async function teardown(): Promise<void> {
  await browser?.stop();
}
```

Create one daemon and root session in each test file:

```ts
import {inject} from "vitest";
import {connect, type Browser, type Session} from "@onsi/biloba-vitest-prototype";

let browser: Browser;
let session: Session;

beforeAll(async () => {
  browser = await connect({chromeConnection: inject("chromeConnection")});
  session = await browser.openSession();
});

beforeEach(async () => { await session.prepare(); });
afterAll(async () => { await browser.close(); });
```

Declare the injected value in Vitest's type augmentation and register `globalSetup` in the Vitest config. Keep test files parallel: the architecture assumes one worker process and daemon per file.

## Launch choices

- `connect` reads `BILOBA_DAEMON_EXECUTABLE` when `daemonExecutable` is omitted.
- Pass `chromeConnection` from global setup for a suite. Omitting it launches one Chrome per daemon and is suitable only for a single file or isolated debugging.
- Prefer `chromeConnection` over the legacy `chromeWsUrl`; the structured connection retains validated launch metadata.
- `startSharedBrowser` and a self-launching `connect` accept `mode`, `chromePath`, `autoInstall`, ordered `chromeArgs`, and `windowSize`.
- Modes are `"headless-shell"`, `"headless"`, and `"headful"`; the default viewport is 1024×768.
- Use `diagnostics` to configure failure, progress, and on-demand capture. `artifactDir` is a compatibility alias.

## Lifecycle rules

- Reuse the root session and call `prepare()` before every test.
- Use `newTab()` for sibling tabs in the same browser context. Root sessions have isolated cookies and storage.
- Close `Browser` in `afterAll`; global teardown stops the shared Chrome.
- Do not hide a missing daemon path by skipping tests. Fail setup loudly.

Authoring → `biloba-vitest:write-tests`. Diagnostics → `biloba-vitest:debug-failures`. Docs: <https://onsi.github.io/biloba/vitest.html>.

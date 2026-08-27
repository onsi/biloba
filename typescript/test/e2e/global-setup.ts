import {createReadStream} from "node:fs";
import {mkdtemp, rm} from "node:fs/promises";
import {createServer, type Server} from "node:http";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {fileURLToPath} from "node:url";
import type {TestProject} from "vitest/node";

import {startSharedBrowser, type SharedBrowserProcess} from "../../src/index.js";

const fixturePath = fileURLToPath(new URL("../../../fixtures/graft-parity.html", import.meta.url));

declare module "vitest" {
  export interface ProvidedContext {
    chromeWsUrl: string;
    chromePid: number;
    baseUrl: string;
    rendezvousDir: string;
    daemonExecutable: string;
  }
}

let browser: SharedBrowserProcess | undefined;
let server: Server | undefined;
let rendezvousDir: string | undefined;

// One Chrome for the whole run, started here in the main process - before any worker exists - and
// handed to every worker.  That is the topology this suite exists to prove: N test-runner worker
// *processes*, one bilobad each, all attached to this single browser.
export async function setup(project: TestProject): Promise<void> {
  const daemonExecutable = process.env.BILOBA_DAEMON_EXECUTABLE;
  if (!daemonExecutable) {
    throw new Error(
      "BILOBA_DAEMON_EXECUTABLE is not set, so the multi-worker end-to-end suite cannot run.\n" +
      "Run `make driver-e2e` from the repository root, or build the daemon with " +
      "`go build -o .bin/bilobad ./cmd/bilobad` and point BILOBA_DAEMON_EXECUTABLE at it.",
    );
  }

  server = createServer((_request, response) => {
    response.setHeader("content-type", "text/html");
    createReadStream(fixturePath).pipe(response);
  });
  const listening = server;
  await new Promise<void>((resolve, reject) => {
    listening.once("error", reject);
    listening.listen(0, "127.0.0.1", resolve);
  });
  const address = listening.address();
  if (!address || typeof address === "string") throw new Error("fixture server did not bind TCP");

  rendezvousDir = await mkdtemp(join(tmpdir(), "biloba-e2e-rendezvous-"));
  browser = await startSharedBrowser({executable: daemonExecutable});

  project.provide("chromeWsUrl", browser.wsURL);
  project.provide("chromePid", browser.pid);
  project.provide("baseUrl", `http://127.0.0.1:${address.port}`);
  project.provide("rendezvousDir", rendezvousDir);
  project.provide("daemonExecutable", daemonExecutable);
}

export async function teardown(): Promise<void> {
  await browser?.stop();
  if (server) await new Promise<void>((resolve, reject) => server!.close((error) => error ? reject(error) : resolve()));
  if (rendezvousDir) await rm(rendezvousDir, {recursive: true, force: true});
}

import {afterAll, beforeAll, expect, it} from "vitest";

import {claimState, connectWorker, expectOwnStateOnly, expectScreenshotIsolation, rendezvous, type Worker} from "./harness.js";

// The worker that leaves early.  Its job is to prove that one worker finishing - closing its
// browser handle, which stops its bilobad and disposes its browser context - takes nothing away
// from the workers still running, and in particular does not take down the shared Chrome.
const NAME = "gamma";
let worker: Worker;
let closed = false;

beforeAll(async () => { worker = await connectWorker(NAME); });
afterAll(async () => { if (!closed) await worker?.browser.close(); });

it("runs in its own worker process alongside the others", async () => {
  const peers = await rendezvous("connected", NAME, 3);
  expect(new Set(peers.map((peer) => peer["pid"])).size).toBe(3);
});

it("keeps its browser state to itself while the other workers write theirs", async () => {
  await claimState(worker);
  await rendezvous("claimed", NAME, 3);

  await expectOwnStateOnly(worker);
});

it("keeps screenshot bytes and paths isolated from other workers", async () => { await expectScreenshotIsolation(worker); });

it("closes its daemon while the other workers are still working", async () => {
  await rendezvous("ready-for-close", NAME, 3);

  await worker.browser.close();
  closed = true;

  // Announce only after the close has fully returned, so alpha and beta assert against a world
  // where this worker's daemon is genuinely gone rather than on its way out.
  await rendezvous("gamma-closed", NAME, 3);
  expect(closed).toBe(true);
});

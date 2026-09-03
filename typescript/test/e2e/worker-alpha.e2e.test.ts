import {afterAll, beforeAll, expect, inject, it} from "vitest";

import {claimState, connectWorker, expectOwnStateOnly, rendezvous, type Worker} from "./harness.js";

// One of three files, therefore one of three vitest worker processes, each with its own bilobad
// attached to the run's single shared Chrome.  See worker-gamma.e2e.test.ts for the teardown half.
const NAME = "alpha";
let worker: Worker;

beforeAll(async () => { worker = await connectWorker(NAME); });
afterAll(async () => { await worker?.browser.close(); });

it("runs in its own worker process alongside the others", async () => {
  const peers = await rendezvous("connected", NAME, 3);

  const pids = new Set(peers.map((peer) => peer["pid"]));
  expect(pids.size, "each test file must get its own worker process").toBe(3);
  expect(pids.has(process.pid)).toBe(true);
  expect(pids.has(inject("chromePid")), "workers must be separate from the browser host").toBe(false);
});

it("keeps its browser state to itself while the other workers write theirs", async () => {
  await claimState(worker);
  // Everyone has written before anyone reads, so this is a genuine concurrent-isolation check
  // rather than three sequential ones that would pass even with a shared context.
  await rendezvous("claimed", NAME, 3);

  await expectOwnStateOnly(worker);
});

it("survives another worker closing its daemon mid-run", async () => {
  await rendezvous("ready-for-close", NAME, 3);
  await rendezvous("gamma-closed", NAME, 3);

  // gamma's daemon and session are gone.  Ours must be untouched, and the shared Chrome alive.
  await expectOwnStateOnly(worker);
  await worker.session.getByRole("heading", {name: "Biloba parity"}).expectVisible();
  expect(await worker.session.evaluate<number>("1 + 1")).toBe(2);
});

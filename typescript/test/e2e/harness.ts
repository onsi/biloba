import {mkdir, readdir, readFile, writeFile} from "node:fs/promises";
import {join} from "node:path";
import {expect, inject} from "vitest";

import {connect, type Browser, type Session} from "../../src/index.js";

export interface Worker {
  readonly name: string;
  readonly browser: Browser;
  readonly session: Session;
  readonly baseUrl: string;
}

/**
 * Attaches this worker process to the run's single shared Chrome, through a bilobad of its own.
 * One daemon per worker, one Chrome for all of them.
 */
export async function connectWorker(name: string): Promise<Worker> {
  const browser = await connect({
    daemonExecutable: inject("daemonExecutable"),
    chromeWsUrl: inject("chromeWsUrl"),
  });
  const session = await browser.openSession();
  await session.prepare();
  await session.navigate(inject("baseUrl"));
  return {name, browser, session, baseUrl: inject("baseUrl")};
}

/**
 * A file-backed barrier across worker processes.
 *
 * It is doing two jobs.  The obvious one is ordering: hold every worker at a known point so the
 * next assertion happens while the others are demonstrably still live.  The subtler one is that it
 * *proves the workers are concurrent processes at all* - if vitest ran these files serially, or in
 * one process, the first worker to arrive would wait here until it timed out.  A green run is
 * therefore evidence of the topology, not just of the assertions inside it.
 */
export async function rendezvous(stage: string, name: string, expected: number, payload: unknown = {}): Promise<Record<string, unknown>[]> {
  const directory = join(inject("rendezvousDir"), stage);
  await mkdir(directory, {recursive: true});
  await writeFile(join(directory, `${name}.json`), JSON.stringify({name, pid: process.pid, ...(payload as object)}));

  const deadline = Date.now() + 30_000;
  for (;;) {
    const entries = await readdir(directory);
    if (entries.length >= expected) {
      return await Promise.all(entries.map(async (entry) =>
        JSON.parse(await readFile(join(directory, entry), "utf8")) as Record<string, unknown>));
    }
    if (Date.now() > deadline) {
      throw new Error(
        `rendezvous "${stage}" timed out: saw ${entries.length} of ${expected} workers (${entries.join(", ") || "none"}).\n` +
        "The e2e suite requires its test files to run concurrently, in separate processes - check " +
        "fileParallelism/minForks in vitest.e2e.config.ts, and that no file failed before reaching this point.",
      );
    }
    await new Promise((resolve) => setTimeout(resolve, 25));
  }
}

/** Asserts this worker sees only the state it created - no bleed from the workers beside it. */
export async function expectOwnStateOnly(worker: Worker): Promise<void> {
  const state = await worker.session.evaluate<{cookie: string; stored: string | null}>(
    `({cookie: document.cookie, stored: window.localStorage.getItem("owner")})`,
  );
  expect(state.stored, `${worker.name} should see only its own localStorage`).toBe(worker.name);
  expect(state.cookie, `${worker.name} should see only its own cookie`).toBe(`owner=${worker.name}`);
}

/** Writes this worker's marker state into its own isolated browser context. */
export async function claimState(worker: Worker): Promise<void> {
  await worker.session.setCookies([{name: "owner", value: worker.name, domain: "127.0.0.1", path: "/"}]);
  await worker.session.evaluate(`window.localStorage.setItem("owner", ${JSON.stringify(worker.name)})`);
}

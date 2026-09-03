import {execSync, spawn} from "node:child_process";
import {mkdtemp, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {afterAll, beforeAll, expect, inject, it} from "vitest";

import {BilobaError, connect} from "../../src/index.js";
import {connectWorker, type Worker} from "./harness.js";

// The crash scenarios, in one file and in a deliberate order: the recoverable ones first, the
// terminal one last, because it destroys the Chrome the others need.  vitest runs `it`s in source
// order within a file, which makes that ordering explicit and local rather than a property of how
// the runner happens to schedule files.  This file gets its own vitest run - and so its own Chrome -
// for the same reason (see vitest.e2e-crash.config.ts).
//
// What every case here pins is a *diagnosis*.  A crash that reports itself as an assertion timeout
// is worse than useless: it says the page never reached the expected state, which sends the reader
// off to debug a test that was fine.
let worker: Worker;

beforeAll(async () => { worker = await connectWorker("crash"); });
afterAll(async () => { try { await worker?.browser.close(); } catch { /* expected once Chrome is gone */ } });

it("names a crashed page instead of hanging until the deadline", async () => {
  await worker.session.getByRole("heading", {name: "Biloba parity"}).expectVisible();

  killRenderers();

  // Chrome does not fail calls to a dead renderer, it stops answering them.  So this first call is
  // the one that used to sit on a renderer that would never reply, for the whole 30s default
  // deadline, before reporting a timeout - the session interrupts it on Inspector.targetCrashed
  // instead.  The bound is what pins that: without the interrupt this takes 30s.
  const started = Date.now();
  const first = await codeOf(worker.session.evaluate("1 + 1"));
  expect(Date.now() - started, "a call in flight when the renderer dies must be interrupted, not left to its deadline").toBeLessThan(10_000);
  // Chrome announces the crash asynchronously, so this one may still land in the teardown window.
  expect(["PAGE_CRASHED", "CANCELLED"]).toContain(first);

  // What matters is where it settles, and that it settles at all.
  await expect.poll(() => codeOf(worker.session.evaluate("1 + 1")), {timeout: 10_000}).toBe("PAGE_CRASHED");
  const failure = await worker.session.evaluate("1 + 1").catch((error: unknown) => error);
  expect(failure).toBeInstanceOf(BilobaError);
  expect((failure as BilobaError).message).toMatch(/navigate the session again to recover/);
});

it("recovers a crashed page on the next navigation", async () => {
  // The advice in that message has to be true, or it is just a nicer-sounding dead end.
  await worker.session.navigate(worker.baseUrl);

  await worker.session.getByRole("heading", {name: "Biloba parity"}).expectVisible();
  expect(await worker.session.evaluate<number>("1 + 1")).toBe(2);
});

it("reaps a worker's daemon when the worker dies without closing, leaving the others alone", async () => {
  // A vitest worker that is killed - or crashes - never runs its teardown.  Its daemon has to notice
  // the pipe close on its own, and taking Chrome or anyone else's session with it is not an option.
  // The helper spawns bilobad the way the client does - piped stdio, its own process group - rather
  // than importing the client, because it runs under plain node with no transpiler.  The mechanism
  // under test is the daemon noticing its stdin close, and this exercises exactly that.
  const directory = await mkdtemp(join(tmpdir(), "biloba-crash-"));
  const script = join(directory, "worker.mjs");
  await writeFile(script, [
    `import {spawn} from "node:child_process";`,
    `const daemon = spawn(${JSON.stringify(inject("daemonExecutable"))}, ["--chrome-ws-url=" + ${JSON.stringify(inject("chromeWsUrl"))}], {stdio: ["pipe", "pipe", "pipe"], detached: true});`,
    `daemon.once("spawn", () => console.log("READY"));`,
    `setInterval(() => {}, 1000);`,
  ].join("\n"));

  const child = spawn(process.execPath, [script], {stdio: ["ignore", "pipe", "pipe"]});
  await new Promise<void>((resolve, reject) => {
    child.stdout.on("data", (chunk: Buffer) => { if (chunk.toString().includes("READY")) resolve(); });
    child.once("error", reject);
    child.once("exit", (code) => reject(new Error(`helper worker exited early (${String(code)})`)));
  });
  const before = daemonCount();
  expect(before, "the helper worker should have started a daemon of its own").toBeGreaterThan(1);

  child.kill("SIGKILL");

  await expect.poll(() => daemonCount(), {timeout: 10_000}).toBe(before - 1);
  expect(chromeIsRunning(), "one worker dying must not take the shared browser with it").toBe(true);
  await worker.session.getByRole("heading", {name: "Biloba parity"}).expectVisible();
});

it("reports why it could not reach Chrome, rather than a bare startup failure", async () => {
  // Nothing is listening on port 1.  The daemon exits during startup and the reason has to survive
  // the trip back, or the user is left guessing at an exit code.
  const failure = await connect({
    daemonExecutable: inject("daemonExecutable"),
    chromeWsUrl: "ws://127.0.0.1:1/devtools/browser/nope",
  }).catch((error: unknown) => error);

  expect(failure).toBeInstanceOf(BilobaError);
  expect((failure as BilobaError).code).toBe("DRIVER_CLOSED");
  expect((failure as BilobaError).daemonDetail, "the daemon's stderr is the only place the real cause exists").toMatch(/connect browser/);
});

// Terminal: everything above needs a live Chrome, so this runs last.
it("tells a worker the browser is gone instead of blaming its assertion", async () => {
  killSharedChrome();

  const started = Date.now();
  const failure = await worker.session.locator("h1").expectVisible({timeoutMs: 10_000, intervalMs: 50})
    .catch((error: unknown) => error);

  expect(failure).toBeInstanceOf(BilobaError);
  expect((failure as BilobaError).code, "a dead browser is not an assertion timeout").toBe("BROWSER_GONE");
  expect((failure as BilobaError).message).toMatch(/browser is no longer available/);
  expect(Date.now() - started, "the poll must stop as soon as it learns the browser is gone").toBeLessThan(5_000);
});

it("reports the same thing for a one-shot operation", async () => {
  const failure = await worker.session.evaluate("1 + 1").catch((error: unknown) => error);

  expect((failure as BilobaError).code).toBe("BROWSER_GONE");
});

/** Resolves to the BilobaError code an operation fails with, or "OK" if it succeeds. */
async function codeOf(operation: Promise<unknown>): Promise<string> {
  try {
    await operation;
    return "OK";
  } catch (error) {
    return (error as BilobaError).code;
  }
}

/**
 * Every process on the machine, as (pgid, pid, command line).
 *
 * `ps -A` rather than `-e`: BSD ps (macOS) reads `-e` as "also show the environment".  `-ww` keeps
 * the command line from being truncated, which the matching below depends on.
 */
function processTable(): {pgid: number; pid: number; args: string}[] {
  return execSync("ps -A -ww -o pgid=,pid=,args=").toString().split("\n").flatMap((line) => {
    const fields = /^\s*(\d+)\s+(\d+)\s+(\S.*?)\s*$/.exec(line);
    if (!fields?.[3]) return [];
    return [{pgid: Number(fields[1]), pid: Number(fields[2]), args: fields[3]}];
  });
}

/**
 * The shared Chrome's processes - and only those.
 *
 * The scoping is the whole point of this helper, because everything below SIGKILLs what it finds.
 * A `pgrep -f chrome-headless-shell` would sweep the entire machine and take out a developer's own
 * browser, a concurrent `make test`, or another checkout's suite.  globalSetup spawns the browser
 * host detached (browser-manager.ts), so the host leads a process group of its own and every
 * process Chrome forks beneath it inherits that group id - which turns the pid vitest handed us
 * into an exact, per-run boundary.  It is the same boundary the harness already uses to stop the
 * browser (`process.kill(-pid)`), and unlike a parent-walk it still holds for a renderer that has
 * been reparented away from its zygote.  The host itself is excluded: these tests kill Chrome, not
 * the daemon that is supposed to notice.
 */
function chromeProcesses(): {pid: number; args: string}[] {
  const host = inject("chromePid");
  return processTable()
    .filter((entry) => entry.pgid === host && entry.pid !== host)
    .map((entry) => ({pid: entry.pid, args: entry.args}));
}

/** Chrome's browser process: the group member that is neither the host nor a `--type=` helper. */
function chromeBrowserProcesses(): {pid: number; args: string}[] {
  return chromeProcesses().filter((entry) => !entry.args.includes("--type="));
}

function chromeIsRunning(): boolean {
  return chromeBrowserProcesses().length > 0;
}

/**
 * How many bilobads are attached to *this run's* Chrome.
 *
 * Daemons cannot be scoped by process group the way Chrome is - the one this test cares about is
 * deliberately orphaned when its worker is killed - so they are matched on a marker instead, and
 * the websocket URL already is one: it carries the port and the per-launch browser guid, so no
 * other run on the machine can share it.  Counting a bare `--chrome-ws-url` would make the
 * assertion below depend on nothing else on the box running a daemon.
 */
function daemonCount(): number {
  const marker = `--chrome-ws-url=${inject("chromeWsUrl")}`;
  return processTable().filter((entry) => entry.args.includes(marker)).length;
}

/** Kills the renderer processes, leaving the browser process alive - Chrome's "Aw, Snap". */
function killRenderers(): void {
  const renderers = chromeProcesses().filter((entry) => entry.args.includes("--type=renderer"));
  expect(renderers.length, "expected a renderer to be running").toBeGreaterThan(0);
  sigkill(renderers);
}

/** SIGKILL, so there is no orderly CDP shutdown - the daemon finds out the way it would if Chrome
 *  had crashed or been OOM-killed, which is the case worth testing. */
function killSharedChrome(): void {
  const chrome = chromeProcesses();
  expect(chromeBrowserProcesses().length, "expected a shared Chrome to be running").toBeGreaterThan(0);
  sigkill(chrome);
}

function sigkill(processes: readonly {pid: number}[]): void {
  for (const {pid} of processes) {
    try { process.kill(pid, "SIGKILL"); } catch { /* already gone */ }
  }
}

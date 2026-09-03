import {spawn, type ChildProcessByStdio} from "node:child_process";
import {platform} from "node:os";
import type {Readable, Writable} from "node:stream";

import {BilobaError} from "../index.js";

export interface StartSharedBrowserOptions {
  executable: string;
  chromePath?: string;
  readyTimeoutMs?: number;
}

export interface SharedBrowserProcess {
  readonly wsURL: string;
  readonly pid: number;
  stop(): Promise<void>;
}

type BrowserChild = ChildProcessByStdio<Writable, Readable, Readable>;

export async function startSharedBrowser(options: StartSharedBrowserOptions): Promise<SharedBrowserProcess> {
  const child = spawn(options.executable, [
    "serve-browser",
    ...(options.chromePath ? [`--chrome-path=${options.chromePath}`] : []),
  ], {stdio: ["pipe", "pipe", "pipe"], detached: platform() !== "win32"});
  const stop = createStop(child);
  const killOnExit = () => killProcessGroup(child, "SIGTERM");
  process.once("exit", killOnExit);
  try {
    const wsURL = await waitForReady(child, options.readyTimeoutMs ?? 60_000);
    if (child.pid === undefined) throw new BilobaError({code: "DRIVER_CLOSED", message: "browser host did not expose a process id"});
    return {
      wsURL,
      pid: child.pid,
      async stop() {
        process.removeListener("exit", killOnExit);
        await stop();
      },
    };
  } catch (error) {
    process.removeListener("exit", killOnExit);
    await stop();
    throw error;
  }
}

function waitForReady(child: BrowserChild, timeoutMs: number): Promise<string> {
  return new Promise((resolve, reject) => {
    let stdout = "";
    let stderr = "";
    const timeout = setTimeout(() => fail(new BilobaError({
      code: "DRIVER_CLOSED",
      message: `shared Chrome did not become ready within ${timeoutMs}ms`,
      ...(stderr && {daemonDetail: stderr}),
    })), timeoutMs);
    const cleanup = () => {
      clearTimeout(timeout);
      child.stdout.removeListener("data", onStdout);
      child.removeListener("error", onError);
      child.removeListener("exit", onExit);
    };
    const fail = (error: Error) => { cleanup(); reject(error); };
    const onStderr = (chunk: Buffer | string) => { stderr = (stderr + chunk.toString()).slice(-64 * 1024); };
    const onStdout = (chunk: Buffer | string) => {
      stdout += chunk.toString();
      const lines = stdout.split(/\r?\n/);
      stdout = lines.pop() ?? "";
      for (const line of lines) {
        try {
          const ready = JSON.parse(line) as {wsURL?: unknown};
          if (typeof ready.wsURL === "string" && ready.wsURL.startsWith("ws://")) {
            cleanup();
            resolve(ready.wsURL);
            return;
          }
        } catch {
          fail(new BilobaError({code: "DRIVER_ERROR", message: "browser host wrote invalid startup JSON"}));
          return;
        }
      }
    };
    const onError = (error: Error) => fail(new BilobaError({code: "DRIVER_CLOSED", message: `Could not start browser host: ${error.message}`}));
    const onExit = (code: number | null, signal: NodeJS.Signals | null) => fail(new BilobaError({
      code: "DRIVER_CLOSED",
      message: `browser host exited before ready (code=${String(code)}, signal=${String(signal)})`,
      ...(stderr && {daemonDetail: stderr}),
    }));
    child.stdout.on("data", onStdout);
    child.stderr.on("data", onStderr);
    child.once("error", onError);
    child.once("exit", onExit);
  });
}

function createStop(child: BrowserChild): () => Promise<void> {
  let stopping: Promise<void> | undefined;
  return () => stopping ??= new Promise<void>((resolve) => {
    if (child.exitCode !== null || child.signalCode !== null) { resolve(); return; }
    const forceTimer = setTimeout(() => {
      if (child.exitCode === null && child.signalCode === null) killProcessGroup(child, "SIGKILL");
    }, 2_000);
    child.once("exit", () => { clearTimeout(forceTimer); resolve(); });
    // Same ordering as the daemon's stop (daemon-manager.ts): end stdin, then signal the group in
    // the same turn, while the leader is still alive and the pgid cannot have been recycled.
    child.stdin.end();
    killProcessGroup(child, "SIGTERM");
  });
}

function killProcessGroup(child: BrowserChild, signal: NodeJS.Signals): void {
  if (child.pid === undefined) return;
  try {
    if (platform() === "win32") child.kill(signal);
    else process.kill(-child.pid, signal);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ESRCH") throw error;
  }
}

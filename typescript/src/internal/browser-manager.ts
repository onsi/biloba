import {spawn, type ChildProcessByStdio} from "node:child_process";
import {platform} from "node:os";
import type {Readable, Writable} from "node:stream";

import {BilobaError} from "../index.js";

export interface StartSharedBrowserOptions {
  executable: string;
  chromePath?: string | undefined;
  mode?: "headless-shell" | "headless" | "headful" | undefined;
  chromeArgs?: readonly string[] | undefined;
  autoInstall?: boolean | undefined;
  windowSize?: {readonly width: number; readonly height: number} | undefined;
  readyTimeoutMs?: number | undefined;
}

export interface SharedBrowserProcess {
  readonly connection: SharedBrowserConnection;
  readonly wsURL: string;
  readonly launch: ResolvedLaunchMetadata & {readonly attached: false};
  readonly pid: number;
  stop(): Promise<void>;
}

export interface ResolvedLaunchMetadata { readonly mode: "headless-shell" | "headless" | "headful"; readonly executablePath: string; readonly chromeArgs: readonly string[]; readonly windowSize: {readonly width: number; readonly height: number}; readonly autoInstalled: boolean }
export interface SharedBrowserConnection { readonly wsURL: string; readonly launch: ResolvedLaunchMetadata }

type BrowserChild = ChildProcessByStdio<Writable, Readable, Readable>;

export async function startSharedBrowser(options: StartSharedBrowserOptions): Promise<SharedBrowserProcess> {
  const child = spawn(options.executable, [
    "serve-browser",
    ...(options.chromePath ? [`--chrome-path=${options.chromePath}`] : []),
    ...(options.mode ? [`--chrome-mode=${options.mode}`] : []),
    ...(options.chromeArgs ?? []).map((argument) => `--chrome-arg=${argument}`),
    ...(options.autoInstall !== undefined ? [`--auto-install=${String(options.autoInstall)}`] : []),
    ...(options.windowSize ? [`--window-width=${options.windowSize.width}`, `--window-height=${options.windowSize.height}`] : []),
  ], {stdio: ["pipe", "pipe", "pipe"], detached: platform() !== "win32"});
  const stop = createStop(child);
  const killOnExit = () => killProcessGroup(child, "SIGTERM");
  process.once("exit", killOnExit);
  try {
    const ready = await waitForReady(child, options.readyTimeoutMs ?? 60_000);
    if (child.pid === undefined || child.pid !== ready.pid) throw new BilobaError({code: "DRIVER_CLOSED", message: "browser host returned an invalid process id"});
    return {
      connection: {wsURL: ready.wsURL, launch: ready.launch},
      wsURL: ready.wsURL,
      launch: {...ready.launch, attached: false},
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

function waitForReady(child: BrowserChild, timeoutMs: number): Promise<{wsURL: string; pid: number; launch: ResolvedLaunchMetadata}> {
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
          const ready = parseReady(JSON.parse(line));
          if (ready) {
            cleanup();
            resolve(ready);
            return;
          }
          fail(new BilobaError({code: "DRIVER_ERROR", message: "browser host wrote invalid startup metadata"}));
          return;
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

function parseReady(value: unknown): {wsURL: string; pid: number; launch: ResolvedLaunchMetadata} | undefined {
  if (!value || typeof value !== "object") return undefined;
  const ready = value as {wsURL?: unknown; pid?: unknown; launch?: unknown};
  if (typeof ready.wsURL !== "string" || !ready.wsURL.startsWith("ws://") || !Number.isInteger(ready.pid) || (ready.pid as number) <= 0 || !ready.launch || typeof ready.launch !== "object") return undefined;
  const launch = ready.launch as Record<string, unknown>;
  const size = launch.windowSize as Record<string, unknown> | undefined;
  if (!(["headless-shell", "headless", "headful"] as unknown[]).includes(launch.mode) || typeof launch.executablePath !== "string" || launch.executablePath.length === 0 || !Array.isArray(launch.chromeArgs) || !launch.chromeArgs.every((arg) => typeof arg === "string" && /^--[A-Za-z0-9][A-Za-z0-9-]*(=.*)?$/.test(arg)) || !size || !Number.isInteger(size.width) || (size.width as number) <= 0 || !Number.isInteger(size.height) || (size.height as number) <= 0 || typeof launch.autoInstalled !== "boolean") return undefined;
  return {wsURL: ready.wsURL, pid: ready.pid as number, launch: {mode: launch.mode as ResolvedLaunchMetadata["mode"], executablePath: launch.executablePath, chromeArgs: [...launch.chromeArgs] as string[], windowSize: {width: size.width as number, height: size.height as number}, autoInstalled: launch.autoInstalled}};
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

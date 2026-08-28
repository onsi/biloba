import {spawn, type ChildProcessByStdio} from "node:child_process";
import {platform} from "node:os";
import type {Readable, Writable} from "node:stream";

import {BilobaError} from "../index.js";
import {StdioTransport} from "./stdio-transport.js";

export interface StartDaemonOptions {
  executable: string;
  chromePath?: string | undefined;
  chromeWsUrl?: string | undefined;
  artifactDir?: string | undefined;
  screenshotBaselinesDir?: string | undefined;
  updateScreenshots?: boolean | undefined;
  screenshotPixelTolerance?: number | undefined;
  screenshotChannelTolerance?: number | undefined;
  maxScreenshotBytes?: number | undefined;
  readyTimeoutMs?: number | undefined;
}

export interface DaemonProcess {
  readonly pid: number;
  stop(): Promise<void>;
}

export interface ManagedDaemon extends DaemonProcess {
  readonly transport: StdioTransport;
}

type DaemonChild = ChildProcessByStdio<Writable, Readable, Readable>;

export async function startDaemon(options: StartDaemonOptions): Promise<ManagedDaemon> {
  const args = [
    ...(options.chromePath ? [`--chrome-path=${options.chromePath}`] : []),
    ...(options.chromeWsUrl ? [`--chrome-ws-url=${options.chromeWsUrl}`] : []),
    ...(options.artifactDir ? [`--artifact-dir=${options.artifactDir}`] : []),
    ...(options.screenshotBaselinesDir ? [`--screenshot-baselines-dir=${options.screenshotBaselinesDir}`] : []),
    ...(options.updateScreenshots !== undefined ? [`--update-screenshots=${String(options.updateScreenshots)}`] : []),
    ...(options.screenshotPixelTolerance !== undefined ? [`--screenshot-pixel-tolerance=${String(options.screenshotPixelTolerance)}`] : []),
    ...(options.screenshotChannelTolerance !== undefined ? [`--screenshot-channel-tolerance=${String(options.screenshotChannelTolerance)}`] : []),
    ...(options.maxScreenshotBytes !== undefined ? [`--max-screenshot-bytes=${String(options.maxScreenshotBytes)}`] : []),
  ];
  const child = spawn(options.executable, args, {
    stdio: ["pipe", "pipe", "pipe"],
    detached: platform() !== "win32",
  });
  const stop = createStop(child);
  const killOnExit = () => killProcessGroup(child, "SIGTERM");
  process.once("exit", killOnExit);
  const stderr = new StderrBuffer();
  child.stderr.on("data", (chunk: Buffer | string) => stderr.append(chunk.toString()));

  try {
    await waitForSpawn(child, options.readyTimeoutMs ?? 10_000, stderr);
    if (child.pid === undefined) {
      throw new BilobaError({code: "DRIVER_CLOSED", message: "bilobad did not expose a process id"});
    }
    const transport = new StdioTransport(child.stdout, child.stdin, {failOnEnd: false});
    child.once("exit", (code, signal) => transport.fail(new Error(
      `bilobad exited (code=${String(code)}, signal=${String(signal)})`,
    ), stderr.value));
    return {
      pid: child.pid,
      transport,
      async stop() {
        process.removeListener("exit", killOnExit);
        transport.close();
        await stop();
      },
    };
  } catch (error) {
    process.removeListener("exit", killOnExit);
    await stop();
    throw error;
  }
}

function waitForSpawn(child: DaemonChild, timeoutMs: number, stderr: StderrBuffer): Promise<void> {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => {
      cleanup();
      reject(new BilobaError({
        code: "DRIVER_CLOSED",
        message: `bilobad did not start within ${timeoutMs}ms`,
        ...(stderr.value && {daemonDetail: stderr.value}),
      }));
    }, timeoutMs);
    const cleanup = () => {
      clearTimeout(timeout);
      child.removeListener("spawn", onSpawn);
      child.removeListener("error", onError);
      child.removeListener("exit", onExit);
    };
    const onSpawn = () => { cleanup(); resolve(); };
    const onError = (error: Error) => {
      cleanup();
      reject(new BilobaError({code: "DRIVER_CLOSED", message: `Could not start bilobad: ${error.message}`}));
    };
    const onExit = (code: number | null, signal: NodeJS.Signals | null) => {
      cleanup();
      reject(new BilobaError({
        code: "DRIVER_CLOSED",
        message: `bilobad exited during startup (code=${String(code)}, signal=${String(signal)})`,
        ...(stderr.value && {daemonDetail: stderr.value}),
      }));
    };
    child.once("spawn", onSpawn);
    child.once("error", onError);
    child.once("exit", onExit);
  });
}

class StderrBuffer {
  #value = "";
  append(value: string): void {
    this.#value = (this.#value + value).slice(-64 * 1024);
  }
  get value(): string { return this.#value; }
}

function createStop(child: DaemonChild): () => Promise<void> {
  let stopping: Promise<void> | undefined;
  return () => {
    stopping ??= new Promise<void>((resolve) => {
      if (child.exitCode !== null || child.signalCode !== null) {
        resolve();
        return;
      }
      const forceTimer = setTimeout(() => {
        // Escalation is only safe while the leader is still ours to signal (see below).
        if (child.exitCode === null && child.signalCode === null) killProcessGroup(child, "SIGKILL");
      }, 2_000);
      child.once("exit", () => {
        clearTimeout(forceTimer);
        resolve();
      });
      // Close stdin so bilobad can shut down over the protocol, then sweep the group in the same
      // turn - while the leader is still alive (or at worst an unreaped zombie, which keeps the
      // group id ours).  Signalling -pid from the exit handler instead would race pgid recycling
      // and could hit an unrelated process group.  bilobad treats SIGTERM as a clean shutdown
      // (cmd/bilobad/main.go's signal.NotifyContext), and any Chrome it spawned shares the group,
      // so the one signal both stops the daemon and reaps its descendants.
      child.stdin.end();
      killProcessGroup(child, "SIGTERM");
    });
    return stopping;
  };
}

function killProcessGroup(child: DaemonChild, signal: NodeJS.Signals): void {
  if (child.pid === undefined) return;
  try {
    if (platform() === "win32") child.kill(signal);
    else process.kill(-child.pid, signal);
  } catch (error) {
    const code = (error as NodeJS.ErrnoException).code;
    if (code !== "ESRCH") throw error;
  }
}

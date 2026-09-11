import {EventEmitter} from "node:events";
import {describe, expect, it, vi} from "vitest";

import {run} from "../src/cli.js";
import {BilobaError} from "../src/index.js";

class FakeWriter {
  chunks: string[] = [];
  write(chunk: string): boolean { this.chunks.push(chunk); return true; }
  get text(): string { return this.chunks.join(""); }
}

class FakeChild extends EventEmitter {
  killed: NodeJS.Signals[] = [];
  kill(signal: NodeJS.Signals): boolean { this.killed.push(signal); return true; }
}

describe("biloba CLI", () => {
  it("prints usage and exits 0 for no arguments", async () => {
    const stdout = new FakeWriter();
    const code = await run([], {stdout});
    expect(code).toBe(0);
    expect(stdout.text).toContain("Usage: biloba");
    expect(stdout.text).toContain("install-chrome");
  });

  it("prints usage and exits 0 for help/--help/-h", async () => {
    for (const argument of ["help", "--help", "-h"]) {
      const stdout = new FakeWriter();
      const code = await run([argument], {stdout});
      expect(code).toBe(0);
      expect(stdout.text).toContain("Usage: biloba");
    }
  });

  it("prints usage to stderr and exits 1 for an unknown command", async () => {
    const stderr = new FakeWriter();
    const code = await run(["frobnicate"], {stderr});
    expect(code).toBe(1);
    expect(stderr.text).toContain("Unknown command: frobnicate");
    expect(stderr.text).toContain("Usage: biloba");
  });

  it("resolves bilobad and runs install-chrome with inherited stdio, propagating the exit code", async () => {
    const child = new FakeChild();
    const spawn = vi.fn().mockReturnValue(child);
    const resolveDaemonExecutable = vi.fn().mockResolvedValue("/resolved/bilobad");

    const promise = run(["install-chrome"], {spawn: spawn as never, resolveDaemonExecutable});
    await Promise.resolve();
    expect(spawn).toHaveBeenCalledWith("/resolved/bilobad", ["install-chrome"], {stdio: "inherit"});

    child.emit("exit", 3, null);
    await expect(promise).resolves.toBe(3);
  });

  it("forwards extra arguments after install-chrome", async () => {
    const child = new FakeChild();
    const spawn = vi.fn().mockReturnValue(child);
    const resolveDaemonExecutable = vi.fn().mockResolvedValue("/resolved/bilobad");

    const promise = run(["install-chrome", "--force"], {spawn: spawn as never, resolveDaemonExecutable});
    await Promise.resolve();
    expect(spawn).toHaveBeenCalledWith("/resolved/bilobad", ["install-chrome", "--force"], {stdio: "inherit"});
    child.emit("exit", 0, null);
    await promise;
  });

  it("maps a signal-terminated child to a 128+signum exit code", async () => {
    const child = new FakeChild();
    const spawn = vi.fn().mockReturnValue(child);
    const resolveDaemonExecutable = vi.fn().mockResolvedValue("/resolved/bilobad");

    const promise = run(["install-chrome"], {spawn: spawn as never, resolveDaemonExecutable});
    await Promise.resolve();
    child.emit("exit", null, "SIGTERM");
    await expect(promise).resolves.toBe(128 + 15);
  });

  it("forwards SIGINT/SIGTERM received by the CLI to the child process", async () => {
    const child = new FakeChild();
    const spawn = vi.fn().mockReturnValue(child);
    const resolveDaemonExecutable = vi.fn().mockResolvedValue("/resolved/bilobad");
    const signalSource = new EventEmitter() as unknown as NodeJS.Process;

    const promise = run(["install-chrome"], {spawn: spawn as never, resolveDaemonExecutable, signalSource});
    await Promise.resolve();
    (signalSource as unknown as EventEmitter).emit("SIGINT");
    expect(child.killed).toEqual(["SIGINT"]);

    child.emit("exit", 0, null);
    await promise;
  });

  it("prints the resolver's error message (no stack) and exits 1", async () => {
    const stderr = new FakeWriter();
    const resolveDaemonExecutable = vi.fn().mockRejectedValue(new BilobaError({code: "DRIVER_ERROR", message: "could not find biloba-linux-arm64"}));

    const code = await run(["install-chrome"], {stderr, resolveDaemonExecutable});

    expect(code).toBe(1);
    expect(stderr.text.trim()).toBe("could not find biloba-linux-arm64");
  });
});

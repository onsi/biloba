import {mkdtemp, readFile, rm, writeFile, chmod} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {afterEach, describe, expect, it} from "vitest";

import {connect, startDaemon, startSharedBrowser, type DaemonProcess, type SharedBrowserProcess} from "../src/index.js";

describe("bilobad process manager", () => {
  let daemon: DaemonProcess | undefined;
  let browser: SharedBrowserProcess | undefined;
  let directory: string | undefined;

  afterEach(async () => {
    await daemon?.stop();
    await browser?.stop();
    if (directory) await rm(directory, {recursive: true, force: true});
  });

  it("spawns a supplied executable with protocol-only stdio", async () => {
    directory = await mkdtemp(join(tmpdir(), "biloba-daemon-test-"));
    const executable = join(directory, "fake-bilobad");
    const argumentsPath = join(directory, "arguments.json");
    await writeFile(executable, `#!/usr/bin/env node
const fs = require("node:fs");
fs.writeFileSync(${JSON.stringify(argumentsPath)}, JSON.stringify(process.argv.slice(2)));
process.on("SIGTERM", () => process.exit(0));
process.stdin.resume();
process.stdin.on("end", () => process.exit(0));
setInterval(() => {}, 1000);
`);
    await chmod(executable, 0o755);

    daemon = await startDaemon({
      executable,
      chromePath: "/opt/chrome",
      artifactDir: "/tmp/biloba-artifacts",
    });

    await expect.poll(async () => await readFile(argumentsPath, "utf8").catch(() => "")).not.toBe("");
    expect(JSON.parse(await readFile(argumentsPath, "utf8"))).toEqual([
      "--chrome-path=/opt/chrome",
      "--artifact-dir=/tmp/biloba-artifacts",
    ]);
    expect(daemon.pid).toBeGreaterThan(0);

    const pid = daemon.pid;
    await daemon.stop();
    expect(() => process.kill(pid, 0)).toThrow();
  });

  it("reports retained stderr when the daemon exits before handshake", async () => {
    directory = await mkdtemp(join(tmpdir(), "biloba-daemon-test-"));
    const executable = join(directory, "broken-bilobad");
    await writeFile(executable, `#!/usr/bin/env node
process.stderr.write("chrome executable is missing\\n");
process.exit(7);
`);
    await chmod(executable, 0o755);

    await expect(connect({daemonExecutable: executable})).rejects.toMatchObject({
      code: "DRIVER_CLOSED",
      daemonDetail: "chrome executable is missing\n",
    });
  });

  it("starts a shared-browser host and reads its websocket endpoint", async () => {
    directory = await mkdtemp(join(tmpdir(), "biloba-browser-test-"));
    const executable = join(directory, "fake-bilobad");
    await writeFile(executable, `#!/usr/bin/env node
if (process.argv[2] !== "serve-browser") process.exit(2);
console.log(JSON.stringify({wsURL: "ws://127.0.0.1:43123/devtools/browser/test", width: 1920, height: 1080}));
process.stdin.resume();
process.stdin.on("end", () => process.exit(0));
setInterval(() => {}, 1000);
`);
    await chmod(executable, 0o755);

    browser = await startSharedBrowser({executable, chromePath: "/opt/chrome"});
    expect(browser.wsURL).toBe("ws://127.0.0.1:43123/devtools/browser/test");
    expect(browser.pid).toBeGreaterThan(0);
  });

  it.runIf(process.platform !== "win32")("signals the process group while the daemon is still alive", async () => {
    directory = await mkdtemp(join(tmpdir(), "biloba-daemon-test-"));
    const executable = join(directory, "slow-bilobad");
    const signalPath = join(directory, "sigterm.txt");
    const readyPath = join(directory, "ready.txt");
    // Exits a beat after stdin closes, so a group signal sent only from the exit handler - once the
    // pgid may already have been recycled - would never reach this process at all.
    await writeFile(executable, `#!/usr/bin/env node
const fs = require("node:fs");
process.on("SIGTERM", () => { fs.writeFileSync(${JSON.stringify(signalPath)}, "sigterm"); process.exit(0); });
fs.writeFileSync(${JSON.stringify(readyPath)}, "ready");
process.stdin.resume();
process.stdin.on("end", () => setTimeout(() => process.exit(0), 100));
setInterval(() => {}, 1000);
`);
    await chmod(executable, 0o755);
    daemon = await startDaemon({executable});
    await expect.poll(async () => await readFile(readyPath, "utf8").catch(() => "")).toBe("ready");

    await daemon.stop();

    expect(await readFile(signalPath, "utf8").catch(() => "")).toBe("sigterm");
  });

  it.runIf(process.platform !== "win32")("reaps descendant processes when the daemon stops", async () => {
    directory = await mkdtemp(join(tmpdir(), "biloba-daemon-test-"));
    const executable = join(directory, "daemon-with-child");
    const descendantPath = join(directory, "descendant.pid");
    await writeFile(executable, `#!/usr/bin/env node
const fs = require("node:fs");
const {spawn} = require("node:child_process");
const child = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], {stdio: "ignore"});
fs.writeFileSync(${JSON.stringify(descendantPath)}, String(child.pid));
process.stdin.resume();
process.stdin.on("end", () => process.exit(0));
setInterval(() => {}, 1000);
`);
    await chmod(executable, 0o755);
    daemon = await startDaemon({executable});
    await expect.poll(async () => await readFile(descendantPath, "utf8").catch(() => "")).not.toBe("");
    const descendantPid = Number(await readFile(descendantPath, "utf8"));

    await daemon.stop();

    await expect.poll(() => processExists(descendantPid), {timeout: 2_000}).toBe(false);
  });
});

function processExists(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    return (error as NodeJS.ErrnoException).code !== "ESRCH";
  }
}

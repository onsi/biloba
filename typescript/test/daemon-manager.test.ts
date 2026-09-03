import {mkdtemp, readFile, rm, writeFile, chmod} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join, resolve} from "node:path";
import {afterEach, describe, expect, it} from "vitest";

import {connect, startDaemon, startSharedBrowser, type DaemonProcess, type SharedBrowserProcess} from "../src/index.js";
import {resolveUpdateScreenshots, resolveVisualConnectOptions} from "../src/internal/client.js";

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
      screenshotBaselinesDir: "/tmp/biloba-baselines",
      updateScreenshots: true,
      screenshotPixelTolerance: 0.02,
      screenshotChannelTolerance: 8,
      maxScreenshotBytes: 1024,
    });

    await expect.poll(async () => await readFile(argumentsPath, "utf8").catch(() => "")).not.toBe("");
    expect(JSON.parse(await readFile(argumentsPath, "utf8"))).toEqual([
      "--chrome-path=/opt/chrome",
      "--artifact-dir=/tmp/biloba-artifacts",
      "--screenshot-baselines-dir=/tmp/biloba-baselines",
      "--update-screenshots=true",
      "--screenshot-pixel-tolerance=0.02",
      "--screenshot-channel-tolerance=8",
      "--max-screenshot-bytes=1024",
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

  it("parses every screenshot-update environment spelling and warns on invalid values", () => {
    const warnings: string[] = [];
    for (const value of ["1", "t", "true", "y", "yes", "on", " TRUE "]) {
      expect(resolveUpdateScreenshots(undefined, value, (message) => warnings.push(message))).toBe(true);
    }
    for (const value of ["0", "f", "false", "n", "no", "off", "", " ", undefined]) {
      expect(resolveUpdateScreenshots(undefined, value, (message) => warnings.push(message))).toBe(false);
    }
    expect(resolveUpdateScreenshots(undefined, " definitely ", (message) => warnings.push(message))).toBe(false);
    expect(warnings).toEqual([expect.stringContaining("unrecognized value")]);
    expect(resolveUpdateScreenshots(true, " definitely ", (message) => warnings.push(message))).toBe(true);
    expect(resolveUpdateScreenshots(false, "true", (message) => warnings.push(message))).toBe(false);
    expect(warnings).toHaveLength(1);
  });

  it("resolves explicit visual roots over environment roots over absolute defaults", () => {
    const defaults = resolveVisualConnectOptions({}, {}, () => undefined);
    expect(defaults).toMatchObject({
      artifactDir: resolve("biloba-screenshots"),
      screenshotBaselinesDir: resolve("biloba-baselines"),
      updateScreenshots: false,
      screenshotPixelTolerance: 0,
      screenshotChannelTolerance: 0,
      maxScreenshotBytes: 16 * 1024 * 1024,
    });
    const environment = resolveVisualConnectOptions({}, {
      BILOBA_SCREENSHOTS_DIR: "env-artifacts",
      BILOBA_SCREENSHOT_BASELINES_DIR: "env-baselines",
      BILOBA_UPDATE_SCREENSHOTS: "yes",
    }, () => undefined);
    expect(environment).toMatchObject({artifactDir: resolve("env-artifacts"), screenshotBaselinesDir: resolve("env-baselines"), updateScreenshots: true});
    const explicit = resolveVisualConnectOptions({
      artifactDir: "explicit-artifacts",
      screenshotBaselinesDir: "explicit-baselines",
      updateScreenshots: false,
      screenshotPixelTolerance: 0.02,
      screenshotChannelTolerance: 8,
      maxScreenshotBytes: 1024,
    }, {
      BILOBA_SCREENSHOTS_DIR: "env-artifacts",
      BILOBA_SCREENSHOT_BASELINES_DIR: "env-baselines",
      BILOBA_UPDATE_SCREENSHOTS: "yes",
    }, () => undefined);
    expect(explicit).toMatchObject({
      artifactDir: resolve("explicit-artifacts"),
      screenshotBaselinesDir: resolve("explicit-baselines"),
      updateScreenshots: false,
      screenshotPixelTolerance: 0.02,
      screenshotChannelTolerance: 8,
      maxScreenshotBytes: 1024,
    });
    for (const options of [
      {maxScreenshotBytes: 0}, {maxScreenshotBytes: 16 * 1024 * 1024 + 1},
      {screenshotPixelTolerance: Number.NaN}, {screenshotPixelTolerance: Number.POSITIVE_INFINITY}, {screenshotPixelTolerance: -0.01}, {screenshotPixelTolerance: 1.01},
      {screenshotChannelTolerance: -1}, {screenshotChannelTolerance: 256}, {screenshotChannelTolerance: 1.5},
    ]) expect(() => resolveVisualConnectOptions(options, {}, () => undefined)).toThrow();
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

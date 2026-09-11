import {chmod, mkdir, mkdtemp, readFile, realpath, rm, writeFile} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {afterEach, describe, expect, it} from "vitest";

import {resolveDaemonExecutable} from "../src/internal/daemon-resolver.js";

const ownVersion = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8")).version as string;

describe("resolveDaemonExecutable", () => {
  let directory: string | undefined;

  afterEach(async () => {
    if (directory) await rm(directory, {recursive: true, force: true});
    directory = undefined;
  });

  it("prefers an explicit executable over everything else", async () => {
    await expect(resolveDaemonExecutable({
      explicit: "/explicit/bilobad",
      env: {BILOBA_DAEMON_EXECUTABLE: "/env/bilobad"},
      platform: "darwin",
      arch: "arm64",
    })).resolves.toBe("/explicit/bilobad");
  });

  it("falls back to BILOBA_DAEMON_EXECUTABLE when there is no explicit option", async () => {
    await expect(resolveDaemonExecutable({
      env: {BILOBA_DAEMON_EXECUTABLE: "/env/bilobad"},
      platform: "linux",
      arch: "x64",
    })).resolves.toBe("/env/bilobad");
  });

  it("resolves the platform package's bin/bilobad when neither override is set", async () => {
    directory = await realpath(await mkdtemp(join(tmpdir(), "biloba-resolver-test-")));
    const executable = await stagePlatformPackage(directory, "biloba-darwin-arm64", {mode: 0o755});

    const resolved = await resolveDaemonExecutable({
      env: {},
      platform: "darwin",
      arch: "arm64",
      resolveFrom: join(directory, "project.js"),
    });

    expect(resolved).toBe(executable);
  });

  it("maps every supported platform/arch pair to its own package", async () => {
    const cases: Array<[NodeJS.Platform, string, string]> = [
      ["darwin", "arm64", "biloba-darwin-arm64"],
      ["darwin", "x64", "biloba-darwin-x64"],
      ["linux", "arm64", "biloba-linux-arm64"],
      ["linux", "x64", "biloba-linux-x64"],
    ];
    for (const [platform, arch, packageName] of cases) {
      const caseDirectory = await realpath(await mkdtemp(join(tmpdir(), "biloba-resolver-test-")));
      try {
        const executable = await stagePlatformPackage(caseDirectory, packageName, {mode: 0o755});
        const resolved = await resolveDaemonExecutable({
          env: {},
          platform,
          arch,
          resolveFrom: join(caseDirectory, "project.js"),
        });
        expect(resolved).toBe(executable);
      } finally {
        await rm(caseDirectory, {recursive: true, force: true});
      }
    }
  });

  it("chmods a bin/bilobad that was packaged without the executable bit", async () => {
    directory = await realpath(await mkdtemp(join(tmpdir(), "biloba-resolver-test-")));
    const executable = await stagePlatformPackage(directory, "biloba-linux-x64", {mode: 0o644});

    const resolved = await resolveDaemonExecutable({
      env: {},
      platform: "linux",
      arch: "x64",
      resolveFrom: join(directory, "project.js"),
    });

    expect(resolved).toBe(executable);
    const stats = await import("node:fs/promises").then((fs) => fs.stat(executable));
    expect(stats.mode & 0o111).not.toBe(0);
  });

  it("explains a missing platform package with likely causes and both overrides", async () => {
    directory = await realpath(await mkdtemp(join(tmpdir(), "biloba-resolver-test-")));
    await mkdir(join(directory, "node_modules"), {recursive: true});

    await expect(resolveDaemonExecutable({
      env: {},
      platform: "linux",
      arch: "arm64",
      resolveFrom: join(directory, "project.js"),
    })).rejects.toMatchObject({
      code: "DRIVER_ERROR",
      message: expect.stringMatching(/biloba-linux-arm64/),
    });

    try {
      await resolveDaemonExecutable({env: {}, platform: "linux", arch: "arm64", resolveFrom: join(directory, "project.js")});
      expect.unreachable();
    } catch (error) {
      const message = (error as Error).message;
      expect(message).toContain("biloba-linux-arm64");
      expect(message).toContain("--omit=optional");
      expect(message).toContain("different OS/architecture");
      expect(message).toContain("daemonExecutable");
      expect(message).toContain("BILOBA_DAEMON_EXECUTABLE");
      expect(message).toContain(`go install github.com/onsi/biloba/cmd/bilobad@v${ownVersion}`);
    }
  });

  it("reports a missing bin/bilobad inside an otherwise-installed platform package", async () => {
    directory = await realpath(await mkdtemp(join(tmpdir(), "biloba-resolver-test-")));
    const packageDirectory = join(directory, "node_modules", "biloba-darwin-x64");
    await mkdir(packageDirectory, {recursive: true});
    await writeFile(join(packageDirectory, "package.json"), JSON.stringify({name: "biloba-darwin-x64", version: "0.0.0"}));

    await expect(resolveDaemonExecutable({
      env: {},
      platform: "darwin",
      arch: "x64",
      resolveFrom: join(directory, "project.js"),
    })).rejects.toMatchObject({
      code: "DRIVER_ERROR",
      message: expect.stringMatching(/missing its bilobad binary/),
    });
  });

  it("errors clearly on an unsupported platform, naming go install and both overrides", async () => {
    await expect(resolveDaemonExecutable({
      env: {},
      platform: "win32",
      arch: "x64",
    })).rejects.toMatchObject({
      code: "DRIVER_ERROR",
      message: expect.stringContaining("win32-x64"),
    });

    try {
      await resolveDaemonExecutable({env: {}, platform: "win32", arch: "x64"});
      expect.unreachable();
    } catch (error) {
      const message = (error as Error).message;
      expect(message).toContain("win32-x64");
      expect(message).toContain("daemonExecutable");
      expect(message).toContain("BILOBA_DAEMON_EXECUTABLE");
      expect(message).toContain(`go install github.com/onsi/biloba/cmd/bilobad@v${ownVersion}`);
    }
  });

  it("errors on an unsupported architecture on an otherwise-supported platform", async () => {
    await expect(resolveDaemonExecutable({
      env: {},
      platform: "linux",
      arch: "ia32",
    })).rejects.toMatchObject({code: "DRIVER_ERROR", message: expect.stringContaining("linux-ia32")});
  });

  it("still honors an explicit override on an unsupported platform", async () => {
    await expect(resolveDaemonExecutable({
      explicit: "/explicit/bilobad",
      env: {},
      platform: "win32",
      arch: "x64",
    })).resolves.toBe("/explicit/bilobad");
  });

  it("still honors BILOBA_DAEMON_EXECUTABLE on an unsupported platform", async () => {
    await expect(resolveDaemonExecutable({
      env: {BILOBA_DAEMON_EXECUTABLE: "/env/bilobad"},
      platform: "win32",
      arch: "x64",
    })).resolves.toBe("/env/bilobad");
  });
});

async function stagePlatformPackage(root: string, packageName: string, options: {mode: number}): Promise<string> {
  const packageDirectory = join(root, "node_modules", packageName);
  const binDirectory = join(packageDirectory, "bin");
  await mkdir(binDirectory, {recursive: true});
  await writeFile(join(packageDirectory, "package.json"), JSON.stringify({name: packageName, version: "0.0.0", os: [], cpu: []}));
  const executable = join(binDirectory, "bilobad");
  await writeFile(executable, "#!/usr/bin/env node\n");
  await chmod(executable, options.mode);
  return executable;
}

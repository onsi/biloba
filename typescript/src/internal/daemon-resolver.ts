import {constants as fsConstants} from "node:fs";
import {access, chmod} from "node:fs/promises";
import {createRequire} from "node:module";
import {dirname, join} from "node:path";

import {BilobaError} from "../index.js";
import {packageVersion} from "./package-version.js";

/** The npm package that ships the bilobad binary for a given `process.platform`/`process.arch`
 *  pair. Windows and every architecture other than x64/arm64 are unsupported: `go install` (see
 *  `resolveDaemonExecutable`) is the escape hatch there. */
const PLATFORM_PACKAGES: Readonly<Record<string, Readonly<Record<string, string>>>> = {
  darwin: {x64: "biloba-darwin-x64", arm64: "biloba-darwin-arm64"},
  linux: {x64: "biloba-linux-x64", arm64: "biloba-linux-arm64"},
};

export interface ResolveDaemonExecutableOptions {
  /** An explicit executable path, e.g. `ConnectOptions.daemonExecutable` or
   *  `StartSharedBrowserOptions.executable`. Wins over everything else when set. */
  explicit?: string | undefined;
  /** Defaults to `process.env`. Overridable for tests. */
  env?: NodeJS.ProcessEnv;
  /** Defaults to `process.platform`. Overridable for tests. */
  platform?: NodeJS.Platform;
  /** Defaults to `process.arch`. Overridable for tests. */
  arch?: string;
  /** Base path or URL that `node_modules` package resolution is rooted at, in the same sense as
   *  `createRequire`'s argument. Defaults to this module's own location. Overridable for tests
   *  that stand up a fake `node_modules` layout under a temp directory. */
  resolveFrom?: string | URL;
}

/** Finds the bilobad executable to run, in precedence order:
 *
 *  1. `options.explicit` (the caller's own option, e.g. `daemonExecutable`/`executable`).
 *  2. `BILOBA_DAEMON_EXECUTABLE`.
 *  3. The per-platform npm package (`biloba-darwin-arm64`, etc.) sitting alongside `biloba` in
 *     `node_modules`, resolved via `require.resolve`.
 *
 *  Throws a `BilobaError` (code `DRIVER_ERROR`) when none of those produce a usable executable -
 *  either because the platform isn't one Biloba ships a daemon for yet, or because the platform
 *  package is missing or broken. */
export async function resolveDaemonExecutable(options: ResolveDaemonExecutableOptions = {}): Promise<string> {
  if (options.explicit) return options.explicit;

  const env = options.env ?? process.env;
  const fromEnv = env.BILOBA_DAEMON_EXECUTABLE;
  if (fromEnv) return fromEnv;

  const platform = options.platform ?? process.platform;
  const arch = options.arch ?? process.arch;
  const packageName = PLATFORM_PACKAGES[platform]?.[arch];

  if (!packageName) {
    throw new BilobaError({
      code: "DRIVER_ERROR",
      message: `Biloba does not ship a bilobad daemon for ${platform}-${arch} yet (only darwin/linux on x64 or arm64; Windows is not supported yet). ` +
        `Install Go and run \`${goInstallHint()}\` to build one, then point at it with the daemonExecutable option or the BILOBA_DAEMON_EXECUTABLE environment variable.`,
    });
  }

  const require = createRequire(options.resolveFrom ?? import.meta.url);
  let manifestPath: string;
  try {
    manifestPath = require.resolve(`${packageName}/package.json`);
  } catch {
    throw new BilobaError({
      code: "DRIVER_ERROR",
      message: `Could not find the ${packageName} package, which provides the bilobad daemon for ${platform}-${arch}. Likely causes: ` +
        `it was excluded from the install (for example \`npm install --omit=optional\`, \`npm install --no-optional\`, or pnpm/bun pruning optional dependencies), ` +
        `or node_modules was installed on a different OS/architecture than this one and reused here (for example a lockfile generated on macOS and installed inside a Linux container). ` +
        `Reinstall with optional dependencies included, point at an existing binary with the daemonExecutable option or BILOBA_DAEMON_EXECUTABLE, or install Go and run \`${goInstallHint()}\`.`,
    });
  }

  const executablePath = join(dirname(manifestPath), "bin", "bilobad");
  await ensureExecutable(executablePath, packageName);
  return executablePath;
}

/** `access`+`chmod` fallback for a packaging slip that ships `bin/bilobad` without the executable
 *  bit set (for example a tarball built on a filesystem that doesn't preserve permissions). */
async function ensureExecutable(executablePath: string, packageName: string): Promise<void> {
  try {
    await access(executablePath, fsConstants.X_OK);
    return;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") {
      throw new BilobaError({
        code: "DRIVER_ERROR",
        message: `The ${packageName} package is installed but is missing its bilobad binary at ${executablePath}. This looks like a broken install - try reinstalling ${packageName}, or install Go and run \`${goInstallHint()}\`.`,
      });
    }
  }

  try {
    await chmod(executablePath, 0o755);
    await access(executablePath, fsConstants.X_OK);
  } catch (error) {
    throw new BilobaError({
      code: "DRIVER_ERROR",
      message: `${executablePath} (from ${packageName}) exists but is not executable, and Biloba could not make it executable: ${(error as Error).message}. Try reinstalling ${packageName}, or install Go and run \`${goInstallHint()}\`.`,
    });
  }
}

/** Builds the `go install …@vX.Y.Z` hint from this package's own version (falling back to
 *  `@latest` when the version can't be read), for the "no daemon available" error messages. Called
 *  only when one of those errors is about to be thrown, not on the success path. */
function goInstallHint(): string {
  const version = packageVersion();
  return `go install github.com/onsi/biloba/cmd/bilobad@${version ? `v${version}` : "latest"}`;
}

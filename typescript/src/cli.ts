#!/usr/bin/env node
import {spawn as nodeSpawn, type ChildProcess} from "node:child_process";
import {realpathSync} from "node:fs";
import {constants as osConstants} from "node:os";

import {resolveDaemonExecutable} from "./internal/daemon-resolver.js";

const USAGE = `Usage: biloba <command>

Commands:
  install-chrome   Download and cache the Chrome build bilobad needs
  help             Show this message
`;

interface Writer {
  write(chunk: string): unknown;
}

export interface RunOptions {
  resolveDaemonExecutable?: typeof resolveDaemonExecutable;
  spawn?: typeof nodeSpawn;
  stdout?: Writer;
  stderr?: Writer;
  /** Process to forward SIGINT/SIGTERM to the child from. Defaults to the real `process`. */
  signalSource?: NodeJS.Process;
}

const FORWARDED_SIGNALS: readonly NodeJS.Signals[] = ["SIGINT", "SIGTERM"];

/** Entry point for the `biloba` CLI (`dist/cli.js`, wired up via `package.json`'s `bin` field).
 *  Deliberately tiny: a single `install-chrome` subcommand that resolves bilobad the same way the
 *  library does (so `BILOBA_DAEMON_EXECUTABLE` works) and runs `bilobad install-chrome` with
 *  inherited stdio. Returns the process exit code rather than calling `process.exit` itself, so
 *  it's easy to unit test. */
export async function run(argv: readonly string[], options: RunOptions = {}): Promise<number> {
  const resolve = options.resolveDaemonExecutable ?? resolveDaemonExecutable;
  const spawn = options.spawn ?? nodeSpawn;
  const stdout = options.stdout ?? process.stdout;
  const stderr = options.stderr ?? process.stderr;
  const signalSource = options.signalSource ?? process;

  const [command, ...rest] = argv;

  if (command === undefined || command === "help" || command === "--help" || command === "-h") {
    stdout.write(USAGE);
    return 0;
  }

  if (command === "install-chrome") {
    let executable: string;
    try {
      executable = await resolve({});
    } catch (error) {
      stderr.write(`${(error as Error).message}\n`);
      return 1;
    }
    return await runInherited(spawn, executable, ["install-chrome", ...rest], signalSource);
  }

  stderr.write(`Unknown command: ${command}\n\n${USAGE}`);
  return 1;
}

function runInherited(spawn: typeof nodeSpawn, executable: string, args: string[], signalSource: NodeJS.Process): Promise<number> {
  return new Promise((resolve) => {
    let child: ChildProcess;
    try {
      child = spawn(executable, args, {stdio: "inherit"});
    } catch (error) {
      process.stderr.write(`${(error as Error).message}\n`);
      resolve(1);
      return;
    }

    // Node invokes signal listeners with no arguments, so each forwarded signal needs its own
    // closure to know which one to re-send to the child.
    const forwarders = FORWARDED_SIGNALS.map((signal): [NodeJS.Signals, () => void] => [signal, () => { child.kill(signal); }]);
    for (const [signal, handler] of forwarders) signalSource.on(signal, handler);
    const stopForwarding = () => { for (const [signal, handler] of forwarders) signalSource.removeListener(signal, handler); };

    child.once("error", (error) => {
      stopForwarding();
      process.stderr.write(`${error.message}\n`);
      resolve(1);
    });
    child.once("exit", (code, signal) => {
      stopForwarding();
      if (code !== null) { resolve(code); return; }
      // The child died from a signal rather than exiting normally (including one we just
      // forwarded above) - reflect that in our own exit code using the common 128+signum
      // convention, rather than re-raising the signal against ourselves and risking taking the
      // parent process down with it.
      const signalNumber = signal ? osConstants.signals[signal] : undefined;
      resolve(signalNumber ? 128 + signalNumber : 1);
    });
  });
}

// npm's bin shim is a symlink (node_modules/.bin/biloba -> ../biloba/dist/cli.js), and Node
// resolves symlinks when it turns argv[1] into import.meta.url, so this has to compare real
// paths rather than the raw argv[1] string.
const isMain = (() => {
  const entry = process.argv[1];
  if (!entry) return false;
  try {
    return import.meta.url === new URL(`file://${realpathSync(entry)}`).href;
  } catch {
    return false;
  }
})();

if (isMain) {
  run(process.argv.slice(2)).then((code) => process.exit(code));
}

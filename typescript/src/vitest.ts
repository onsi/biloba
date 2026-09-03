import {afterEach, beforeEach} from "vitest";

import type {ContextDiagnostics, Session} from "./index.js";
import {formatDiagnostics} from "./internal/diagnostics-formatter.js";
import {VitestAdapter} from "./internal/vitest-adapter.js";

export interface BilobaVitestOptions {
  sessions: () => Session | readonly Session[] | undefined;
  progressAfterMs?: number | undefined;
  failOnConsoleAssert?: boolean | undefined;
  replayConsoleErrors?: boolean | undefined;
  output?: ((text: string) => void) | undefined;
}

export interface BilobaVitestHooks {
  captureProgress(name?: string): Promise<readonly ContextDiagnostics[]>;
  dispose(): Promise<void>;
}

export function installBilobaVitestHooks(options: BilobaVitestOptions): BilobaVitestHooks {
  const output = options.output ?? ((text: string) => process.stderr.write(text));
  const adapter = new VitestAdapter({...options, output, format: (values) => formatDiagnostics(values)});
  beforeEach((context) => adapter.start(context.task.name));
  afterEach(async (context) => await adapter.finish(context.task.name, context.task.result?.state === "fail"));

  return {
    async captureProgress(name?: string) { return await adapter.capture("progress", name); },
    async dispose() { await adapter.dispose(); },
  };
}

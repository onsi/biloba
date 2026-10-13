import type {ContextDiagnostics, PageError, Session} from "../index.js";

export interface VitestAdapterOptions {
  sessions: () => Session | readonly Session[] | undefined;
  progressAfterMs?: number | undefined;
  failOnConsoleAssert?: boolean | undefined;
  replayConsoleErrors?: boolean | undefined;
  output: (text: string) => void;
  format: (values: readonly ContextDiagnostics[]) => string;
}

export class VitestAdapter {
  readonly #options: VitestAdapterOptions;
  #timer: NodeJS.Timeout | undefined;
  #progressCapture: Promise<void> | undefined;
  #disposed = false;

  constructor(options: VitestAdapterOptions) { this.#options = options; }

  start(name: string): void {
    if (this.#disposed || this.#options.progressAfterMs === undefined) return;
    this.#clearTimer();
    this.#timer = setTimeout(() => {
      this.#timer = undefined;
      this.#progressCapture = this.capture("progress", name)
        .then((values) => this.#options.output(this.#options.format(values)))
        .catch((error: unknown) => this.#options.output(`Biloba progress diagnostics failed: ${errorMessage(error)}\n`));
    }, this.#options.progressAfterMs);
  }

  async finish(name: string, failed: boolean): Promise<void> {
    this.#clearTimer();
    await this.#awaitProgress();
    if (this.#disposed) return;
    const current = this.#sessions();
    const consoleMessages = (await Promise.all(current.map(async (session) => await settled(() => session.consoleMessages())))).flat();
    const pageErrors = (await Promise.all(current.map(async (session) => await settled(() => session.pageErrors())))).flat();
    // Chrome's own messages (a non-empty source) stay in consoleMessages() but are not replayed: the
    // replay is for what the page itself reported, as in the Go client's failure summary.
    const consoleErrors = consoleMessages.filter((message) => !message.source && (message.type === "error" || message.type === "assert"));
    if (this.#options.replayConsoleErrors ?? true) {
      const replay = [
        ...consoleErrors.map((message) => ({timestamp: message.timestamp, text: `[browser ${message.type}] ${message.text}\n`})),
        ...pageErrors.map((pageError) => ({timestamp: pageError.timestamp, text: `[browser pageerror] ${describePageError(pageError)}\n`})),
      ];
      replay.sort((a, b) => replayTime(a.timestamp) - replayTime(b.timestamp));
      for (const entry of replay) this.#options.output(entry.text);
    }
    const asserted = (this.#options.failOnConsoleAssert ?? true) && consoleErrors.some((message) => message.type === "assert");
    if (failed || asserted) {
      try { this.#options.output(this.#options.format(await this.capture("failure", name))); }
      catch (error) { this.#options.output(`Biloba failure diagnostics failed: ${errorMessage(error)}\n`); }
    }
    if (asserted && !failed) {
      const assertions = consoleErrors.filter((message) => message.type === "assert").map((message) => message.text).join("; ");
      throw new Error(`browser console.assert failed: ${assertions}`);
    }
  }

  async capture(purpose: "failure" | "progress", name?: string): Promise<readonly ContextDiagnostics[]> {
    const results: ContextDiagnostics[] = [];
    for (const session of this.#sessions()) results.push(await session.captureDiagnostics({purpose, ...(name && {name})}));
    return results;
  }

  async dispose(): Promise<void> {
    this.#disposed = true;
    this.#clearTimer();
    await this.#awaitProgress();
  }

  #sessions(): Session[] {
    const value = this.#options.sessions();
    const values = value === undefined ? [] : Array.isArray(value) ? value : [value];
    const unique = new Map<string, Session>();
    for (const session of values) unique.set(session.contextId || session.id, session);
    return [...unique.values()];
  }

  #clearTimer(): void {
    if (this.#timer) clearTimeout(this.#timer);
    this.#timer = undefined;
  }

  async #awaitProgress(): Promise<void> {
    if (!this.#progressCapture) return;
    await this.#progressCapture;
    this.#progressCapture = undefined;
  }
}

async function settled<T>(read: () => Promise<readonly T[]>): Promise<readonly T[]> {
  try { return await read(); } catch { return []; }
}

function replayTime(timestamp: string): number {
  const parsed = Date.parse(timestamp);
  return Number.isNaN(parsed) ? 0 : parsed;
}

// describePageError prints where the error was thrown as url:line:column, one-based the way editors
// and DevTools show it; PageError's own line and column stay zero-based, as Chrome reports them.
export function describePageError(pageError: PageError): string {
  return pageError.url ? `${pageError.message} (${pageError.url}:${pageError.line + 1}:${pageError.column + 1})` : pageError.message;
}

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : String(error); }

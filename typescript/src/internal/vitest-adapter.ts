import type {ContextDiagnostics, Session} from "../index.js";

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
    const consoleMessages = (await Promise.all(current.map(async (session) => await session.consoleMessages().catch(() => [])))).flat();
    const consoleErrors = consoleMessages.filter((message) => message.type === "error" || message.type === "assert");
    if (this.#options.replayConsoleErrors ?? true) {
      for (const message of consoleErrors) this.#options.output(`[browser ${message.type}] ${message.text}\n`);
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

function errorMessage(error: unknown): string { return error instanceof Error ? error.message : String(error); }

import {afterEach, describe, expect, it, vi} from "vitest";
import {deflateSync} from "node:zlib";

import {installBilobaVitestHooks} from "../src/vitest.js";
import {detectInlineProtocol, formatDiagnostics, inlinePNG} from "../src/internal/diagnostics-formatter.js";
import {VitestAdapter} from "../src/internal/vitest-adapter.js";
import type {ContextDiagnostics, Session} from "../src/index.js";

const captures: Array<Record<string, unknown>> = [];
const session = {contextId: "context-a", consoleMessages: async () => [], captureDiagnostics: async (options: Record<string, unknown>) => { captures.push(options); return {purpose: options.purpose, tabs: []} as ContextDiagnostics; }} as unknown as Session;
const hooks = installBilobaVitestHooks({sessions: () => session, progressAfterMs: 60_000, output: () => undefined});

describe("Vitest diagnostics adapter", () => {
  it("offers explicit progress capture without duplicating a context", async () => {
    await expect(hooks.captureProgress("waiting for checkout")).resolves.toEqual([{purpose: "progress", tabs: []}]);
    expect(captures.at(-1)).toEqual({purpose: "progress", name: "waiting for checkout"});
  });
});

describe("Vitest diagnostics lifecycle", () => {
  afterEach(() => vi.useRealTimers());

  it("captures ordinary and Biloba test failures without hardcoding capture members", async () => {
    const requested: unknown[] = [];
    const fake = fakeSession("a", requested);
    const adapter = adapterFor([fake]);
    await adapter.finish("ordinary assertion", true);
    await adapter.finish("Biloba timeout", true);
    expect(requested).toEqual([{purpose: "failure", name: "ordinary assertion"}, {purpose: "failure", name: "Biloba timeout"}]);
  });

  it("preserves the primary failure when capture fails", async () => {
    const output: string[] = [];
    const fake = {...fakeSession("a", []), captureDiagnostics: async () => { throw new Error("disk unavailable"); }} as unknown as Session;
    await expect(adapterFor([fake], output).finish("primary assertion", true)).resolves.toBeUndefined();
    expect(output.join("")).toContain("Biloba failure diagnostics failed: disk unavailable");
  });

  it("replays console entries before artifacts and promotes console.assert at the boundary", async () => {
    const output: string[] = [];
    const fake = {...fakeSession("a", []), consoleMessages: async () => [
      {type: "error", text: "first", args: [], timestamp: "", stack: []},
      {type: "assert", text: "broken invariant", args: [], timestamp: "", stack: []},
    ]} as unknown as Session;
    const adapter = adapterFor([fake], output);
    await expect(adapter.finish("console", false)).rejects.toThrow("browser console.assert failed: broken invariant");
    expect(output).toEqual(["[browser error] first\n", "[browser assert] broken invariant\n", "artifact\n"]);
    await expect(adapter.finish("already failed", true)).resolves.toBeUndefined();
  });

  it("deduplicates contexts and awaits a fired progress capture before finish", async () => {
    vi.useFakeTimers();
    const requested: unknown[] = [];
    let release: (() => void) | undefined;
    const pending = new Promise<void>((resolve) => { release = resolve; });
    const first = fakeSession("same", requested, pending); const sibling = {...first, id: "sibling"} as Session;
    const adapter = adapterFor([first, sibling], [], 10);
    adapter.start("slow test");
    await vi.advanceTimersByTimeAsync(10);
    let finished = false; const finish = adapter.finish("slow test", false).then(() => { finished = true; });
    await Promise.resolve(); expect(finished).toBe(false);
    release?.(); await finish;
    expect(requested).toEqual([{purpose: "progress", name: "slow test"}]);
  });

  it("cancels an unfired timer on finish and dispose", async () => {
    vi.useFakeTimers();
    const requested: unknown[] = [];
    const adapter = adapterFor([fakeSession("a", requested)], [], 10);
    adapter.start("finished"); await adapter.finish("finished", false);
    adapter.start("disposed"); await adapter.dispose();
    await vi.runAllTimersAsync();
    expect(requested).toEqual([]);
  });
});

describe("diagnostics formatting", () => {
  const png = onePixelPNG();

  it("gates and renders iTerm, Kitty, and SIXEL terminal protocols", () => {
    expect(inlinePNG(png, {})).toBe("");
    expect(inlinePNG(png, {BILOBA_INLINE_SCREENSHOTS: "iterm"})).toMatch(/^\u001b\]1337;File=/);
    expect(inlinePNG(png, {BILOBA_INLINE_SCREENSHOTS: "kitty"})).toMatch(/^\u001b_Gf=100,a=T;/);
    expect(inlinePNG(png, {BILOBA_INLINE_SCREENSHOTS: "sixel"})).toMatch(/^\u001bPq"1;1;1;1.*\u001b\\$/s);
    expect(inlinePNG(png, {BILOBA_INLINE_SCREENSHOTS: "none"})).toBe("");
    // "none" means off, including in a terminal that could render.  Falling through to detection here
    // made the documented kill switch the one value that turned images on.
    expect(inlinePNG(png, {BILOBA_INLINE_SCREENSHOTS: "none", TERM_PROGRAM: "iTerm.app"})).toBe("");
    expect(inlinePNG(png, {BILOBA_INLINE_SCREENSHOTS: "off", TERM_PROGRAM: "iTerm.app"})).toBe("");
    // "auto" and unrecognized values fall through to detection, as they do in Go.
    expect(inlinePNG(png, {BILOBA_INLINE_SCREENSHOTS: "auto", TERM_PROGRAM: "iTerm.app"})).toMatch(/^\u001b\]1337/);
  });

  it("detects the same terminals screenshots.go does", () => {
    expect(detectInlineProtocol({KITTY_WINDOW_ID: "1"})).toBe("kitty");
    expect(detectInlineProtocol({TERM: "xterm-kitty"})).toBe("kitty");
    expect(detectInlineProtocol({TERM_PROGRAM: "ghostty"})).toBe("kitty");
    expect(detectInlineProtocol({TERM_PROGRAM: "vscode"})).toBe("sixel");
    expect(detectInlineProtocol({TERM_PROGRAM: "WezTerm"})).toBe("iterm");
    expect(detectInlineProtocol({TERM_PROGRAM: "rio"})).toBe("iterm");
    expect(detectInlineProtocol({LC_TERMINAL: "iTerm2"})).toBe("iterm");
    expect(detectInlineProtocol({KONSOLE_VERSION: "231200"})).toBe("iterm");
    expect(detectInlineProtocol({TERM: "mintty"})).toBe("iterm");
    expect(detectInlineProtocol({TERM: "dumb"})).toBe("none");
  });

  it("says why a failure screenshot is neither shown nor saved", () => {
    const diagnostics = [{purpose: "failure", tabs: [{targetId: "tab", title: "", screenshot: png, errors: []}]}] as unknown as ContextDiagnostics[];
    expect(formatDiagnostics(diagnostics, {})).toContain("inline screenshots disabled");
    expect(formatDiagnostics(diagnostics, {TERM_PROGRAM: "iTerm.app"})).not.toContain("inline screenshots disabled");
  });

  it("prints console-following artifact paths, outlines, and secondary errors", () => {
    const diagnostics = [{purpose: "failure", tabs: [{targetId: "tab", title: "", screenshotPath: "/tmp/failure.png", outlinePath: "/tmp/failure.txt", errors: [{artifact: "screenshot", code: "IO_ERROR", message: "disk full"}]}]}] as ContextDiagnostics[];
    expect(formatDiagnostics(diagnostics, {})).toBe("screenshot: /tmp/failure.png\noutline: /tmp/failure.txt\ndiagnostic screenshot failed: disk full\n");
  });
});

function fakeSession(contextId: string, requested: unknown[], wait?: Promise<void>): Session {
  return {id: contextId, contextId, consoleMessages: async () => [], captureDiagnostics: async (options: Record<string, unknown>) => {
    requested.push(options); await wait; return {purpose: options.purpose, tabs: []} as ContextDiagnostics;
  }} as unknown as Session;
}

function adapterFor(sessions: readonly Session[], output: string[] = [], progressAfterMs?: number): VitestAdapter {
  return new VitestAdapter({sessions: () => sessions, ...(progressAfterMs !== undefined && {progressAfterMs}), output: (text) => output.push(text), format: () => "artifact\n"});
}

function onePixelPNG(): Uint8Array {
  const chunk = (type: string, data: Buffer) => {
    const result = Buffer.alloc(12 + data.length); result.writeUInt32BE(data.length); result.write(type, 4, 4, "ascii"); data.copy(result, 8); return result;
  };
  const header = Buffer.alloc(13); header.writeUInt32BE(1); header.writeUInt32BE(1, 4); header[8] = 8; header[9] = 6;
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk("IHDR", header), chunk("IDAT", deflateSync(Buffer.from([0, 255, 0, 0, 255]))), chunk("IEND", Buffer.alloc(0))]);
}

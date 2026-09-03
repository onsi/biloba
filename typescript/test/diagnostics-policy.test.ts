import {describe, expect, it, vi} from "vitest";

import {automationDetected, booleanEnvironment, resolveDiagnosticsPolicy} from "../src/internal/client.js";

describe("diagnostics policy", () => {
  it("uses interactive defaults without forcing disk output", () => {
    expect(resolveDiagnosticsPolicy({}, {}, false)).toEqual({failureScreenshots: true, failureOutlines: false, progressScreenshots: true, progressOutlines: false, pollTrajectory: true, inlineScreenshots: "auto", maxScreenshotBytes: 16 * 1024 * 1024});
  });

  it("detects canonical agent environments and applies inline environment selection", () => {
    for (const key of ["CI", "AI_AGENT", "CLAUDECODE", "CURSOR_AGENT", "GEMINI_CLI", "CODEX_SANDBOX"]) expect(automationDetected({[key]: "1"})).toBe(true);
    expect(resolveDiagnosticsPolicy({}, {BILOBA_INLINE_SCREENSHOTS: "none"}, false).inlineScreenshots).toBe(false);
    expect(resolveDiagnosticsPolicy({}, {BILOBA_INLINE_SCREENSHOTS: "kitty"}, false).inlineScreenshots).toBe(true);
    expect(resolveDiagnosticsPolicy({inlineScreenshots: false}, {BILOBA_INLINE_SCREENSHOTS: "kitty"}, false).inlineScreenshots).toBe(false);
    expect(resolveDiagnosticsPolicy({}, {BILOBA_INTERACTIVE: "1"}, true).inlineScreenshots).toBe("auto");
  });

  it("parses interactive booleans without JavaScript truthiness and warns on invalid values", () => {
    const warning = vi.spyOn(process, "emitWarning").mockImplementation(() => undefined);
    expect(booleanEnvironment("BILOBA_INTERACTIVE", {BILOBA_INTERACTIVE: "1"})).toBe(true);
    expect(booleanEnvironment("BILOBA_INTERACTIVE", {BILOBA_INTERACTIVE: "TRUE"})).toBe(true);
    expect(booleanEnvironment("BILOBA_INTERACTIVE", {BILOBA_INTERACTIVE: "0"})).toBe(false);
    expect(booleanEnvironment("BILOBA_INTERACTIVE", {BILOBA_INTERACTIVE: "false"})).toBe(false);
    expect(booleanEnvironment("BILOBA_INTERACTIVE", {BILOBA_INTERACTIVE: "sometimes"})).toBeUndefined();
    expect(resolveDiagnosticsPolicy({}, {BILOBA_INTERACTIVE: "0"}, true).inlineScreenshots).toBe(false);
    expect(resolveDiagnosticsPolicy({}, {BILOBA_INLINE_SCREENSHOTS: "bogus"}, false).inlineScreenshots).toBe("auto");
    expect(warning).toHaveBeenCalledTimes(2);
    warning.mockRestore();
  });

  it("uses automation defaults and lets each explicit member win", () => {
    expect(resolveDiagnosticsPolicy({failureOutlines: false, inlineScreenshots: true}, {BILOBA_SCREENSHOTS_DIR: "env-shots"}, true)).toMatchObject({artifactDir: "env-shots", failureScreenshots: true, failureOutlines: false, inlineScreenshots: true});
    expect(resolveDiagnosticsPolicy({}, {}, true)).toMatchObject({artifactDir: "./biloba-screenshots", failureOutlines: true, inlineScreenshots: false});
  });
});

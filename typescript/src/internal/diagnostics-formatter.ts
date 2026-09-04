import {inflateSync} from "node:zlib";

import type {ContextDiagnostics} from "../index.js";

export function formatDiagnostics(values: readonly ContextDiagnostics[], environment: NodeJS.ProcessEnv = process.env): string {
  const lines: string[] = [];
  for (const diagnostics of values) for (const tab of diagnostics.tabs) {
    if (tab.screenshotPath) lines.push(`screenshot: ${tab.screenshotPath}`);
    if (tab.screenshot) {
      const inline = inlinePNG(tab.screenshot, environment);
      // Go says so out loud rather than printing nothing, which otherwise reads as "there was no
      // screenshot" when the truth is "this terminal cannot show it and nowhere was set to put it".
      if (inline) lines.push(inline);
      else if (!tab.screenshotPath) lines.push("(inline screenshots disabled; configure an artifactDir to save screenshot files)");
    }
    if (tab.outlinePath) lines.push(`outline: ${tab.outlinePath}`);
    else if (tab.domOutline) lines.push(tab.domOutline);
    for (const error of tab.errors) lines.push(`diagnostic ${error.artifact} failed: ${error.message}`);
  }
  return lines.length === 0 ? "" : `${lines.join("\n")}\n`;
}

// Mirrors detectInlineImageProtocol/inlineImageProtocolFromEnv in screenshots.go.  BILOBA_INLINE_
// SCREENSHOTS forces a protocol; "none"/"off"/"false" force it off; "auto" and anything
// unrecognized fall through to terminal detection - previously "none" was the one value routed to
// detection, so the documented way to force images off turned them on.
export function detectInlineProtocol(environment: NodeJS.ProcessEnv): string {
  switch ((environment.BILOBA_INLINE_SCREENSHOTS ?? "").toLowerCase()) {
    case "iterm": case "iterm2": return "iterm";
    case "kitty": return "kitty";
    case "sixel": return "sixel";
    case "none": case "off": case "false": return "none";
  }
  const termProgram = environment.TERM_PROGRAM ?? "";
  const term = environment.TERM ?? "";
  if (environment.KITTY_WINDOW_ID || term === "xterm-kitty" || termProgram === "ghostty") return "kitty";
  // VSCode's integrated terminal renders Sixel but not OSC 1337.
  if (termProgram === "vscode") return "sixel";
  if (termProgram === "iTerm.app" || termProgram === "WezTerm" || termProgram === "rio") return "iterm";
  if (environment.LC_TERMINAL === "iTerm2") return "iterm";   // iTerm2 forwarded over ssh
  if (environment.KONSOLE_VERSION) return "iterm";            // Konsole speaks OSC 1337
  if (term === "mintty") return "iterm";
  return "none";
}

export function inlinePNG(bytes: Uint8Array, environment: NodeJS.ProcessEnv): string {
  const protocol = detectInlineProtocol(environment);
  const encoded = Buffer.from(bytes).toString("base64");
  if (protocol === "iterm") return `\u001b]1337;File=inline=1;preserveAspectRatio=1:${encoded}\u0007`;
  if (protocol === "kitty") return `\u001b_Gf=100,a=T;${encoded}\u001b\\`;
  if (protocol === "sixel") return pngToSixel(bytes);
  return "";
}

function pngToSixel(bytes: Uint8Array): string {
  const png = Buffer.from(bytes);
  if (!png.subarray(0, 8).equals(Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]))) return "";
  let width = 0, height = 0, colorType = 0, bitDepth = 0, interlace = 0;
  const data: Buffer[] = [];
  for (let offset = 8; offset + 12 <= png.length;) {
    const length = png.readUInt32BE(offset); const type = png.subarray(offset + 4, offset + 8).toString("ascii");
    if (offset + 12 + length > png.length) return "";
    const chunk = png.subarray(offset + 8, offset + 8 + length);
    if (type === "IHDR") { width = chunk.readUInt32BE(0); height = chunk.readUInt32BE(4); bitDepth = chunk[8] ?? 0; colorType = chunk[9] ?? 0; interlace = chunk[12] ?? 0; }
    else if (type === "IDAT") data.push(chunk);
    else if (type === "IEND") break;
    offset += 12 + length;
  }
  const channels = colorType === 6 ? 4 : colorType === 2 ? 3 : 0;
  if (width <= 0 || height <= 0 || bitDepth !== 8 || channels === 0 || interlace !== 0) return "";
  const packed = inflateSync(Buffer.concat(data)); const stride = width * channels; const rows = Buffer.alloc(stride * height);
  for (let y = 0, input = 0; y < height; y++) {
    const filter = packed[input++] ?? 0;
    for (let x = 0; x < stride; x++, input++) {
      const raw = packed[input] ?? 0; const left = x >= channels ? rows[y * stride + x - channels] ?? 0 : 0;
      const up = y > 0 ? rows[(y - 1) * stride + x] ?? 0 : 0; const upperLeft = y > 0 && x >= channels ? rows[(y - 1) * stride + x - channels] ?? 0 : 0;
      const predictor = filter === 0 ? 0 : filter === 1 ? left : filter === 2 ? up : filter === 3 ? Math.floor((left + up) / 2) : filter === 4 ? paeth(left, up, upperLeft) : -1;
      if (predictor < 0) return "";
      rows[y * stride + x] = (raw + predictor) & 0xff;
    }
  }
  const palette = new Set<number>(); const pixels = new Uint8Array(width * height);
  for (let index = 0; index < pixels.length; index++) {
    const source = index * channels; const alpha = channels === 4 ? rows[source + 3] ?? 255 : 255;
    const color = alpha < 128 ? 0 : 1 + Math.round((rows[source] ?? 0) / 51) * 36 + Math.round((rows[source + 1] ?? 0) / 51) * 6 + Math.round((rows[source + 2] ?? 0) / 51);
    pixels[index] = color; palette.add(color);
  }
  let sixel = `\u001bPq\"1;1;${width};${height}`;
  const colors = [...palette];
  for (const color of colors) {
    if (color === 0) sixel += "#0;2;0;0;0";
    else { const value = color - 1; sixel += `#${color};2;${Math.floor(value / 36) * 20};${(Math.floor(value / 6) % 6) * 20};${(value % 6) * 20}`; }
  }
  for (let top = 0; top < height; top += 6) for (const color of colors) {
    sixel += `#${color}`;
    for (let x = 0; x < width; x++) { let bits = 0; for (let dy = 0; dy < 6 && top + dy < height; dy++) if (pixels[(top + dy) * width + x] === color) bits |= 1 << dy; sixel += String.fromCharCode(63 + bits); }
    sixel += color === colors.at(-1) ? "-" : "$";
  }
  return `${sixel}\u001b\\`;
}

function paeth(left: number, up: number, upperLeft: number): number {
  const estimate = left + up - upperLeft; const leftDistance = Math.abs(estimate - left); const upDistance = Math.abs(estimate - up); const upperLeftDistance = Math.abs(estimate - upperLeft);
  return leftDistance <= upDistance && leftDistance <= upperLeftDistance ? left : upDistance <= upperLeftDistance ? up : upperLeft;
}

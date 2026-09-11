import {createRequire} from "node:module";

let cached: string | undefined;

// packageVersion reads the biloba npm package's own version out of its package.json at runtime
// rather than hard-coding it, so it never drifts from what actually shipped. A relative path works
// from both locations this module is loaded from: src/internal/package-version.ts (running under
// vitest, which transpiles TS in place) and dist/internal/package-version.js (tsconfig.build.json's
// rootDir src / outDir dist) - both are exactly two directories below the package root. Returns ""
// on any failure (an unreadable or malformed package.json) so a caller that cannot resolve a
// version simply falls back rather than throwing. The result is memoized after the first read -
// the package's own version never changes over the life of a process.
export function packageVersion(): string {
  if (cached !== undefined) return cached;
  try {
    const packageJSON = createRequire(import.meta.url)("../../package.json") as {version?: unknown};
    cached = typeof packageJSON.version === "string" ? packageJSON.version : "";
  } catch {
    cached = "";
  }
  return cached;
}

---
name: visual-assertions
description: Create and maintain Biloba screenshot assertions in TypeScript/Vitest — page and locator captures, committed baselines, update mode, masks, pixel and channel tolerances, animation freezing, light/dark schemes, diff artifacts, and structured visual diagnosis. Use when adding expectScreenshot(), reviewing baselines, or diagnosing a visual mismatch.
---

# Vitest visual assertions

Call `expectScreenshot()` on a session or locator:

```ts
const result = await session.getByTestId("chart").expectScreenshot("revenue-chart", {
  mask: [session.getByTestId("last-updated")],
  colorSchemes: ["light", "dark"],
  pixelTolerance: 0.001,
});
expect(result.match).toBe(true);
```

The first run must fail when the baseline is missing. Review the actual image, enable update mode deliberately, write the baseline, then commit it. Never leave update mode enabled in CI: it turns comparisons into writes and makes the suite pass without checking regressions.

## Keep the comparison meaningful

- Mask timestamps, random avatars, and other intentionally dynamic regions.
- Prefer `channelTolerance` for small rasterization deltas spread across many pixels.
- Use `pixelTolerance` only when evidence supports allowing a bounded changed area.
- Keep per-assertion overrides rare; a tolerance that keeps growing is evidence that the subject is nondeterministic.
- Animations, transitions, and the caret freeze automatically. Opt into animated capture only when motion is the subject.
- When capturing both color schemes, ensure the app still follows `prefers-color-scheme`; a stored/manual override can make both baselines identical.

A mismatch throws `BilobaError`. Read its `visual` diagnosis and `artifactPaths` before opening the PNGs: the pixel count and amplitude verdict distinguish rendering noise from a real content or color change. The baseline is committed evidence; `.actual.png` and `.diff.png` are disposable failure artifacts.

Failure handling → `biloba-vitest:debug-failures`. Flake prevention → `biloba-vitest:flaky-tests`. Docs: <https://onsi.github.io/biloba/vitest.html#screenshots-and-visual-assertions>.

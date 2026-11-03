---
name: visual-assertions
description: Create and maintain Biloba screenshot assertions in TypeScript/Vitest — page and locator captures, committed baselines, update mode, masks, pixel and channel tolerances, animation freezing, light/dark schemes, diff artifacts, and structured visual diagnosis. Use when adding expectScreenshot(), reviewing baselines, or diagnosing a visual mismatch.
---

# Vitest visual assertions

Call `expectScreenshot()` on a session or locator:

```ts
await session.getByTestId("chart").expectScreenshot("revenue-chart", {
  mask: [session.getByTestId("last-updated")],
  colorSchemes: ["light", "dark"],
  pixelTolerance: 0.001,
});
```

Nothing needs asserting on the result: it resolves only when the screenshot matched or, in update mode, when the baseline was written, and its `match` is `true` either way. `updated` says whether the call wrote a baseline; each `schemes[i].status` is `matched`, `created`, `updated`, or `unchanged`.

The first run must fail when the baseline is missing. Review the actual image, enable update mode deliberately (`BILOBA_UPDATE_SCREENSHOTS=1`, or `updateScreenshots: true` on `connect`), write the baseline, then commit it. Never leave update mode enabled in CI: it turns comparisons into writes and makes the suite pass without checking regressions.

## Keep the comparison meaningful

- Mask timestamps, random avatars, and other intentionally dynamic regions.
- Prefer `channelTolerance` for small rasterization deltas spread across many pixels.
- Use `pixelTolerance` only when evidence supports allowing a bounded changed area.
- Keep per-assertion overrides rare; a tolerance that keeps growing is evidence that the subject is nondeterministic.
- Element captures round the element's box outward to whole CSS pixels.
- In `expectScreenshot()`, animations, transitions, and the caret freeze automatically. CSS animations on a scroll or view timeline keep running, since they depend only on the scroll position. Opt into animated capture only when motion is the subject. A plain `captureScreenshot()` is not frozen unless you pass `frozen: true`.
- When capturing both color schemes, ensure the app still follows `prefers-color-scheme`; a stored/manual override can make both baselines identical.

A mismatch throws `BilobaError`. Read its `visual` diagnosis and `artifactPaths` before opening the PNGs: the pixel count and amplitude verdict distinguish rendering noise from a real content or color change. The baseline is committed evidence; `.actual.png` and `.diff.png` are disposable failure artifacts.

Failure handling → `biloba-vitest:debug-failures`. Flake prevention → `biloba-vitest:flaky-tests`. Docs: <https://onsi.github.io/biloba/vitest.html#screenshots-and-visual-assertions>.

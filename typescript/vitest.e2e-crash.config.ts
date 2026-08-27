import {defineConfig} from "vitest/config";

// The crash file gets its own run, and therefore its own shared Chrome, because it kills that
// Chrome on purpose.  Sharing a browser with the worker-* files would mean destroying theirs
// mid-assertion and reporting it as their failure.  One fork: these are about what a worker sees
// when things die, not about concurrency, and the file's specs run in a deliberate order.
export default defineConfig({
  test: {
    environment: "node",
    globalSetup: ["./test/e2e/global-setup.ts"],
    include: ["test/e2e/crash.e2e.test.ts"],
    pool: "forks",
    poolOptions: {forks: {minForks: 1, maxForks: 1}},
    testTimeout: 60_000,
    hookTimeout: 60_000,
  },
});

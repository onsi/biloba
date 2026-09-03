import {defineConfig} from "vitest/config";

// The multi-worker end-to-end suite.  Every knob here is load-bearing:
//
//   globalSetup      starts exactly one Chrome, in the main process, and provides its websocket
//                    URL to the workers - so the browser is genuinely shared rather than one
//                    browser per file pretending to be.
//   pool: "forks"    each test file gets its own OS process.  This is the point: the topology
//                    claim is about worker *processes*, and a thread pool would not test it.
//   fileParallelism  the files must run at the same time, or the rendezvous barriers below never
//   + min/maxForks   release and the suite fails - which is the correct outcome, because a serial
//                    run has not exercised concurrent workers at all.
export default defineConfig({
  test: {
    environment: "node",
    globalSetup: ["./test/e2e/global-setup.ts"],
    include: ["test/e2e/worker-*.e2e.test.ts"],
    pool: "forks",
    fileParallelism: true,
    poolOptions: {forks: {minForks: 3, maxForks: 3, isolate: true}},
    testTimeout: 60_000,
    hookTimeout: 60_000,
  },
});

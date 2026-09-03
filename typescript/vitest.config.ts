import {defineConfig} from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    testTimeout: 5_000,
    // The two real-daemon suites need a Go binary that pnpm has no business building, and both
    // fail rather than skip when it is missing, so neither can ride along here.  `pnpm test`
    // covers the unit suites; `pnpm test:parity` and `pnpm test:e2e` (via `make driver-parity` and
    // `make driver-e2e`, which build the daemon first) run them through their own configs - the
    // e2e one needs a globalSetup and a forked, genuinely parallel pool that would be wrong for
    // unit tests.  CI runs all three; see the driver job in .github/workflows/test.yml.
    exclude: ["**/node_modules/**", "**/dist/**", "test/parity.test.ts", "test/e2e/**"],
  },
});

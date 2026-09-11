import {defineConfig} from "vitest/config";

export default defineConfig({
  test: {
    globalSetup: ["./test/global-setup.ts"],
    pool: "forks",
    testTimeout: 30_000,
    hookTimeout: 30_000,
  },
});

import {defineConfig} from "vitest/config";

// The Go/TypeScript parity contract: a real bilobad, a real shared Chrome, and the same fixture
// and expectations the Go suite asserts against (see graft_parity_test.go).  Run it with
// `make driver-parity`, which builds the daemon and points BILOBA_DAEMON_EXECUTABLE at it.
export default defineConfig({
  test: {
    environment: "node",
    // A real browser start plus a shared-Chrome handshake needs more room than a unit test.
    testTimeout: 30_000,
    hookTimeout: 60_000,
    include: ["test/parity.test.ts"],
  },
});

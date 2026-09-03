import {readFile} from "node:fs/promises";
import {describe, expect, it} from "vitest";

import type {OperationResult, ProtocolError, Request, Response} from "../src/generated/protocol.js";
import {encodeFrame} from "../src/internal/framing.js";

// These assert with toEqual, not toMatchObject: the goldens exist to catch a field the daemon
// starts or stops emitting, and toMatchObject ignores exactly that.  The Go side has the mirror of
// this check (protocol/encoding_test.go) tying the generated declarations to what json.Marshal
// actually produces.
describe("Go-generated protocol goldens", () => {
  it("matches the TypeScript request envelope and framing", async () => {
    const golden = await loadGolden<Request>("handshake-request.json");
    expect(golden).toEqual({id: 1, method: "handshake", params: {protocolVersion: "2"}});

    const frame = encodeFrame(golden);
    expect(frame.readUInt32LE(0)).toBe(frame.length - 4);
    expect(JSON.parse(frame.subarray(4).toString("utf8"))).toEqual(golden);
  });

  it("keeps structured errors stable", async () => {
    const golden = await loadGolden<Response<never>>("protocol-error-response.json");
    expect(golden).toEqual({
      id: 7,
      error: {code: "PROTOCOL_MISMATCH", message: "protocol version mismatch"} satisfies ProtocolError,
    });
  });

  it("keeps operation metadata stable, and omits diagnostics when there are none", async () => {
    const golden = await loadGolden<Response<OperationResult>>("operation-response.json");
    expect(golden).toEqual({
      id: 9,
      result: {
        matched: true,
        observedJson: '"Saved"',
        attemptCount: 2,
        trajectory: [
          {attempt: 1, elapsedMs: 0, observedJson: '"Saving"'},
          {attempt: 2, elapsedMs: 10, observedJson: '"Saved"'},
        ],
        timings: {startedUnixMs: 1_700_000_000_000, elapsedMs: 10},
        rpcRequestCount: 1,
        rpcResponseCount: 1,
      } satisfies OperationResult,
    });
    // diagnostics is declared optional, so it must genuinely be absent rather than an empty object
    expect(golden.result).not.toHaveProperty("diagnostics");
  });
});

async function loadGolden<T>(name: string): Promise<T> {
  const contents = await readFile(new URL(`../../protocol/testdata/golden/${name}`, import.meta.url), "utf8");
  return JSON.parse(contents) as T;
}

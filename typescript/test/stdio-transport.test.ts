import {once} from "node:events";
import {PassThrough} from "node:stream";
import {afterEach, beforeEach, describe, expect, it, vi} from "vitest";

import type {Request} from "../src/generated/protocol.js";
import {encodeFrame, FrameDecoder, MAX_FRAME_SIZE} from "../src/internal/framing.js";
import {StdioTransport} from "../src/internal/stdio-transport.js";

describe("stdio transport", () => {
  let fromClient: PassThrough;
  let toClient: PassThrough;
  let requests: FrameDecoder;
  let transport: StdioTransport;

  beforeEach(() => {
    fromClient = new PassThrough();
    toClient = new PassThrough();
    requests = new FrameDecoder();
    fromClient.pipe(requests);
    transport = new StdioTransport(toClient, fromClient);
  });

  afterEach(() => transport.close());

  it("correlates a fragmented framed response by request id", async () => {
    const handshake = transport.handshake({protocolVersion: "1"});
    const request = await nextRequest(requests);
    expect(request).toEqual({id: 1, method: "handshake", params: {protocolVersion: "1"}, timeoutMs: 30_000});

    const frame = encodeFrame({id: request.id, result: {protocolVersion: "1", capabilities: ["evaluate"]}});
    toClient.write(frame.subarray(0, 3));
    toClient.write(frame.subarray(3));

    await expect(handshake).resolves.toEqual({protocolVersion: "1", capabilities: ["evaluate"]});
  });

  it("gives every non-polling operation a default deadline the caller can override", async () => {
    // These requests are never answered; the afterEach close rejects them.
    transport.navigate({sessionId: "session-1", url: "http://localhost/"}).catch(() => undefined);
    expect(await nextRequest(requests)).toMatchObject({method: "navigate", timeoutMs: 30_000});

    transport.prepareSession({sessionId: "session-1"}, {timeoutMs: 250}).catch(() => undefined);
    expect(await nextRequest(requests)).toMatchObject({method: "prepareSession", timeoutMs: 250});

    // The polling operations derive their deadline from the poll budget instead - a default here
    // would race the daemon's own poll timeout.
    transport.assert({
      sessionId: "session-1",
      assertion: {kind: "VISIBLE", locator: {kind: "CSS", value: "main", first: false}},
    }).catch(() => undefined);
    expect(await nextRequest(requests)).not.toHaveProperty("timeoutMs");
  });

  it("fails a silent non-polling operation locally once its default deadline passes", async () => {
    vi.useFakeTimers();
    try {
      const navigation = transport.navigate({sessionId: "session-1", url: "http://localhost/"});
      const failure = navigation.catch((reason: unknown) => reason);
      await vi.advanceTimersByTimeAsync(30_000);
      expect(await failure).toMatchObject({code: "TIMEOUT", message: "Biloba request timed out after 30000ms"});
    } finally {
      vi.useRealTimers();
    }
  });

  it("maps structured daemon errors without transport-specific status codes", async () => {
    const session = transport.openSession({});
    const request = await nextRequest(requests);
    toClient.write(encodeFrame({
      id: request.id,
      error: {code: "DRIVER_ERROR", message: "chrome disconnected", diagnostics: {daemonDetail: "websocket EOF"}},
    }));

    await expect(session).rejects.toMatchObject({
      code: "DRIVER_ERROR",
      message: "chrome disconnected",
      daemonDetail: "websocket EOF",
    });
  });

  it("preserves fatal visual diagnostics and derives their artifact paths", async () => {
    const comparison = transport.screenshot({sessionId: "session-1", operation: {kind: "EXPECT", target: {kind: "PAGE"}, name: "card"}});
    const request = await nextRequest(requests);
    const visual = {
      match: false,
      updated: false,
      warnings: [],
      schemes: [{status: "missing", match: false, baselinePath: "/tmp/card.png", actualPath: "/tmp/card.actual.png"}],
      attemptCount: 1,
      elapsedMs: 2,
    };
    toClient.write(encodeFrame({id: request.id, error: {code: "VISUAL_BASELINE", message: "missing baseline", diagnostics: {visual}}}));

    await expect(comparison).rejects.toMatchObject({
      code: "VISUAL_BASELINE",
      visual,
      artifactPaths: ["/tmp/card.png", "/tmp/card.actual.png"],
    });
  });

  it("sends an explicit cancellation frame for an AbortSignal", async () => {
    const controller = new AbortController();
    const assertion = transport.assert({
      sessionId: "session-1",
      assertion: {kind: "VISIBLE", locator: {kind: "CSS", value: "main", first: false}},
    }, {signal: controller.signal});
    const request = await nextRequest(requests);
    controller.abort("worker stopped");

    await expect(assertion).rejects.toMatchObject({code: "CANCELLED"});
    expect(await nextRequest(requests)).toEqual({
      id: 0,
      method: "cancel",
      params: {requestId: request.id},
    });
  });
  it("does not leak an unhandled rejection when the cancel frame cannot be written", async () => {
    const rejections: unknown[] = [];
    const record = (reason: unknown) => rejections.push(reason);
    process.on("unhandledRejection", record);
    try {
      const controller = new AbortController();
      const assertion = transport.assert({
        sessionId: "session-1",
        assertion: {kind: "VISIBLE", locator: {kind: "CSS", value: "main", first: false}},
      }, {signal: controller.signal});
      await nextRequest(requests);

      // A worker that aborts its in-flight work and tears the browser down in the same turn: the
      // best-effort cancel frame is queued behind a closed transport and can never be written.  Its
      // rejection must not escape - an unhandled rejection here takes the whole worker process down.
      controller.abort("worker stopped");
      transport.close();

      await expect(assertion).rejects.toMatchObject({code: "CANCELLED"});
      await settleRejections();
      expect(rejections).toEqual([]);
    } finally {
      process.off("unhandledRejection", record);
    }
  });

  it("returns callback errors and substitutes an oversize callback result without desynchronizing", async () => {
    transport.registerResponseCallback("throws", () => { throw new Error("transform exploded"); });
    toClient.write(encodeFrame({event: "responseIntercepted", invocationId: "throw-1", callbackId: "throws", payload: {}}));
    expect(await nextRequest(requests)).toEqual({id: 0, method: "callbackResult", params: {invocationId: "throw-1", error: "transform exploded"}});

    transport.registerResponseCallback("oversize", () => ({bodyBase64: "x".repeat(MAX_FRAME_SIZE)}));
    toClient.write(encodeFrame({event: "responseIntercepted", invocationId: "large-1", callbackId: "oversize", payload: {}}));
    const fallback = await nextRequest(requests);
    expect(fallback).toMatchObject({id: 0, method: "callbackResult", params: {invocationId: "large-1"}});
    expect((fallback.params as {error?: string}).error).toContain("maximum");

    const handshake = transport.handshake({protocolVersion: "1"});
    const request = await nextRequest(requests);
    toClient.write(encodeFrame({id: request.id, result: {protocolVersion: "1", capabilities: []}}));
    await expect(handshake).resolves.toMatchObject({protocolVersion: "1"});
  });
});

// unhandledRejection is emitted a turn after the microtask queue drains, so give it a few.
async function settleRejections(): Promise<void> {
  for (let turn = 0; turn < 3; turn++) await new Promise((resolve) => setTimeout(resolve, 5));
}

async function nextRequest(decoder: FrameDecoder): Promise<Request> {
  const [value] = await once(decoder, "data");
  return value as Request;
}

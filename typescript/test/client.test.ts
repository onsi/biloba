import {PassThrough} from "node:stream";
import {afterEach, beforeEach, describe, expect, it} from "vitest";

import {
  BilobaError,
  type AssertionResult,
  type Browser,
  type Cookie,
} from "../src/index.js";
import {connectWithTransport} from "../src/internal/client.js";
import type {HandshakeResponse, OpenSessionResponse, OperationResult, Request} from "../src/generated/protocol.js";
import {encodeFrame, FrameDecoder} from "../src/internal/framing.js";
import {expectTimedOutAction, expectTimedOutAssertion} from "./support/assertions.js";
import {StdioTransport} from "../src/internal/stdio-transport.js";

// The stub daemon answers with the generated response types, not a free-form object.  A fake that
// can emit shapes the real daemon never emits is a fake that silently drifts: before this was
// typed, every operation here replied without timings/rpcRequestCount/rpcResponseCount, which
// resultToWire (protocol/server.go) always sets and the generated declarations mark required.
// These tests are about the client's translation layer - the wire shape they translate has to be
// one the daemon could actually produce.
type Respond = (result: OperationResult) => void;
type Implementation = (request: Record<string, unknown>, respond: Respond) => void;

// Fills in the fields resultToWire sets on every single response, so a test only has to state the
// part it actually cares about.
function operationResult(overrides: Partial<OperationResult> = {}): OperationResult {
  return {
    matched: true,
    attemptCount: 1,
    timings: {startedUnixMs: 1_700_000_000_000, elapsedMs: 0},
    rpcRequestCount: 1,
    rpcResponseCount: 1,
    ...overrides,
  };
}

describe("Biloba TypeScript client", () => {
  let transport: StdioTransport;
  let toClient: PassThrough;
  let browser: Browser | undefined;
  let assertImplementation: Implementation;
  let clickImplementation: Implementation;
  let observeCancel: (() => void) | undefined;
  const requests: Array<{method: string; request: Record<string, unknown>}> = [];
  let openedSessions = 0;

  beforeEach(() => {
    requests.length = 0;
    openedSessions = 0;
    assertImplementation = (_request, respond) => {
      respond(operationResult({
        observedJson: JSON.stringify("Saved"),
        attemptCount: 2,
        trajectory: [
          {attempt: 1, elapsedMs: 0, observedJson: JSON.stringify("Saving")},
          {attempt: 2, elapsedMs: 10, observedJson: JSON.stringify("Saved")},
        ],
      }));
    };
    clickImplementation = (_request, respond) => respond(operationResult());
    observeCancel = undefined;
    const fromClient = new PassThrough();
    toClient = new PassThrough();
    transport = new StdioTransport(toClient, fromClient);
    const decoder = new FrameDecoder();
    fromClient.pipe(decoder);
    decoder.on("data", (request: Request) => handleRequest(request));
  });

  afterEach(async () => {
    await browser?.close();
    transport.close();
  });

  function handleRequest(envelope: Request): void {
    const request = envelope.params as Record<string, unknown>;
    const method = envelope.method[0]!.toUpperCase() + envelope.method.slice(1);
    requests.push({method, request});
    const reply = (result: unknown) => toClient.write(encodeFrame({id: envelope.id, result}));
    const respond: Respond = reply;
    switch (envelope.method) {
      case "handshake": reply({protocolVersion: "1", capabilities: ["assertions", "evaluate"]} satisfies HandshakeResponse); break;
      case "openSession": reply({sessionId: `session-${++openedSessions}`} satisfies OpenSessionResponse); break;
      case "evaluate": respond(operationResult({observedJson: JSON.stringify({ready: true})})); break;
      case "assert": assertImplementation(request, respond); break;
      case "click": clickImplementation(request, respond); break;
      case "cancel": observeCancel?.(); break;
      case "closeSession":
      case "prepareSession": reply({}); break;
      default: respond(operationResult());
    }
  }

  async function connectClient(): Promise<Browser> {
    return await connectWithTransport(transport);
  }

  it("negotiates the protocol and sends idiomatic session operations over gRPC", async () => {
    browser = await connectClient();
    expect(browser.protocolVersion).toBe("1");
    expect(browser.capabilities).toEqual(new Set(["assertions", "evaluate"]));

    const session = await browser.openSession();
    const cookies: Cookie[] = [{name: "auth", value: "abc", domain: "localhost", httpOnly: true}];
    await session.prepare();
    await session.setCookies(cookies);
    await session.navigate("http://localhost/bookings");
    await session.getByRole("button", {name: "Save", exact: true}).first().click();
    await session.getByTestId("name").setValue("Ada");
    expect(await session.evaluate<{ready: boolean}>("window.appState")).toEqual({ready: true});
    await session.close();

    expect(requests).toMatchObject([
      {method: "Handshake", request: {protocolVersion: "1"}},
      {method: "OpenSession"},
      {method: "PrepareSession", request: {sessionId: "session-1"}},
      {method: "SetCookies", request: {sessionId: "session-1", cookies}},
      {method: "Navigate", request: {sessionId: "session-1", url: "http://localhost/bookings"}},
      {
        method: "Click",
        request: {
          sessionId: "session-1",
          locator: {
            kind: "ROLE",
            role: "button",
            name: "Save",
            match: "EXACT",
            first: true,
          },
        },
      },
      {
        method: "SetValue",
        request: {
          sessionId: "session-1",
          locator: {kind: "TEST_ID", value: "name", match: "EXACT", first: false},
          valueJson: JSON.stringify("Ada"),
        },
      },
      {
        method: "Evaluate",
        request: {sessionId: "session-1", expression: "window.appState", argumentsJson: "[]", invoke: false},
      },
      {method: "CloseSession", request: {sessionId: "session-1"}},
    ]);
  });

  it("expresses every pilot locator and assertion as one RPC", async () => {
    browser = await connectClient();
    const session = await browser.openSession();

    const results: AssertionResult[] = [
      await session.locator("main").expectVisible(),
      await session.getByText("Saved", {exact: false}).expectText("Saved", {exact: true, timeoutMs: 50}),
      await session.locator("article").expectCount(3),
      await session.getByTestId("save").expectAttribute("aria-busy", "false"),
      await session.locator("input").expectValue("Ada"),
      await session.expectUrl("/bookings", {pathname: true}),
      await session.expectEvaluation("window.ready", true),
    ];

    expect(results.every((result) => result.attemptCount === 2)).toBe(true);
    expect(results.every((result) => result.rpcRequestCount === 1 && result.rpcResponseCount === 1)).toBe(true);
    expect(requests.filter(({method}) => method === "Assert")).toHaveLength(7);
    expect(requests.at(-1)).toMatchObject({
      method: "Assert",
      request: {
        sessionId: "session-1",
        assertion: {
          kind: "EVALUATE",
          expression: "window.ready",
          expectedJson: "true",
        },
      },
    });
  });

  it("hands out an independent session per openSession call", async () => {
    browser = await connectClient();

    const first = await browser.openSession();
    const second = await browser.openSession();

    expect(first).not.toBe(second);
    expect(first.id).not.toBe(second.id);
    expect(requests.filter(({method}) => method === "OpenSession")).toHaveLength(2);

    await first.close();
    expect(requests.at(-1)).toMatchObject({method: "CloseSession", request: {sessionId: first.id}});

    // Closing one session leaves its sibling usable - a spec that opens two to prove browser-context
    // isolation must not silently be talking to one.
    await second.navigate("http://localhost/other");
    expect(requests.at(-1)).toMatchObject({method: "Navigate", request: {sessionId: second.id}});

    await browser.close();
    browser = undefined;
    expect(requests.filter(({method}) => method === "CloseSession")).toHaveLength(2);
  });

  it("says whether an evaluation is a function call rather than leaving the daemon to guess", async () => {
    browser = await connectClient();
    const session = await browser.openSession();

    // No args array at all: read the expression.
    await session.evaluate("window.appState");
    expect(requests.at(-1)).toMatchObject({method: "Evaluate", request: {invoke: false}});

    // An empty args array is still a call - the daemon's pre-invoke inference read this one back as
    // the function's source instead of running it.
    await session.evaluate("() => 41 + 1", []);
    expect(requests.at(-1)).toMatchObject({
      method: "Evaluate",
      request: {expression: "() => 41 + 1", argumentsJson: "[]", invoke: true},
    });

    await session.evaluate("(a) => a + 1", [41]);
    expect(requests.at(-1)).toMatchObject({method: "Evaluate", request: {argumentsJson: "[41]", invoke: true}});
  });

  it("accepts cookie expiry timestamps returned by the SIV sign-in helper", async () => {
    browser = await connectClient();
    const session = await browser.openSession();

    await session.setCookies([{name: "session", value: "abc", expires: 1_800_000_000}]);

    expect(requests.at(-1)).toMatchObject({
      method: "SetCookies",
      request: {
        cookies: [{name: "session", value: "abc", expiresUnix: 1_800_000_000}],
      },
    });
  });

  it("turns a structured assertion mismatch into a BilobaError at the TypeScript callsite", async () => {
    assertImplementation = (_request, respond) => {
      respond(operationResult({
        matched: false,
        observedJson: JSON.stringify("Saving"),
        trajectory: [{attempt: 1, elapsedMs: 0, observedJson: JSON.stringify("Saving"), retryReason: "text mismatch"}],
        diagnostics: {
          locator: "getByText(Saved)",
          expected: "Saved",
          domOutline: "body\n  button Saving",
          screenshotPath: "/tmp/failure.png",
          daemonDetail: "timed out after 50ms",
        },
      }));
    };
    browser = await connectClient();
    const session = await browser.openSession();

    const invokeFromTest = () => session.getByText("Saved").expectText("Saved", {timeoutMs: 50});
    const error = await invokeFromTest().catch((reason: unknown) => reason);

    // the contract every timed-out assertion owes, stub or real daemon
    expectTimedOutAssertion(error, {locator: "getByText(Saved)", expected: "Saved"});
    expect(error.message).toContain("last observed: \"Saving\"");
    expect(error.message).toContain("attempts: 1");
    expect(error).toMatchObject({
      code: "TIMEOUT",
      locator: "getByText(Saved)",
      expected: "Saved",
      observed: "Saving",
      domOutline: "body\n  button Saving",
      screenshotPath: "/tmp/failure.png",
      daemonDetail: "timed out after 50ms",
      rpcRequestCount: 1,
      rpcResponseCount: 1,
    });
    expect((error as Error).stack).toContain("invokeFromTest");
    expect((error as BilobaError).trajectory).toEqual([
      {attempt: 1, elapsedMs: 0, observed: "Saving", retryReason: "text mismatch"},
    ]);
  });

  it("rejects a timed-out action instead of silently accepting a successful RPC envelope", async () => {
    clickImplementation = (_request, respond) => respond(operationResult({
      matched: false,
      attemptCount: 3,
      // The daemon builds this from engine.Poll's attempts, which always records at least one, so
      // a timed-out operation without a trajectory is a shape it cannot produce.  The stub used to
      // claim exactly that; the shared contract in support/assertions.ts caught it.
      trajectory: [
        {attempt: 1, elapsedMs: 0},
        {attempt: 2, elapsedMs: 7},
        {attempt: 3, elapsedMs: 14},
      ],
      diagnostics: {
        locator: `locator("#missing")`,
        expected: "operation to succeed",
        daemonDetail: "poll: context deadline exceeded",
      },
    }));
    browser = await connectClient();
    const session = await browser.openSession();

    const invokeFromTest = () => session.locator("#missing").click({timeoutMs: 20});
    const error = await invokeFromTest().catch((reason: unknown) => reason);

    expectTimedOutAction(error, {locator: `locator("#missing")`, operation: "click"});
    expect(error.stack).toContain("invokeFromTest");
  });

  it("cancels the in-flight assertion when its AbortSignal aborts", async () => {
    let observeStart!: () => void;
    let observeCancellation!: () => void;
    const started = new Promise<void>((resolve) => {
      observeStart = resolve;
    });
    const cancelled = new Promise<void>((resolve) => {
      observeCancellation = resolve;
    });
    assertImplementation = () => {
      observeStart();
    };
    observeCancel = observeCancellation;
    browser = await connectClient();
    const session = await browser.openSession();
    const controller = new AbortController();

    const assertion = session.locator("main").expectVisible({signal: controller.signal});
    await started;
    controller.abort("worker stopped");

    await expect(assertion).rejects.toMatchObject({code: "CANCELLED"});
    await cancelled;
  });

  it("uses the assertion timeout as the request deadline and keeps the test callsite", async () => {
    assertImplementation = () => {};
    browser = await connectClient();
    const session = await browser.openSession();

    const invokeTimedAssertion = () => session.locator("main").expectVisible({timeoutMs: 20});
    const error = await invokeTimedAssertion().catch((reason: unknown) => reason);

    expect(error).toMatchObject({code: "TIMEOUT"});
    expect((error as Error).stack).toContain("invokeTimedAssertion");
  });
});

describe("published entry point", () => {
  it("exposes only the public API, keeping internal seams out of dist/index.js", async () => {
    // tsconfig.build.json's stripInternal deletes an `@internal` export from the .d.ts but not from
    // the emitted JS, so anything exported here ships as an untyped escape hatch.  Internal modules
    // are unreachable through the package's "exports" map, which is where seams belong.
    const api: Record<string, unknown> = await import("../src/index.js");
    expect(Object.keys(api).sort()).toEqual(["BilobaError", "connect", "startDaemon", "startSharedBrowser"]);
  });
});

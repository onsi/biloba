import type {
  Assertion as WireAssertion,
  Locator as WireLocator,
  LocatorRequest,
  OperationResult as StdioOperationResult,
  SetValueRequest,
} from "../generated/protocol.js";
import {
  BilobaError,
  type AssertionResult,
  type Browser,
  type ConnectOptions,
  type Cookie,
  type Locator,
  type SerializableValue,
  type Session,
  type WaitOptions,
} from "../index.js";
import {StdioTransport} from "./stdio-transport.js";

type DriverTransport = StdioTransport;
type DriverOperationResult = StdioOperationResult;

class ClientBrowser implements Browser {
  readonly #transport: DriverTransport;
  readonly #sessions = new Set<ClientSession>();
  readonly #stopDaemon?: () => Promise<void>;
  readonly protocolVersion: string;
  readonly capabilities: ReadonlySet<string>;
  #closed = false;

  constructor(
    transport: DriverTransport,
    protocolVersion: string,
    capabilities: readonly string[],
    stopDaemon?: () => Promise<void>,
  ) {
    this.#transport = transport;
    this.protocolVersion = protocolVersion;
    this.capabilities = new Set(capabilities);
    if (stopDaemon) this.#stopDaemon = stopDaemon;
  }

  // Every call opens a genuinely new daemon session with its own browser context.  Sessions are
  // tracked only so close() can reap the ones a suite forgot - reusing one per worker would make
  // any spec that opens two sessions to prove isolation pass vacuously, and a client that Ginkgo,
  // vitest or anything else can drive has no business reading VITEST_POOL_ID.
  async openSession(): Promise<Session> {
    this.#assertOpen();
    const response = await this.#transport.openSession({});
    const session: ClientSession = new ClientSession(
      response.sessionId ?? "",
      this.#transport,
      () => this.#sessions.delete(session),
    );
    this.#sessions.add(session);
    return session;
  }

  async close(): Promise<void> {
    if (this.#closed) return;
    this.#closed = true;
    await Promise.allSettled([...this.#sessions].map(async (session) => session.close()));
    this.#sessions.clear();
    this.#transport.close();
    await this.#stopDaemon?.();
  }

  #assertOpen(): void {
    if (this.#closed) throw new BilobaError({code: "DRIVER_CLOSED", message: "Biloba browser is closed"});
  }
}

class ClientSession implements Session {
  readonly id: string;
  readonly #transport: DriverTransport;
  readonly #onClose: () => void;
  closed = false;

  constructor(id: string, transport: DriverTransport, onClose: () => void) {
    this.id = id;
    this.#transport = transport;
    this.#onClose = onClose;
  }

  async prepare(): Promise<void> {
    this.#assertOpen();
    await this.#transport.prepareSession({sessionId: this.id});
  }

  async navigate(url: string, options: WaitOptions = {}): Promise<void> {
    this.#assertOpen();
    await this.#transport.navigate({sessionId: this.id, url}, options);
  }

  async setCookies(cookies: readonly Cookie[]): Promise<void> {
    this.#assertOpen();
    await this.#transport.setCookies({
      sessionId: this.id,
      cookies: cookies.map(({expires, ...cookie}) => ({
        ...cookie,
        ...(expires !== undefined && {
          expiresUnix: typeof expires === "number" ? expires : expires.getTime() / 1_000,
        }),
      })),
    });
  }

  // invoke tells the daemon what expression means instead of letting it infer that from the
  // argument count: passing an args array - even an empty one - is the caller saying "call this",
  // so evaluate("(a) => a + 1", []) runs the function while evaluate("window.appState") reads the
  // expression.  Without it the daemon falls back to the pre-invoke inference, where an empty array
  // silently turned a function call back into a function-source read.
  async evaluate<T = unknown>(
    expression: string,
    args?: readonly SerializableValue[],
    options: WaitOptions = {},
  ): Promise<T> {
    this.#assertOpen();
    const response = await this.#transport.evaluate({
      sessionId: this.id,
      expression,
      argumentsJson: JSON.stringify(args ?? []),
      invoke: args !== undefined,
    }, options);
    return parseJson(response.observedJson) as T;
  }

  async close(): Promise<void> {
    if (this.closed) return;
    this.closed = true;
    try {
      await this.#transport.closeSession({sessionId: this.id});
    } finally {
      this.#onClose();
    }
  }

  locator(css: string): Locator {
    return new ClientLocator(this, {kind: "CSS", value: css, match: "EXACT", first: false});
  }

  getByTestId(value: string): Locator {
    return new ClientLocator(this, {kind: "TEST_ID", value, match: "EXACT", first: false});
  }

  getByText(value: string, options: {exact?: boolean} = {}): Locator {
    return new ClientLocator(this, {
      kind: "TEXT",
      value,
      match: options.exact === true ? "EXACT" : "CONTAINS",
      first: false,
    });
  }

  getByRole(role: string, options: {name?: string; exact?: boolean} = {}): Locator {
    return new ClientLocator(this, {
      kind: "ROLE",
      role,
      ...(options.name !== undefined && {name: options.name}),
      match: options.exact === false ? "CONTAINS" : "EXACT",
      first: false,
    });
  }

  async expectUrl(
    expected: string,
    options: WaitOptions & {exact?: boolean; pathname?: boolean} = {},
  ): Promise<AssertionResult> {
    const callsiteStack = new Error().stack;
    if (options.pathname) {
      return await this.assert({
        kind: "EVALUATE",
        expression: "window.location.pathname",
        expectedJson: JSON.stringify(expected),
      }, options, callsiteStack);
    }
    return await this.assert({
      kind: "URL",
      expectedString: expected,
      match: matchMode(options.exact),
    }, options, callsiteStack);
  }

  async expectEvaluation(
    expression: string,
    expected: SerializableValue,
    options: WaitOptions = {},
  ): Promise<AssertionResult> {
    const callsiteStack = new Error().stack;
    return await this.assert({
      kind: "EVALUATE",
      expression,
      expectedJson: JSON.stringify(expected),
    }, options, callsiteStack);
  }

  async assert(
    assertion: WireAssertion,
    options: WaitOptions,
    callsiteStack: string | undefined,
  ): Promise<AssertionResult> {
    this.#assertOpen();
    try {
      const response = await this.#transport.assert({
        sessionId: this.id,
        assertion,
        poll: pollPolicy(options),
      }, {
        ...options,
        ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100}),
      });
      return assertionResult(response, callsiteStack);
    } catch (error) {
      if (error instanceof BilobaError && callsiteStack && !error.stack?.includes(stripStackHeader(callsiteStack))) {
        error.stack = `${error.name}: ${error.message}\n${stripStackHeader(callsiteStack)}`;
      }
      throw error;
    }
  }

  async action(
    method: "click" | "setValue",
    locator: WireLocator,
    payload: Record<string, unknown>,
    options: WaitOptions,
  ): Promise<void> {
    this.#assertOpen();
    const callsiteStack = new Error().stack;
    const request = {
      sessionId: this.id,
      locator,
      ...payload,
      poll: pollPolicy(options),
    };
    const transportOptions = {
      ...options,
      ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100}),
    };
    const response = method === "click"
      ? await this.#transport.click(request as LocatorRequest, transportOptions)
      : await this.#transport.setValue(request as unknown as SetValueRequest, transportOptions);
    operationResult(response, `${method} operation`, callsiteStack);
  }

  #assertOpen(): void {
    if (this.closed) throw new BilobaError({code: "DRIVER_CLOSED", message: `Biloba session ${this.id} is closed`});
  }
}

class ClientLocator implements Locator {
  readonly #session: ClientSession;
  readonly #locator: WireLocator;

  constructor(session: ClientSession, locator: WireLocator) {
    this.#session = session;
    this.#locator = locator;
  }

  first(): Locator {
    return new ClientLocator(this.#session, {...this.#locator, first: true});
  }

  async click(options: WaitOptions = {}): Promise<void> {
    await this.#session.action("click", this.#locator, {}, options);
  }

  async setValue(value: SerializableValue, options: WaitOptions = {}): Promise<void> {
    await this.#session.action("setValue", this.#locator, {valueJson: JSON.stringify(value)}, options);
  }

  async expectVisible(options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#assert({kind: "VISIBLE", locator: this.#locator}, options);
  }

  async expectText(
    expected: string,
    options: WaitOptions & {exact?: boolean} = {},
  ): Promise<AssertionResult> {
    return await this.#assert({
      kind: "TEXT",
      locator: this.#locator,
      expectedString: expected,
      match: matchMode(options.exact),
    }, options);
  }

  async expectCount(expected: number, options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#assert({kind: "COUNT", locator: this.#locator, expectedCount: expected}, options);
  }

  async expectAttribute(
    name: string,
    expected: string,
    options: WaitOptions & {exact?: boolean} = {},
  ): Promise<AssertionResult> {
    return await this.#assert({
      kind: "ATTRIBUTE",
      locator: this.#locator,
      attribute: name,
      expectedString: expected,
      match: matchMode(options.exact),
    }, options);
  }

  async expectValue(expected: SerializableValue, options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#assert({kind: "VALUE", locator: this.#locator, expectedJson: JSON.stringify(expected)}, options);
  }

  async #assert(assertion: WireAssertion, options: WaitOptions): Promise<AssertionResult> {
    const callsiteStack = new Error().stack;
    return await this.#session.assert(assertion, options, callsiteStack);
  }
}

// The seam test/client.test.ts uses to drive the client against an in-memory framed peer.  It lives
// here rather than on the public entry point: exporting it there and marking it internal only makes
// stripInternal delete it from dist/index.d.ts, while dist/index.js goes on exposing it - an untyped
// escape hatch in the published surface.  Internal modules are outside the package's "exports" map,
// so this stays reachable from the repo's own tests and from nowhere else.
export async function connectWithTransport(
  transport: StdioTransport,
  options: Pick<ConnectOptions, "signal"> = {},
  stopDaemon?: () => Promise<void>,
): Promise<Browser> {
  try {
    const handshake = await transport.handshake(
      {protocolVersion: "1"},
      options.signal ? {signal: options.signal} : {},
    );
    if (handshake.protocolVersion !== "1") {
      throw new BilobaError({
        code: "PROTOCOL_MISMATCH",
        message: `Biloba protocol mismatch: client 1, daemon ${handshake.protocolVersion}`,
      });
    }
    return new ClientBrowser(
      transport,
      handshake.protocolVersion ?? "",
      handshake.capabilities ?? [],
      stopDaemon,
    );
  } catch (error) {
    transport.close();
    await stopDaemon?.();
    throw error;
  }
}

function assertionResult(response: DriverOperationResult, callsiteStack: string | undefined): AssertionResult {
  const observed = parseJson(response.observedJson);
  const trajectory = (response.trajectory ?? []).map((entry) => ({
    attempt: entry.attempt ?? 0,
    elapsedMs: Number(entry.elapsedMs ?? 0),
    observed: parseJson(entry.observedJson),
    ...(entry.retryReason && {retryReason: entry.retryReason}),
  }));
  if (!response.matched) {
    const diagnostics = response.diagnostics ?? {};
    const message = [
      "Biloba assertion timed out",
      diagnostics.locator ? `locator: ${diagnostics.locator}` : undefined,
      diagnostics.expected ? `expected: ${diagnostics.expected}` : undefined,
      `last observed: ${JSON.stringify(observed)}`,
      `attempts: ${response.attemptCount ?? trajectory.length}`,
    ].filter((line): line is string => line !== undefined).join("\n");
    throw new BilobaError({
      code: "TIMEOUT",
      message,
      ...(diagnostics.locator && {locator: diagnostics.locator}),
      ...(diagnostics.expected && {expected: diagnostics.expected}),
      observed,
      trajectory,
      ...(diagnostics.domOutline && {domOutline: diagnostics.domOutline}),
      ...(diagnostics.screenshotPath && {screenshotPath: diagnostics.screenshotPath}),
      ...(diagnostics.daemonDetail && {daemonDetail: diagnostics.daemonDetail}),
      rpcRequestCount: response.rpcRequestCount ?? 0,
      rpcResponseCount: response.rpcResponseCount ?? 0,
      ...(callsiteStack && {callsiteStack}),
    });
  }
  return {
    observed,
    attemptCount: response.attemptCount || trajectory.length,
    trajectory,
    rpcRequestCount: response.rpcRequestCount ?? 0,
    rpcResponseCount: response.rpcResponseCount ?? 0,
    ...(response.timings && {elapsedMs: Number(response.timings.elapsedMs ?? 0)}),
  };
}

function operationResult(
  response: DriverOperationResult,
  operation: string,
  callsiteStack: string | undefined,
): void {
  if (response.matched) return;
  const diagnostics = response.diagnostics ?? {};
  const observed = parseJson(response.observedJson);
  const trajectory = (response.trajectory ?? []).map((entry) => ({
    attempt: entry.attempt ?? 0,
    elapsedMs: Number(entry.elapsedMs ?? 0),
    observed: parseJson(entry.observedJson),
    ...(entry.retryReason && {retryReason: entry.retryReason}),
  }));
  throw new BilobaError({
    code: "TIMEOUT",
    message: [
      `Biloba ${operation} timed out`,
      diagnostics.locator ? `locator: ${diagnostics.locator}` : undefined,
      `attempts: ${response.attemptCount ?? trajectory.length}`,
    ].filter((line): line is string => line !== undefined).join("\n"),
    ...(diagnostics.locator && {locator: diagnostics.locator}),
    ...(diagnostics.expected && {expected: diagnostics.expected}),
    observed,
    trajectory,
    ...(diagnostics.domOutline && {domOutline: diagnostics.domOutline}),
    ...(diagnostics.screenshotPath && {screenshotPath: diagnostics.screenshotPath}),
    ...(diagnostics.daemonDetail && {daemonDetail: diagnostics.daemonDetail}),
    rpcRequestCount: response.rpcRequestCount ?? 0,
    rpcResponseCount: response.rpcResponseCount ?? 0,
    ...(callsiteStack && {callsiteStack}),
  });
}

function pollPolicy(options: WaitOptions): Record<string, number> {
  return {
    ...(options.timeoutMs !== undefined && {timeoutMs: options.timeoutMs}),
    ...(options.intervalMs !== undefined && {intervalMs: options.intervalMs}),
  };
}

function matchMode(exact: boolean | undefined): "EXACT" | "CONTAINS" {
  return exact === false ? "CONTAINS" : "EXACT";
}

function parseJson(value: string | undefined): unknown {
  if (!value) return undefined;
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return value;
  }
}

// index.ts's BilobaError re-homes a failure onto the caller's stack with this too.
export function stripStackHeader(stack: string): string {
  const newline = stack.indexOf("\n");
  return newline === -1 ? stack : stack.slice(newline + 1);
}

import type {
  Assertion as WireAssertion,
  DragToRequest,
  Expectation as WireExpectation,
  Locator as WireLocator,
  LocatorRequest,
  OperationResult as StdioOperationResult,
  SetValueRequest,
  SetUploadRequest,
  TypeRequest,
} from "../generated/protocol.js";
import {
  BilobaError,
  type AssertionResult,
  type Browser,
  type ConnectOptions,
  type Cookie,
  type Expectation,
  type ExpectedValue,
  type HeldResponse,
  type Locator,
  type ResponseHold,
  type SerializableValue,
  type Session,
  type WaitOptions,
  type XPathExpression,
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
    return this.#trackSession(response.sessionId ?? "");
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

  #trackSession(id: string): ClientSession {
    let session: ClientSession;
    session = new ClientSession(
      id,
      this.#transport,
      () => this.#sessions.delete(session),
      (siblingId) => this.#trackSession(siblingId),
    );
    this.#sessions.add(session);
    return session;
  }
}

class ClientSession implements Session {
  readonly id: string;
  readonly #transport: DriverTransport;
  readonly #onClose: () => void;
  readonly #registerSibling: (id: string) => ClientSession;
  closed = false;

  constructor(
    id: string,
    transport: DriverTransport,
    onClose: () => void,
    registerSibling: (id: string) => ClientSession,
  ) {
    this.id = id;
    this.#transport = transport;
    this.#onClose = onClose;
    this.#registerSibling = registerSibling;
  }

  async newTab(): Promise<Session> {
    this.#assertOpen();
    const response = await this.#transport.newTab({sessionId: this.id});
    return this.#registerSibling(response.sessionId ?? "");
  }

  async addInitScript(script: string, options: WaitOptions = {}): Promise<void> {
    this.#assertOpen();
    const response = await this.#transport.addInitScript({sessionId: this.id, script}, options);
    operationResult(response, "add init script operation", new Error().stack);
  }

  async activate(options: WaitOptions = {}): Promise<void> {
    this.#assertOpen();
    const response = await this.#transport.activate({sessionId: this.id}, options);
    operationResult(response, "activate tab operation", new Error().stack);
  }

  async prepare(): Promise<void> {
    this.#assertOpen();
    await this.#transport.prepareSession({sessionId: this.id});
  }

  async navigate(url: string, options: WaitOptions = {}): Promise<void> {
    this.#assertOpen();
    await this.#transport.navigate({sessionId: this.id, url}, options);
  }

  // Mirrors the Go runner's NavigateWithStatus.  navigate() asserting 200 is what makes a broken
  // fixture fail at the navigation rather than as a baffling assertion three lines later; this is
  // the way to say the 4xx/5xx page is the page you meant to load.
  async navigateWithStatus(url: string, expectedStatus: number, options: WaitOptions = {}): Promise<void> {
    this.#assertOpen();
    await this.#transport.navigate({sessionId: this.id, url, expectedStatus}, options);
  }

  async sendKeys(keys: string, options: WaitOptions = {}): Promise<void> {
    this.#assertOpen();
    const response = await this.#transport.sendKeys({sessionId: this.id, keys}, options);
    operationResult(response, "send keys operation", new Error().stack);
  }

  async setCookies(cookies: readonly Cookie[]): Promise<void> {
    this.#assertOpen();
    await this.#transport.setCookies({
      sessionId: this.id,
      // The public Cookie lets a caller pass an explicit undefined for any optional member; the
      // generated wire type does not.  Spreading each field only when it is set is what keeps that
      // convenience from reaching the protocol - and omitted vs. present-and-undefined encode
      // identically anyway, since JSON.stringify drops both.
      cookies: cookies.map(({expires, ...cookie}) => ({
        name: cookie.name,
        value: cookie.value,
        ...(cookie.domain !== undefined && {domain: cookie.domain}),
        ...(cookie.path !== undefined && {path: cookie.path}),
        ...(cookie.secure !== undefined && {secure: cookie.secure}),
        ...(cookie.httpOnly !== undefined && {httpOnly: cookie.httpOnly}),
        ...(cookie.sameSite !== undefined && {sameSite: cookie.sameSite}),
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

  async evaluateAsync<T = unknown>(
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
      awaitPromise: true,
    }, options);
    return parseJson(response.observedJson) as T;
  }

  async setWindowSize(width: number, height: number, options: WaitOptions = {}): Promise<void> {
    this.#assertOpen();
    const response = await this.#transport.setWindowSize({sessionId: this.id, width, height}, options);
    operationResult(response, "set window size operation", new Error().stack);
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

  xpath(expression: string | XPathExpression): Locator {
    return new ClientLocator(this, {kind: "XPATH", value: expression.toString(), match: "EXACT", first: false});
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
    expected: ExpectedValue,
    options: WaitOptions & {exact?: boolean; pathname?: boolean} = {},
  ): Promise<AssertionResult> {
    const callsiteStack = new Error().stack;
    if (options.pathname) {
      return await this.assert({
        kind: "EVALUATE",
        expression: "window.location.pathname",
        expectation: wireExpectation(expected),
      }, options, callsiteStack);
    }
    return await this.assert({
      kind: "URL",
      expectation: wireExpectation(expected, options.exact),
    }, options, callsiteStack);
  }

  async expectEvaluation(
    expression: string,
    expected: ExpectedValue,
    options: WaitOptions = {},
  ): Promise<AssertionResult> {
    const callsiteStack = new Error().stack;
    return await this.assert({
      kind: "EVALUATE",
      expression,
      expectation: wireExpectation(expected),
    }, options, callsiteStack);
  }

  async expectRequest(
    expectedUrl: ExpectedValue,
    options: WaitOptions & {method?: string} = {},
  ): Promise<AssertionResult> {
    const callsiteStack = new Error().stack;
    return await this.assert({
      kind: "REQUEST",
      ...(options.method !== undefined && {method: options.method}),
      expectation: wireExpectation(expectedUrl),
    }, options, callsiteStack);
  }

  async holdResponse(expectedUrl: ExpectedValue, options: WaitOptions = {}): Promise<ResponseHold> {
    this.#assertOpen();
    const response = await this.#transport.holdResponse({
      sessionId: this.id,
      expectation: wireExpectation(expectedUrl),
    }, options);
    const observed = parseJson(response.observedJson) as {holdId?: string};
    if (!observed.holdId) {
      throw new BilobaError({code: "DRIVER_ERROR", message: "hold response did not return a hold ID"});
    }
    return new ClientResponseHold(this.id, observed.holdId, this.#transport);
  }

  async url(): Promise<string> {
    const result = await this.assert({
      kind: "URL",
      expectation: {kind: "ANYTHING"},
    }, {mode: "immediate"}, new Error().stack);
    return result.observed as string;
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
    method: "click" | "setValue" | "type",
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
      : method === "setValue"
        ? await this.#transport.setValue(request as unknown as SetValueRequest, transportOptions)
        : await this.#transport.type(request as unknown as TypeRequest, transportOptions);
    operationResult(response, `${method} operation`, callsiteStack);
  }

  async setUpload(
    locator: WireLocator,
    paths: readonly string[],
    options: WaitOptions,
  ): Promise<DriverOperationResult> {
    this.#assertOpen();
    return await this.#transport.setUpload({
      sessionId: this.id,
      locator,
      paths: [...paths],
      poll: pollPolicy(options),
    } satisfies SetUploadRequest, {
      ...options,
      ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100}),
    });
  }

  async dragTo(
    source: WireLocator,
    target: WireLocator,
    options: WaitOptions,
  ): Promise<DriverOperationResult> {
    this.#assertOpen();
    return await this.#transport.dragTo({
      sessionId: this.id,
      source,
      target,
      poll: pollPolicy(options),
    } satisfies DragToRequest, {
      ...options,
      ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100}),
    });
  }

  #assertOpen(): void {
    if (this.closed) throw new BilobaError({code: "DRIVER_CLOSED", message: `Biloba session ${this.id} is closed`});
  }
}

class ClientResponseHold implements ResponseHold {
  constructor(
    private readonly sessionId: string,
    private readonly holdId: string,
    private readonly transport: DriverTransport,
  ) {}

  async await(options: WaitOptions = {}): Promise<HeldResponse> {
    const response = await this.transport.awaitResponseHold({sessionId: this.sessionId, holdId: this.holdId}, options);
    return parseJson(response.observedJson) as HeldResponse;
  }

  async release(options: WaitOptions = {}): Promise<void> {
    const response = await this.transport.releaseResponseHold({sessionId: this.sessionId, holdId: this.holdId}, options);
    operationResult(response, "release response hold operation", new Error().stack);
  }
}

class ClientLocator implements Locator {
  readonly #session: ClientSession;
  readonly #locator: WireLocator;
  readonly #realistic: boolean;

  constructor(session: ClientSession, locator: WireLocator, realistic = false) {
    this.#session = session;
    this.#locator = locator;
    this.#realistic = realistic;
  }

  realistic(): Locator {
    return new ClientLocator(this.#session, this.#locator, true);
  }

  first(): Locator {
    return new ClientLocator(this.#session, {...this.#locator, first: true}, this.#realistic);
  }

  last(): Locator {
    return new ClientLocator(this.#session, {...this.#locator, first: false, nth: -1, nthSet: true}, this.#realistic);
  }

  nth(index: number): Locator {
    if (!Number.isInteger(index)) {
      throw new BilobaError({code: "INVALID_ARGUMENT", message: "locator.nth requires an integer index"});
    }
    return new ClientLocator(this.#session, {...this.#locator, first: false, nth: index, nthSet: true}, this.#realistic);
  }

  and(other: Locator | string): Locator {
    return this.#combine("AND", other);
  }

  or(other: Locator | string): Locator {
    return this.#combine("OR", other);
  }

  within(scope: Locator | string): Locator {
    return new ClientLocator(this.#session, {...this.#locator, within: wireLocator(scope)}, this.#realistic);
  }

  notWithin(scope: Locator | string): Locator {
    return this.#withFilter({kind: "WITHIN", selector: wireLocator(scope), negate: true});
  }

  filter(options: {
    hasText?: string;
    notHasText?: string;
    has?: Locator | string;
    notHas?: Locator | string;
  }): Locator {
    let locator: ClientLocator = this;
    if (options.hasText !== undefined) {
      locator = locator.#withFilter({kind: "CONTAINS_TEXT", value: options.hasText, match: "CONTAINS"});
    }
    if (options.notHasText !== undefined) {
      locator = locator.#withFilter({kind: "CONTAINS_TEXT", value: options.notHasText, match: "CONTAINS", negate: true});
    }
    if (options.has !== undefined) {
      locator = locator.#withFilter({kind: "CONTAINS", selector: wireLocator(options.has)});
    }
    if (options.notHas !== undefined) {
      locator = locator.#withFilter({kind: "CONTAINS", selector: wireLocator(options.notHas), negate: true});
    }
    return locator;
  }

  async click(options: WaitOptions = {}): Promise<void> {
    await this.#session.action("click", this.#locator, this.#realistic ? {realistic: true} : {}, options);
  }

  async setValue(value: SerializableValue, options: WaitOptions = {}): Promise<void> {
    await this.#session.action("setValue", this.#locator, {
      valueJson: JSON.stringify(value),
      ...(this.#realistic && {realistic: true}),
    }, options);
  }

  async type(keys: string, options: WaitOptions = {}): Promise<void> {
    await this.#session.action("type", this.#locator, {
      keys,
      ...(this.#realistic && {realistic: true}),
    }, options);
  }

  async setUploadFiles(paths: readonly string[], options: WaitOptions = {}): Promise<void> {
    const callsiteStack = new Error().stack;
    const response = await this.#session.setUpload(this.#locator, paths, options);
    operationResult(response, "set upload operation", callsiteStack);
  }

  async dragTo(target: Locator | string, options: WaitOptions = {}): Promise<void> {
    const callsiteStack = new Error().stack;
    const response = await this.#session.dragTo(this.#locator, wireLocator(target), options);
    operationResult(response, "drag to operation", callsiteStack);
  }

  async expectVisible(options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#booleanAssertion("VISIBLE", false, options);
  }

  async expectNotVisible(options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#booleanAssertion("VISIBLE", true, options);
  }

  async expectExists(options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#booleanAssertion("EXISTS", false, options);
  }

  async expectNotExists(options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#booleanAssertion("EXISTS", true, options);
  }

  async expectEnabled(options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#booleanAssertion("ENABLED", false, options);
  }

  async expectNotEnabled(options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#booleanAssertion("ENABLED", true, options);
  }

  async expectClickable(options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#booleanAssertion("CLICKABLE", false, options);
  }

  async expectNotClickable(options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#booleanAssertion("CLICKABLE", true, options);
  }

  async expectText(
    expected: ExpectedValue,
    options: WaitOptions & {exact?: boolean} = {},
  ): Promise<AssertionResult> {
    return await this.#assert({
      kind: "TEXT",
      locator: this.#locator,
      expectation: wireExpectation(expected, options.exact),
    }, options);
  }

  async expectNotText(
    expected: ExpectedValue,
    options: WaitOptions & {exact?: boolean} = {},
  ): Promise<AssertionResult> {
    return await this.#assert({
      kind: "TEXT",
      locator: this.#locator,
      expectation: negate(wireExpectation(expected, options.exact)),
    }, options);
  }

  async expectCount(expected: number | Expectation, options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#assert({kind: "COUNT", locator: this.#locator, expectation: wireExpectation(expected)}, options);
  }

  async expectAttribute(
    name: string,
    expected: ExpectedValue,
    options: WaitOptions & {exact?: boolean} = {},
  ): Promise<AssertionResult> {
    return await this.#assert({
      kind: "ATTRIBUTE",
      locator: this.#locator,
      attribute: name,
      expectation: wireExpectation(expected, options.exact),
    }, options);
  }

  async expectNotAttribute(
    name: string,
    expected: ExpectedValue,
    options: WaitOptions & {exact?: boolean} = {},
  ): Promise<AssertionResult> {
    return await this.#assert({
      kind: "ATTRIBUTE",
      locator: this.#locator,
      attribute: name,
      expectation: negate(wireExpectation(expected, options.exact)),
    }, options);
  }

  async expectProperty(
    name: string,
    expected: ExpectedValue,
    options: WaitOptions & {exact?: boolean} = {},
  ): Promise<AssertionResult> {
    return await this.#assert({
      kind: "PROPERTY",
      locator: this.#locator,
      property: name,
      expectation: wireExpectation(expected, options.exact),
    }, options);
  }

  async expectValue(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#assert({kind: "VALUE", locator: this.#locator, expectation: wireExpectation(expected)}, options);
  }

  async expectAllText(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#assert({kind: "ALL_TEXT", locator: this.#locator, expectation: wireExpectation(expected)}, options);
  }

  async text(): Promise<string> {
    return await this.#read<string>({kind: "TEXT", locator: this.#locator});
  }

  async count(): Promise<number> {
    return await this.#read<number>({kind: "COUNT", locator: this.#locator});
  }

  async getAttribute(name: string): Promise<string | null> {
    return await this.#read<string | null>({kind: "ATTRIBUTE", locator: this.#locator, attribute: name});
  }

  async getProperty<T = unknown>(name: string): Promise<T> {
    return await this.#read<T>({kind: "PROPERTY", locator: this.#locator, property: name});
  }

  async value<T = unknown>(): Promise<T> {
    return await this.#read<T>({kind: "VALUE", locator: this.#locator});
  }

  async exists(): Promise<boolean> {
    return await this.#read<boolean>({kind: "EXISTS", locator: this.#locator});
  }

  async #assert(assertion: WireAssertion, options: WaitOptions): Promise<AssertionResult> {
    const callsiteStack = new Error().stack;
    return await this.#session.assert(assertion, options, callsiteStack);
  }

  async #booleanAssertion(
    kind: "VISIBLE" | "EXISTS" | "ENABLED" | "CLICKABLE",
    negated: boolean,
    options: WaitOptions,
  ): Promise<AssertionResult> {
    const expected: WireExpectation = {kind: "EQUAL", expectedJson: "true"};
    return await this.#assert({
      kind,
      locator: this.#locator,
      expectation: negated ? negate(expected) : expected,
    }, options);
  }

  async #read<T>(assertion: WireAssertion): Promise<T> {
    const result = await this.#assert({...assertion, expectation: {kind: "ANYTHING"}}, {mode: "immediate"});
    return result.observed as T;
  }

  #combine(kind: "AND" | "OR", other: Locator | string): ClientLocator {
    return new ClientLocator(this.#session, {
      kind,
      operands: [this.#locator, wireLocator(other)],
      match: "EXACT",
      first: false,
    }, this.#realistic);
  }

  #withFilter(filter: NonNullable<WireLocator["filters"]>[number]): ClientLocator {
    return new ClientLocator(this.#session, {
      ...this.#locator,
      filters: [...(this.#locator.filters ?? []), filter],
    }, this.#realistic);
  }

  wireLocator(): WireLocator {
    return this.#locator;
  }
}

function wireLocator(locator: Locator | string): WireLocator {
  if (typeof locator === "string") {
    return {kind: "CSS", value: locator, match: "EXACT", first: false};
  }
  if (locator instanceof ClientLocator) return locator.wireLocator();
  throw new BilobaError({code: "INVALID_ARGUMENT", message: "locator belongs to a different Biloba client"});
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

function pollPolicy(options: WaitOptions): Record<string, number | string> {
  return {
    ...(options.timeoutMs !== undefined && {timeoutMs: options.timeoutMs}),
    ...(options.intervalMs !== undefined && {intervalMs: options.intervalMs}),
    ...(options.mode !== undefined && {mode: options.mode.toUpperCase()}),
  };
}

function wireExpectation(expected: ExpectedValue, exact: boolean | undefined = true): WireExpectation {
  if (expected instanceof RegExp) return {kind: "REGEXP", expectedJson: JSON.stringify(expected.source)};
  if (isExpectation(expected)) {
    switch (expected.kind) {
      case "equal": return {kind: "EQUAL", expectedJson: JSON.stringify(expected.expected)};
      case "contains": return {kind: "CONTAINS", expectedJson: JSON.stringify(expected.expected)};
      case "regexp": return {kind: "REGEXP", expectedJson: JSON.stringify(expected.expected)};
      case "prefix": return {kind: "PREFIX", expectedJson: JSON.stringify(expected.expected)};
      case "suffix": return {kind: "SUFFIX", expectedJson: JSON.stringify(expected.expected)};
      case "number": return {kind: "NUMBER", operator: expected.operator, expectedJson: JSON.stringify(expected.expected)};
      case "empty": return {kind: "EMPTY"};
      case "anything": return {kind: "ANYTHING"};
      case "all": return {kind: "ALL", children: expected.children.map((child) => wireExpectation(child))};
      case "any": return {kind: "ANY", children: expected.children.map((child) => wireExpectation(child))};
      case "not": return negate(wireExpectation(expected.child));
    }
  }
  if (typeof expected === "string" && exact === false) {
    return {kind: "CONTAINS", expectedJson: JSON.stringify(expected)};
  }
  return {kind: "EQUAL", expectedJson: JSON.stringify(expected)};
}

function isExpectation(value: ExpectedValue): value is Expectation {
  return typeof value === "object" && value !== null && !(value instanceof RegExp) && "kind" in value;
}

function negate(expectation: WireExpectation): WireExpectation {
  return {kind: "NOT", children: [expectation]};
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

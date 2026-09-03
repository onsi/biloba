import type {
  Assertion as WireAssertion,
  DragToRequest,
  DOMOperation as WireDOMOperation,
  DOMRequest,
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
  type ClickOptions,
  type CancellationOptions,
  type KeyboardOptions,
  type PointerOptions,
  type ScrollIntoViewOptions,
  type NameSpec,
  type GeometryRelation,
  type Box,
  type ScrollOffset,
  type Offset,
  type BoxPair,
  type BoxDelta,
  type DocumentOrder,
  type Expectation,
  type ExpectedValue,
  type HeldResponse,
  type Locator,
  type ResponseHold,
  type SerializableValue,
  type Session,
  type WaitOptions,
  type ValueLabel,
  type WindowKeyboardOptions,
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

  async sendKeys(keys: string, options: WindowKeyboardOptions = {}): Promise<void> {
    this.#assertOpen();
    assertWindowKeyboardOptions(options);
    const response = options.modifiers?.length
      ? await this.dom({kind: "SEND_KEYS", keys, modifiers: [...options.modifiers]}, {...options, mode: "immediate"})
      : await this.#transport.sendKeys({sessionId: this.id, keys}, options);
    operationResult(response, "send keys operation", new Error().stack);
  }

  async clearSelection(options: CancellationOptions = {}): Promise<void> {
    assertCancellationOptions(options, "clearSelection");
    operationResult(await this.dom({kind: "CLEAR_SELECTION"}, {...options, mode: "immediate"}), "clear selection operation", new Error().stack);
  }

  async normalizeColor(color: string, options: CancellationOptions = {}): Promise<string> {
    assertCancellationOptions(options, "normalizeColor");
    const response = await this.dom({kind: "NORMALIZE_COLOR", valueJson: color}, {...options, mode: "immediate"});
    operationResult(response, "normalize color operation", new Error().stack);
    return parseJson(response.observedJson) as string;
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

  getByTestId(value: string, options: {attribute?: string} = {}): Locator {
    return new ClientLocator(this, {kind: "TEST_ID", value, match: "EXACT", first: false, ...(options.attribute && {attribute: options.attribute})});
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

  getByLabel(value: string, options: {exact?: boolean} = {}): Locator {
    return this.#semanticLocator("LABEL", value, options);
  }

  getByPlaceholder(value: string, options: {exact?: boolean} = {}): Locator {
    return this.#semanticLocator("PLACEHOLDER", value, options);
  }

  getByAltText(value: string, options: {exact?: boolean} = {}): Locator {
    return this.#semanticLocator("ALT_TEXT", value, options);
  }

  getByTitle(value: string, options: {exact?: boolean} = {}): Locator {
    return this.#semanticLocator("TITLE", value, options);
  }

  #semanticLocator(
    kind: "LABEL" | "PLACEHOLDER" | "ALT_TEXT" | "TITLE",
    value: string,
    options: {exact?: boolean},
  ): Locator {
    return new ClientLocator(this, {
      kind,
      value,
      match: options.exact === true ? "EXACT" : "CONTAINS",
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

  async dom(operation: WireDOMOperation, options: WaitOptions, expectation?: WireExpectation): Promise<DriverOperationResult> {
    this.#assertOpen();
    return await this.#transport.dom({
      sessionId: this.id,
      operation,
      ...(expectation && {expectation}),
      poll: pollPolicy(options),
    } satisfies DOMRequest, {
      ...options,
      ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100}),
    });
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
		realistic = false,
  ): Promise<DriverOperationResult> {
    this.#assertOpen();
    return await this.#transport.dragTo({
      sessionId: this.id,
      source,
      target,
		...(realistic && {realistic: true}),
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

  level(level: number): Locator {
    if (!Number.isInteger(level) || level <= 0) {
      throw new BilobaError({code: "INVALID_ARGUMENT", message: "locator.level requires a positive integer"});
    }
    return new ClientLocator(this.#session, {...this.#locator, level, levelSet: true}, this.#realistic);
  }

  checked(): Locator { return this.#withState("checked"); }
  disabled(): Locator { return this.#withState("disabled"); }
  expanded(): Locator { return this.#withState("expanded"); }
  pressed(): Locator { return this.#withState("pressed"); }
  selected(): Locator { return this.#withState("selected"); }

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

  async click(options: ClickOptions = {}): Promise<void> {
		if (options.button === undefined && options.clickCount === undefined && options.position === undefined && !options.modifiers?.length) {
			await this.#session.action("click", this.#locator, this.#realistic ? {realistic: true} : {}, options);
			return;
		}
		await this.#domAction({kind: "CLICK", button: options.button ?? "left", clickCount: options.clickCount ?? 1, ...pointerWire(options)}, options, "click");
  }

  async dblclick(options: PointerOptions = {}): Promise<void> {
    await this.#domAction({kind: "CLICK", button: "left", clickCount: 2, ...pointerWire(options)}, options, "double-click");
  }

  async rightClick(options: PointerOptions = {}): Promise<void> {
    await this.#domAction({kind: "CLICK", button: "right", clickCount: 1, ...pointerWire(options)}, options, "right-click");
  }

  async middleClick(options: PointerOptions = {}): Promise<void> {
    await this.#domAction({kind: "CLICK", button: "middle", clickCount: 1, ...pointerWire(options)}, options, "middle-click");
  }

  async clickAll(options: CancellationOptions = {}): Promise<void> {
    assertCancellationOptions(options, "clickAll");
    await this.#domAction({kind: "CLICK_EACH"}, {...options, mode: "immediate"}, "click each");
  }

  async tap(options: PointerOptions = {}): Promise<void> {
    await this.#domAction({kind: "TAP", ...pointerWire(options)}, options, "tap");
  }

  async focus(options: WaitOptions = {}): Promise<void> { await this.#domAction({kind: "FOCUS"}, options, "focus"); }
  async blur(options: WaitOptions = {}): Promise<void> { await this.#domAction({kind: "BLUR"}, options, "blur"); }
  async hover(options: WaitOptions = {}): Promise<void> { await this.#domAction({kind: "HOVER"}, options, "hover"); }

  async setValue(value: SerializableValue, options: WaitOptions = {}): Promise<void> {
    await this.#session.action("setValue", this.#locator, {
      valueJson: JSON.stringify(value),
      ...(this.#realistic && {realistic: true}),
    }, options);
  }

  async selectOption(value: string | ValueLabel, options: WaitOptions = {}): Promise<void> {
    await this.setValue(value, options);
  }

  async type(keys: string, options: KeyboardOptions = {}): Promise<void> {
		if (!options.modifiers?.length) {
			await this.#session.action("type", this.#locator, {keys, ...(this.#realistic && {realistic: true})}, options);
			return;
		}
		await this.#domAction({kind: "TYPE", keys, modifiers: [...options.modifiers]}, options, "type");
  }

  async setUploadFiles(paths: readonly string[], options: WaitOptions = {}): Promise<void> {
    const callsiteStack = new Error().stack;
    const response = await this.#session.setUpload(this.#locator, paths, options);
    operationResult(response, "set upload operation", callsiteStack);
  }

  async dragTo(target: Locator | string, options: WaitOptions = {}): Promise<void> {
		const callsiteStack = new Error().stack;
		const response = await this.#session.dragTo(this.#locator, wireLocator(target), options, this.#realistic);
		operationResult(response, "drag to operation", callsiteStack);
  }

  async scrollIntoView(options: ScrollIntoViewOptions = {}): Promise<void> {
    await this.#domAction({kind: "SCROLL_INTO_VIEW", ...(options.within && {container: wireLocator(options.within)}), ...(options.topOffset !== undefined && {topOffset: options.topOffset, hasTopOffset: true})}, options, "scroll into view");
  }

  async scrollWheel(deltaX: number, deltaY: number, options: WaitOptions = {}): Promise<void> { await this.#domAction({kind: "SCROLL_WHEEL", deltaX, deltaY}, options, "scroll wheel"); }

  async selectText(options: WaitOptions & {substring?: string; occurrence?: number} = {}): Promise<void> {
    await this.#domAction({kind: "SELECT", ...(options.substring !== undefined && {substring: options.substring, occurrence: options.occurrence ?? 1})}, options, "select text");
  }

  async selectRange(start: number, end: number, options: WaitOptions = {}): Promise<void> { await this.#domAction({kind: "SELECT", start, end, range: true}, options, "select range"); }

  async setProperty(name: string, value: SerializableValue, options: WaitOptions = {}): Promise<void> {
    assertPollOptions(options, "setProperty");
    await this.#domAction({kind: "SET_PROPERTY", name, valueJson: JSON.stringify(value)}, options, "set property");
  }

  async setPropertyAll(name: string, value: SerializableValue, options: CancellationOptions = {}): Promise<void> {
    assertCancellationOptions(options, "setPropertyAll");
    await this.#domAction({kind: "SET_PROPERTY", name, valueJson: JSON.stringify(value), all: true}, {...options, mode: "immediate"}, "set property for each");
  }

  async invokeMethod<T = unknown>(method: string, args: readonly SerializableValue[] = [], options: WaitOptions = {}): Promise<T> {
    assertPollOptions(options, "invokeMethod");
    return await this.#domRead<T>({kind: "INVOKE_METHOD", method, argumentsJson: JSON.stringify(args)}, options);
  }

  async invoke<T = unknown>(expression: string, args: readonly SerializableValue[] = [], options: WaitOptions = {}): Promise<T> {
    assertPollOptions(options, "invoke");
    return await this.#domRead<T>({kind: "INVOKE_FUNCTION", expression, argumentsJson: JSON.stringify(args)}, options);
  }

  async invokeMethodAll<T = unknown>(method: string, args: readonly SerializableValue[] = [], options: CancellationOptions = {}): Promise<readonly T[]> {
    assertCancellationOptions(options, "invokeMethodAll");
    return await this.#domRead<readonly T[]>({kind: "INVOKE_METHOD_FOR_EACH", method, argumentsJson: JSON.stringify(args)}, {...options, mode: "immediate"});
  }

  async invokeAll<T = unknown>(expression: string, args: readonly SerializableValue[] = [], options: CancellationOptions = {}): Promise<readonly T[]> {
    assertCancellationOptions(options, "invokeAll");
    return await this.#domRead<readonly T[]>({kind: "INVOKE_FUNCTION_FOR_EACH", expression, argumentsJson: JSON.stringify(args)}, {...options, mode: "immediate"});
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

  async expectChecked(options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domBoolean("STATE", {state: "checked"}, false, options); }
  async expectNotChecked(options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domBoolean("STATE", {state: "checked"}, true, options); }
  async expectFocused(options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domBoolean("STATE", {state: "focused"}, false, options); }
  async expectNotFocused(options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domBoolean("STATE", {state: "focused"}, true, options); }
  async expectAllVisible(options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domBoolean("ALL_STATE", {state: "visible"}, false, options); }
  async expectAllEnabled(options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domBoolean("ALL_STATE", {state: "enabled"}, false, options); }
  async expectClass(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> {
    const expectation = typeof expected === "string"
      ? {kind: "CONTAINS", expectedJson: JSON.stringify(expected)} satisfies WireExpectation
      : wireExpectation(expected);
    const response = await this.#session.dom({kind: "CLASSES", locator: this.#locator}, options, expectation);
    return assertionResult(response, new Error().stack);
  }
  async expectEachClass(name: string, options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.#domAssert({kind: "CLASSES_FOR_EACH", every: true}, {kind: "contains", expected: name}, options);
  }
  async expectInnerText(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "TEXT", textMode: "INNER_TEXT"}, expected, options); }
  async expectTextContent(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "TEXT", textMode: "TEXT_CONTENT"}, expected, options); }
  async expectNormalizedText(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "TEXT", textMode: "NORMALIZED_TEXT"}, expected, options); }
  async expectEachInnerText(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "TEXTS", textMode: "INNER_TEXT", every: true}, expected, options); }
  async expectEachTextContent(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "TEXTS", textMode: "TEXT_CONTENT", every: true}, expected, options); }
  async expectEachNormalizedText(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "TEXTS", textMode: "NORMALIZED_TEXT", every: true}, expected, options); }
  async expectAttributePresent(name: string, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domPresence({kind: "ATTRIBUTES", names: [{name}]}, options); }
  async expectEachAttribute(name: string, expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "ATTRIBUTES_FOR_EACH", names: [{name}], every: true, projectName: name}, expected, options); }
  async expectPropertyPresent(name: string, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domPresence({kind: "PROPERTIES", names: [{name}]}, options); }
  async expectJSONAttribute(name: string, expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "JSON_ATTRIBUTE", name}, expected, options); }
  async expectEachProperty(name: string, expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "PROPERTY_FOR_EACH", name, every: true}, expected, options); }
  async expectInnerHTML(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.expectProperty("innerHTML", expected, options); }
  async expectDistinctAttributeCount(name: string, expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "DISTINCT_ATTRIBUTE_COUNT", name}, expected, options); }
  async expectInViewport(options: WaitOptions & {fully?: boolean; negated?: boolean} = {}): Promise<AssertionResult> { return await this.#domBoolean("IN_VIEWPORT", {fully: options.fully ?? false}, options.negated ?? false, options); }
  async expectGeometry(relation: GeometryRelation, other: Locator | string, options: WaitOptions & {negated?: boolean} = {}): Promise<AssertionResult> { return await this.#domBoolean("GEOMETRY_RELATION", {relation, target: wireLocator(other)}, options.negated ?? false, options); }
  async expectComputedStyle(name: string, expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "COMPUTED_STYLE", name}, expected, options); }
  async expectComputedStyleNumber(name: string, expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "COMPUTED_STYLE_NUMBER", name}, expected, options); }
  async expectComputedColor(name: string, expected: string, options: WaitOptions = {}): Promise<AssertionResult> {
    return await this.expectComputedStyle(name, await this.#session.normalizeColor(expected), options);
  }
  async expectBoundingBox(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "BOUNDING_BOX"}, expected, options); }
  async expectScrollOffset(expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "SCROLL_OFFSET"}, expected, options); }
  async expectOffsetWithin(container: Locator | string, expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "OFFSET_WITHIN", target: wireLocator(container)}, expected, options); }
  async expectRelativeBoxes(other: Locator | string, expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "RELATIVE_BOXES", target: wireLocator(other)}, expected, options); }
  async expectGapBetween(other: Locator | string, expected: ExpectedValue, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "GAP_BETWEEN", target: wireLocator(other)}, expected, options); }
  async expectDocumentOrder(other: Locator | string, expected: DocumentOrder, options: WaitOptions = {}): Promise<AssertionResult> { return await this.#domAssert({kind: "DOCUMENT_ORDER", target: wireLocator(other)}, expected, options); }
  async expectAbove(other: Locator | string, options: WaitOptions = {}): Promise<AssertionResult> { return await this.expectGeometry("above", other, options); }
  async expectBelow(other: Locator | string, options: WaitOptions = {}): Promise<AssertionResult> { return await this.expectGeometry("below", other, options); }
  async expectLeftOf(other: Locator | string, options: WaitOptions = {}): Promise<AssertionResult> { return await this.expectGeometry("leftOf", other, options); }
  async expectRightOf(other: Locator | string, options: WaitOptions = {}): Promise<AssertionResult> { return await this.expectGeometry("rightOf", other, options); }
  async expectEncloses(other: Locator | string, options: WaitOptions = {}): Promise<AssertionResult> { return await this.expectGeometry("encloses", other, options); }
  async expectOverlaps(other: Locator | string, options: WaitOptions = {}): Promise<AssertionResult> { return await this.expectGeometry("overlaps", other, options); }
  async expectBefore(other: Locator | string, options: WaitOptions = {}): Promise<AssertionResult> { return await this.expectDocumentOrder(other, "before", options); }
  async expectAfter(other: Locator | string, options: WaitOptions = {}): Promise<AssertionResult> { return await this.expectDocumentOrder(other, "after", options); }

  async innerText(options: WaitOptions = {}): Promise<string> { return await this.#domRead<string>({kind: "TEXT", textMode: "INNER_TEXT"}, options); }
  async textContent(options: WaitOptions = {}): Promise<string> { return await this.#domRead<string>({kind: "TEXT", textMode: "TEXT_CONTENT"}, options); }
  async normalizedText(options: WaitOptions = {}): Promise<string> { return await this.#domRead<string>({kind: "TEXT", textMode: "NORMALIZED_TEXT"}, options); }
  async innerHTML(options: WaitOptions = {}): Promise<string> { return await this.getProperty<string>("innerHTML", options); }
  async currentInnerTexts(): Promise<readonly string[]> { return await this.#domRead<readonly string[]>({kind: "TEXTS", textMode: "INNER_TEXT"}, {mode: "immediate"}); }
  async currentTextContents(): Promise<readonly string[]> { return await this.#domRead<readonly string[]>({kind: "TEXTS", textMode: "TEXT_CONTENT"}, {mode: "immediate"}); }
  async currentNormalizedTexts(): Promise<readonly string[]> { return await this.#domRead<readonly string[]>({kind: "TEXTS", textMode: "NORMALIZED_TEXT"}, {mode: "immediate"}); }

  // Polls, like Go's GetInnerText: an element that has not rendered yet is something to wait for, not
  // an answer of null.  exists()/count() below stay immediate on purpose - they are the snapshot
  // bucket, where "nothing matched" is the answer.
  async text(options: WaitOptions = {}): Promise<string> {
    const result = await this.#assert({kind: "TEXT", locator: this.#locator, expectation: {kind: "ANYTHING"}}, options);
    return result.observed as string;
  }

  async count(): Promise<number> {
    return await this.#read<number>({kind: "COUNT", locator: this.#locator});
  }

  async classes(options: WaitOptions = {}): Promise<readonly string[]> { return await this.#domRead<readonly string[]>({kind: "CLASSES"}, options); }
  async currentClasses(): Promise<readonly (readonly string[])[]> { return await this.#domRead<readonly (readonly string[])[]>({kind: "CLASSES_FOR_EACH"}, {mode: "immediate"}); }
  async attributes(names: readonly NameSpec[], options: WaitOptions = {}): Promise<Readonly<Record<string, unknown>>> { return await this.#domRead({kind: "ATTRIBUTES", names: wireNames(names)}, options); }
  async currentAttributes(names: readonly string[]): Promise<readonly Readonly<Record<string, string | null>>[]> { return await this.#domRead({kind: "ATTRIBUTES_FOR_EACH", names: wireNames(names)}, {mode: "immediate"}); }
  async jsonAttribute<T = unknown>(name: string, options: WaitOptions = {}): Promise<T> { return await this.#domRead<T>({kind: "JSON_ATTRIBUTE", name}, options); }
  async properties<T extends Readonly<Record<string, unknown>> = Readonly<Record<string, unknown>>>(names: readonly NameSpec[], options: WaitOptions = {}): Promise<T> { return await this.#domRead<T>({kind: "PROPERTIES", names: wireNames(names)}, options); }
  async currentProperties(names: readonly string[]): Promise<readonly Readonly<Record<string, unknown>>[]> { return await this.#domRead({kind: "PROPERTIES_FOR_EACH", names: wireNames(names)}, {mode: "immediate"}); }
  async currentProperty<T = unknown>(name: string): Promise<readonly T[]> { return await this.#domRead<readonly T[]>({kind: "PROPERTY_FOR_EACH", name}, {mode: "immediate"}); }
  async currentValues<T = unknown>(): Promise<readonly T[]> { return await this.#domRead<readonly T[]>({kind: "VALUES"}, {mode: "immediate"}); }

  async getAttribute(name: string, options: WaitOptions = {}): Promise<string | null> {
    const value = await this.attributes([name], options);
    return value[name] as string | null;
  }

  async getProperty<T = unknown>(name: string, options: WaitOptions = {}): Promise<T> {
    const value = await this.properties<Record<string, unknown>>([name], options);
    return value[name] as T;
  }

  async value<T = unknown>(options: WaitOptions = {}): Promise<T> {
    const result = await this.#assert({kind: "VALUE", locator: this.#locator, expectation: {kind: "ANYTHING"}}, options);
    return result.observed as T;
  }

  async exists(): Promise<boolean> {
    return await this.#read<boolean>({kind: "EXISTS", locator: this.#locator});
  }

  async boundingBox(options: WaitOptions = {}): Promise<Box> { return await this.#domRead<Box>({kind: "BOUNDING_BOX"}, options); }
  async scrollOffset(options: WaitOptions = {}): Promise<ScrollOffset> { return await this.#domRead<ScrollOffset>({kind: "SCROLL_OFFSET"}, options); }
  async offsetWithin(container: Locator | string, options: WaitOptions = {}): Promise<Offset> { return await this.#domRead<Offset>({kind: "OFFSET_WITHIN", target: wireLocator(container)}, options); }
  async relativeBoxes(other: Locator | string, options: WaitOptions = {}): Promise<BoxPair> { return await this.#domRead<BoxPair>({kind: "RELATIVE_BOXES", target: wireLocator(other)}, options); }
  async gapBetween(other: Locator | string, options: WaitOptions = {}): Promise<BoxDelta> { return await this.#domRead<BoxDelta>({kind: "GAP_BETWEEN", target: wireLocator(other)}, options); }
  async documentOrder(other: Locator | string, options: WaitOptions = {}): Promise<DocumentOrder> { return await this.#domRead<DocumentOrder>({kind: "DOCUMENT_ORDER", target: wireLocator(other)}, options); }
  async computedStyle(name: string, options: WaitOptions = {}): Promise<string> { return await this.#domRead<string>({kind: "COMPUTED_STYLE", name}, options); }
  async computedStyleNumber(name: string, options: WaitOptions = {}): Promise<number> { return await this.#domRead<number>({kind: "COMPUTED_STYLE_NUMBER", name}, options); }

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

  async #domAction(operation: WireDOMOperation, options: WaitOptions, label: string): Promise<void> {
    const response = await this.#session.dom({...operation, locator: this.#locator, ...(this.#realistic && {realistic: true})}, options);
    operationResult(response, `${label} operation`, new Error().stack);
  }

  async #domRead<T>(operation: WireDOMOperation, options: WaitOptions): Promise<T> {
    const response = await this.#session.dom({...operation, locator: this.#locator}, options);
    operationResult(response, "DOM read", new Error().stack);
    return parseJson(response.observedJson) as T;
  }

  async #domAssert(operation: WireDOMOperation, expected: ExpectedValue, options: WaitOptions): Promise<AssertionResult> {
    const response = await this.#session.dom({...operation, locator: this.#locator}, options, wireExpectation(expected));
    return assertionResult(response, new Error().stack);
  }

  async #domPresence(operation: WireDOMOperation, options: WaitOptions): Promise<AssertionResult> {
    const response = await this.#session.dom({...operation, locator: this.#locator}, options);
    return assertionResult(response, new Error().stack);
  }

  async #domBoolean(kind: WireDOMOperation["kind"], fields: Partial<WireDOMOperation>, negated: boolean, options: WaitOptions): Promise<AssertionResult> {
    const expected: WireExpectation = {kind: "EQUAL", expectedJson: "true"};
    const response = await this.#session.dom({kind, locator: this.#locator, ...fields}, options, negated ? negate(expected) : expected);
    return assertionResult(response, new Error().stack);
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

  #withState(state: "checked" | "disabled" | "expanded" | "pressed" | "selected"): ClientLocator {
    return new ClientLocator(this.#session, {
      ...this.#locator,
      states: [...(this.#locator.states ?? []), state],
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

function wireNames(names: readonly NameSpec[]): NonNullable<WireDOMOperation["names"]> {
  return names.map((name) => typeof name === "string" ? {name} : {name: name.name, allowMissing: true});
}

function pointerWire(options: PointerOptions): Pick<WireDOMOperation, "offset" | "modifiers"> {
  return {
    ...(options.position && {offset: options.position}),
    ...(options.modifiers && {modifiers: [...options.modifiers]}),
  };
}

function assertCancellationOptions(options: CancellationOptions, operation: string): void {
  for (const key of Object.keys(options)) {
    if (key !== "signal") {
      throw new BilobaError({code: "INVALID_ARGUMENT", message: `${operation} only accepts cancellation options`});
    }
  }
}

function assertPollOptions(options: WaitOptions, operation: string): void {
  const allowed = new Set(["timeoutMs", "intervalMs", "signal", "mode"]);
  for (const key of Object.keys(options)) {
    if (!allowed.has(key)) {
      throw new BilobaError({code: "INVALID_ARGUMENT", message: `${operation} received unsupported option ${key}`});
    }
  }
}

function assertWindowKeyboardOptions(options: WindowKeyboardOptions): void {
  for (const key of Object.keys(options)) {
    if (key !== "signal" && key !== "modifiers") {
      throw new BilobaError({code: "INVALID_ARGUMENT", message: "sendKeys only accepts modifiers and cancellation"});
    }
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

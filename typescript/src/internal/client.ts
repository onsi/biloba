import type {
  Assertion as WireAssertion,
  DragToRequest,
  DOMOperation as WireDOMOperation,
  DOMRequest,
  Expectation as WireExpectation,
  Locator as WireLocator,
  LifecycleOperation as WireLifecycleOperation,
  LocatorRequest,
  OperationResult as StdioOperationResult,
  SetValueRequest,
  SetUploadRequest,
  TypeRequest,
} from "../generated/protocol.js";
import {resolve} from "node:path";
import {
  BilobaError,
  type AssertionResult,
  type Browser,
  type ConnectOptions,
  type Cookie,
  type ClickOptions,
  type CommandOptions,
  type WaitingCommandOptions,
  type PollOptions,
  type CookieQuery,
  type BrowserStorage,
  type StorageArea,
  type StorageItem,
  type TabQuery,
  type WindowSize,
  type ConsoleMessage,
  type DeviceMetrics,
  type Geolocation,
  type Permission,
  type PermissionState,
  type MediaEmulation,
  type Dialog, type DialogQuery, type DialogHandler, type DialogHandlerOptions, type Warning,
  type Download, type DownloadQuery, type NetworkRequest, type RequestQuery, type NetworkResponse, type ResponseQuery,
  type RequestOverride, type ResponseOverride, type FulfillResponse, type HeaderEntry, type InterceptedResponse, type NetworkHandlerOptions, type NetworkHandler,
  type NetworkHandlerStats, type NetworkShadowDiagnostic, type NetworkState, type ResponseHoldStats,
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
  type ScreenshotBytesOptions,
  type ScreenshotPathOptions,
  type ScreenshotWarning,
  type VisualResult,
  type VisualScreenshotOptions,
  type LaunchMetadata,
  type CaptureDiagnosticsOptions,
  type ContextDiagnostics,
  type DriverDebugEvent,
  type BilobaWarning,
} from "../index.js";
import {StdioTransport} from "./stdio-transport.js";

type DriverTransport = StdioTransport;
type DriverOperationResult = StdioOperationResult;
let callbackSequence = 0;
const maxScreenshotBytes = 16 * 1024 * 1024;
type VisualClientConfig = {readonly maxBytes: number; readonly warningSink?: (warning: ScreenshotWarning) => void};
export interface ResolvedDiagnosticsPolicy {artifactDir?: string; failureScreenshots: boolean; failureOutlines: boolean; failureViewport?: WindowSize; progressScreenshots: boolean; progressOutlines: boolean; progressViewport?: WindowSize; pollTrajectory: boolean; inlineScreenshots: boolean | "auto"; maxScreenshotBytes: number}

export function booleanEnvironment(name: string, environment: NodeJS.ProcessEnv): boolean | undefined {
  const raw = environment[name];
  if (raw === undefined || raw === "") return undefined;
  if (["1", "t", "true"].includes(raw.toLowerCase())) return true;
  if (["0", "f", "false"].includes(raw.toLowerCase())) return false;
  process.emitWarning(`${name} must be a boolean; ignoring ${JSON.stringify(raw)}`, {code: "BILOBA_INVALID_ENV"});
  return undefined;
}

export function resolveDiagnosticsPolicy(options: import("../index.js").DiagnosticsPolicyOptions, environment: NodeJS.ProcessEnv, automation: boolean): ResolvedDiagnosticsPolicy {
  const artifactDir = options.artifactDir ?? environment.BILOBA_SCREENSHOTS_DIR ?? (automation ? "./biloba-screenshots" : undefined);
  let inlineDefault: boolean | "auto" = automation ? false : "auto";
  if (booleanEnvironment("BILOBA_INTERACTIVE", environment) === true && options.inlineScreenshots === undefined) inlineDefault = "auto";
  if (options.inlineScreenshots === undefined && environment.BILOBA_INLINE_SCREENSHOTS) {
    const inline = environment.BILOBA_INLINE_SCREENSHOTS;
    if (inline === "none") inlineDefault = false;
    else if (["iterm", "kitty", "sixel"].includes(inline)) inlineDefault = true;
    else process.emitWarning(`BILOBA_INLINE_SCREENSHOTS must be iterm, kitty, sixel, or none; ignoring ${JSON.stringify(inline)}`, {code: "BILOBA_INVALID_ENV"});
  }
  return {
    ...(artifactDir !== undefined && {artifactDir}),
    failureScreenshots: options.failureScreenshots ?? true,
    failureOutlines: options.failureOutlines ?? automation,
    ...(options.failureViewport && {failureViewport: options.failureViewport}),
    progressScreenshots: options.progressScreenshots ?? true,
    progressOutlines: options.progressOutlines ?? false,
    ...(options.progressViewport && {progressViewport: options.progressViewport}),
    pollTrajectory: options.pollTrajectory ?? true,
    inlineScreenshots: options.inlineScreenshots ?? inlineDefault,
    maxScreenshotBytes: options.maxScreenshotBytes ?? maxScreenshotBytes,
  };
}

export function automationDetected(environment: NodeJS.ProcessEnv): boolean {
  return Boolean(environment.CI || environment.AI_AGENT || environment.CLAUDECODE || environment.CURSOR_AGENT || environment.GEMINI_CLI || environment.CODEX_SANDBOX);
}

class ClientBrowser implements Browser {
  readonly #transport: DriverTransport;
  readonly #sessions = new Set<ClientSession>();
  readonly #stopDaemon?: () => Promise<void>;
  readonly protocolVersion: string;
  readonly capabilities: ReadonlySet<string>;
  readonly launch: LaunchMetadata;
  readonly #warningSink?: (warning: Warning) => void;
  readonly #visual: VisualClientConfig;
  readonly #diagnostics: ResolvedDiagnosticsPolicy;
  #closed = false;
  #debugSubscription?: Promise<string>;

  constructor(
    transport: DriverTransport,
    protocolVersion: string,
    capabilities: readonly string[],
    launch: LaunchMetadata,
    stopDaemon?: () => Promise<void>,
    warningSink?: (warning: Warning) => void,
    visual: VisualClientConfig = {maxBytes: maxScreenshotBytes},
    debugLog?: (entry: DriverDebugEvent) => void,
    diagnostics: ResolvedDiagnosticsPolicy = resolveDiagnosticsPolicy({}, {}, false),
  ) {
    this.#transport = transport;
    this.protocolVersion = protocolVersion;
    this.capabilities = new Set(capabilities);
    this.launch = launch;
    if (stopDaemon) this.#stopDaemon = stopDaemon;
    if (warningSink) this.#warningSink = warningSink;
    this.#visual = visual;
    this.#diagnostics = diagnostics;
    if (debugLog) {
      this.#debugSubscription = this.#transport.subscribeEvents({types: ["debug"]}).then(({subscriptionId}) => {
        this.#transport.registerEventListener(subscriptionId, (event, envelope) => {
          try {
            if (event === "eventsDropped") debugLog(envelope.payload as Extract<DriverDebugEvent, {code: "EVENTS_DROPPED"}>);
            else debugLog(envelope.payload as Exclude<DriverDebugEvent, {code: "EVENTS_DROPPED"}>);
          } catch { /* sink isolation */ }
        });
        return subscriptionId;
      }).catch((error: unknown) => { try { debugLog({timestamp: new Date().toISOString(), direction: "internal", message: error instanceof Error ? error.message : String(error)}); } catch { /* sink isolation */ } return ""; });
    }
  }

  // Every call opens a genuinely new daemon session with its own browser context.  Sessions are
  // tracked only so close() can reap the ones a suite forgot - reusing one per worker would make
  // any spec that opens two sessions to prove isolation pass vacuously, and a client that Ginkgo,
  // vitest or anything else can drive has no business reading VITEST_POOL_ID.
  async openSession(): Promise<Session> {
    this.#assertOpen();
    const response = await this.#transport.openSession({});
    return this.#trackSession(response);
  }

  async close(): Promise<void> {
    if (this.#closed) return;
    this.#closed = true;
    await Promise.allSettled([...this.#sessions].map(async (session) => session.close()));
    this.#sessions.clear();
    if (this.#debugSubscription) { try { const id = await this.#debugSubscription; if (id) { this.#transport.removeEventListener(id); await this.#transport.unsubscribeEvents({subscriptionId: id}); } } catch { /* teardown is best effort */ } }
    this.#transport.close();
    await this.#stopDaemon?.();
  }

  #assertOpen(): void {
    if (this.#closed) throw new BilobaError({code: "DRIVER_CLOSED", message: "Biloba browser is closed"});
  }

  #trackSession(response: import("../generated/protocol.js").OpenSessionResponse): ClientSession {
    const existing = [...this.#sessions].find((candidate) => candidate.id === response.sessionId);
    if (existing) return existing;
    let session: ClientSession;
    session = new ClientSession(
      response,
      this.#transport,
      () => this.#sessions.delete(session),
      (sibling) => this.#trackSession(sibling),
      (ids) => this.#invalidateSessions(ids),
      this.#warningSink,
      this.#visual,
      this.#diagnostics,
    );
    this.#sessions.add(session);
    return session;
  }

  #invalidateSessions(ids: readonly string[]): void {
    for (const session of this.#sessions) {
      if (ids.includes(session.id)) session.invalidate();
    }
  }
}

class ClientSession implements Session {
  readonly id: string;
  readonly contextId: string;
  readonly targetId: string;
  readonly openerId?: string;
  readonly ownsContext: boolean;
  readonly isFrame: boolean;
  readonly frameUrl?: string;
  readonly #transport: DriverTransport;
  readonly #onClose: () => void;
  readonly #registerSibling: (response: import("../generated/protocol.js").OpenSessionResponse) => ClientSession;
  readonly #invalidateSessions: (ids: readonly string[]) => void;
  readonly #warningSink?: (warning: Warning) => void;
  readonly #visual: VisualClientConfig;
  readonly #diagnostics: ResolvedDiagnosticsPolicy;
  #warningCount = 0;
  readonly #callbackIDs = new Set<string>();
  readonly #consoleListeners = new Set<(message: ConsoleMessage) => void>();
  readonly #warningListeners = new Set<(warning: BilobaWarning) => void>();
  readonly #liveSubscriptions = new Map<"console" | "warning", Promise<string>>();
  readonly #liveTeardown = new Map<"console" | "warning", Promise<void>>();
  #warningFlush: Promise<void> | undefined;
  #eventGeneration = 0;
  closed = false;

  constructor(
    response: import("../generated/protocol.js").OpenSessionResponse,
    transport: DriverTransport,
    onClose: () => void,
    registerSibling: (response: import("../generated/protocol.js").OpenSessionResponse) => ClientSession,
    invalidateSessions: (ids: readonly string[]) => void,
    warningSink?: (warning: Warning) => void,
    visual: VisualClientConfig = {maxBytes: maxScreenshotBytes},
    diagnostics: ResolvedDiagnosticsPolicy = resolveDiagnosticsPolicy({}, {}, false),
  ) {
    this.id = response.sessionId;
    this.contextId = response.contextId ?? "";
    this.targetId = response.targetId ?? "";
    if (response.openerId !== undefined) this.openerId = response.openerId;
    this.ownsContext = response.ownsContext ?? false;
    this.isFrame = response.frame ?? false;
    if (response.url !== undefined) this.frameUrl = response.url;
    this.#transport = transport;
    this.#onClose = onClose;
    this.#registerSibling = registerSibling;
    this.#invalidateSessions = invalidateSessions;
    if (warningSink) this.#warningSink = warningSink;
    this.#visual = visual;
    this.#diagnostics = diagnostics;
  }

  async newTab(options: WaitingCommandOptions = {}): Promise<Session> {
    this.#assertOpen();
    assertWaitingCommandOptions(options, "newTab");
    const response = await this.#transport.newTab({sessionId: this.id}, options);
    await this.#flushWarnings();
    return this.#registerSibling(response);
  }

  async captureScreenshot(options?: ScreenshotBytesOptions): Promise<Uint8Array>;
  async captureScreenshot(options: ScreenshotPathOptions): Promise<string>;
  async captureScreenshot(options: ScreenshotBytesOptions | ScreenshotPathOptions = {}): Promise<Uint8Array | string> {
    return await this.captureScreenshotTarget(undefined, options);
  }

  async captureDiagnostics(options: CaptureDiagnosticsOptions = {}): Promise<ContextDiagnostics> {
    this.#assertOpen();
    assertOptionKeys(options, "captureDiagnostics", new Set(["signal", "timeoutMs", "purpose", "name", "screenshots", "outlines", "viewport", "maxScreenshotBytes", "includeScreenshotBytes"]));
    const purpose = options.purpose ?? "on-demand";
    const screenshots = options.screenshots ?? (purpose === "failure" ? this.#diagnostics.failureScreenshots : purpose === "progress" ? this.#diagnostics.progressScreenshots : true);
    const outlines = options.outlines ?? (purpose === "failure" ? this.#diagnostics.failureOutlines : purpose === "progress" ? this.#diagnostics.progressOutlines : true);
    const viewport = options.viewport ?? (purpose === "failure" ? this.#diagnostics.failureViewport : purpose === "progress" ? this.#diagnostics.progressViewport : undefined);
    if (viewport && (!Number.isInteger(viewport.width) || !Number.isInteger(viewport.height) || viewport.width <= 0 || viewport.height <= 0)) throw new BilobaError({code: "INVALID_ARGUMENT", message: "captureDiagnostics viewport dimensions must be positive integers"});
    const maxBytes = options.maxScreenshotBytes;
    const configuredLimit = this.#visual.maxBytes;
    if (maxBytes !== undefined && (!Number.isInteger(maxBytes) || maxBytes <= 0 || maxBytes > configuredLimit)) throw new BilobaError({code: "INVALID_ARGUMENT", message: `maxScreenshotBytes must be between 1 and ${configuredLimit}`});
    const includeScreenshotBytes = options.includeScreenshotBytes ?? (this.#diagnostics.inlineScreenshots !== false);
    const response = await this.#transport.captureContextDiagnostics({sessionId: this.id, purpose, ...(options.name !== undefined && {name: options.name}), screenshots, outlines, ...(viewport && {width: viewport.width, height: viewport.height}), ...(maxBytes !== undefined && {maxBytes}), ...(includeScreenshotBytes && {includeScreenshotBytes: true})}, options);
    return {purpose: response.purpose as ContextDiagnostics["purpose"], ...(response.artifactDir && {artifactDir: response.artifactDir}), tabs: response.tabs.map((tab) => ({...(tab.sessionId && {sessionId: tab.sessionId}), targetId: tab.targetId, title: tab.title, ...(tab.screenshotPath && {screenshotPath: tab.screenshotPath}), ...(tab.screenshotBase64 && {screenshot: decodeBinaryBody(tab.screenshotBase64, maxBytes ?? configuredLimit)}), ...(tab.outlinePath && {outlinePath: tab.outlinePath}), ...(tab.domOutline && {domOutline: tab.domOutline}), errors: tab.errors as import("../index.js").DiagnosticsArtifactError[]}))};
  }

  async expectScreenshot(name: string, options: VisualScreenshotOptions = {}): Promise<VisualResult> {
    return await this.expectScreenshotTarget(undefined, name, options);
  }

  async captureScreenshotTarget(locator: WireLocator | undefined, options: ScreenshotBytesOptions | ScreenshotPathOptions): Promise<Uint8Array | string> {
    this.#assertOpen();
    assertOptionKeys(options, "captureScreenshot", new Set(["signal", "timeoutMs", "output", "name", "mask", "animated", "colorScheme", "maxBytes"]));
    const limit = this.#screenshotLimit(options.maxBytes);
    const output = options.output === "path" ? "PATH" : "BYTES";
    const response = await this.#transport.screenshot({
      sessionId: this.id,
      operation: {
        kind: "CAPTURE",
        target: locator === undefined ? {kind: "PAGE"} : {kind: "ELEMENT", locator},
        output,
        ...(options.name !== undefined && {name: options.name}),
        ...(options.mask !== undefined && {masks: options.mask.map((mask) => wireScreenshotSubject(this, mask))}),
        ...(options.animated !== undefined && {animated: options.animated}),
        ...(options.colorScheme !== undefined && {colorScheme: options.colorScheme}),
        ...(options.maxBytes !== undefined && {maxBytes: limit}),
      },
    }, options);
    if (response.visual !== undefined || response.screenshot === undefined) throw driverShapeError("screenshot capture response contained an invalid result shape");
    const capture = response.screenshot;
    this.#emitScreenshotWarnings("captureScreenshot", capture.warnings ?? []);
    const hasBytes = capture.pngBase64 !== undefined;
    const hasPath = capture.artifactPath !== undefined;
    if (hasBytes === hasPath || (output === "BYTES") !== hasBytes) throw driverShapeError("screenshot capture response contained conflicting output fields");
    return hasBytes ? decodeBinaryBody(capture.pngBase64!, limit) : capture.artifactPath!;
  }

  async expectScreenshotTarget(locator: WireLocator | undefined, name: string, options: VisualScreenshotOptions): Promise<VisualResult> {
    this.#assertOpen();
    assertOptionKeys(options, "expectScreenshot", new Set(["signal", "timeoutMs", "intervalMs", "mode", "mask", "animated", "colorSchemes", "pixelTolerance", "channelTolerance", "maxBytes"]));
    if (options.mode === "consistently") throw new BilobaError({code: "INVALID_ARGUMENT", message: "expectScreenshot does not support consistently"});
    const limit = this.#screenshotLimit(options.maxBytes);
    const callsiteStack = new Error().stack;
    try {
      const response = await this.#transport.screenshot({
        sessionId: this.id,
        operation: {
          kind: "EXPECT",
          target: locator === undefined ? {kind: "PAGE"} : {kind: "ELEMENT", locator},
          name,
          ...(options.mask !== undefined && {masks: options.mask.map((mask) => wireScreenshotSubject(this, mask))}),
          ...(options.animated !== undefined && {animated: options.animated}),
          ...(options.colorSchemes !== undefined && {colorSchemes: [...options.colorSchemes]}),
          ...(options.pixelTolerance !== undefined && {pixelTolerance: options.pixelTolerance}),
          ...(options.channelTolerance !== undefined && {channelTolerance: options.channelTolerance}),
          ...(options.maxBytes !== undefined && {maxBytes: limit}),
        },
        poll: pollPolicy(options),
      }, {...options, ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100})});
      if (response.screenshot !== undefined || response.visual === undefined) throw driverShapeError("visual comparison response contained an invalid result shape");
      const visual = response.visual as VisualResult;
      this.#emitScreenshotWarnings("expectScreenshot", visual.warnings);
      if (!response.matched || !visual.match) {
        throw new BilobaError({code: "TIMEOUT", message: visualFailureMessage(name, visual), visual, artifactPaths: visualArtifactPaths(visual), ...(callsiteStack && {callsiteStack})});
      }
      return visual;
    } catch (error) {
      if (error instanceof BilobaError && callsiteStack && !error.stack?.includes(stripStackHeader(callsiteStack))) error.stack = `${error.name}: ${error.message}\n${stripStackHeader(callsiteStack)}`;
      throw error;
    }
  }

  #screenshotLimit(requested: number | undefined): number {
    const value = requested ?? this.#visual.maxBytes;
    if (!Number.isSafeInteger(value) || value <= 0 || value > this.#visual.maxBytes || value > maxScreenshotBytes) throw new BilobaError({code: "INVALID_ARGUMENT", message: `maxBytes must be a positive integer no greater than ${Math.min(this.#visual.maxBytes, maxScreenshotBytes)}`});
    return value;
  }

  // Go prints these unconditionally.  They include both vacuity guards - a baseline that never
  // settled, and two color schemes that captured byte-identical images - and the second fires on a
  // *passing* assertion, so a visual test that can never fail said nothing at all.  Default to
  // stderr; onScreenshotWarning stays the way to redirect them.
  #emitScreenshotWarnings(operation: ScreenshotWarning["operation"], warnings: readonly string[]): void {
    for (const message of warnings) {
      const warning = {sessionId: this.id, operation, message};
      if (this.#visual.warningSink) this.#visual.warningSink(warning);
      else process.stderr.write(`biloba ${operation}: ${message}\n`);
    }
  }

  async tabs(options: CommandOptions = {}): Promise<readonly Session[]> { return await this.#listHandles("tabs", false, options); }
  async spawnedTabs(options: CommandOptions = {}): Promise<readonly Session[]> { return await this.#listHandles("tabs", true, options); }
  async frames(options: CommandOptions = {}): Promise<readonly Session[]> { return await this.#listHandles("frames", false, options); }
  async findTab(query: TabQuery, options: CommandOptions = {}): Promise<Session | undefined> {
    const tabs = await this.tabs(options);
    if (Object.keys(query).length === 0) return tabs[0];
    // A zero-time server poll keeps all predicates on the same candidate observation.
    try { return await this.waitForTab(query, {...options, mode: "immediate"}); } catch (error) { if (error instanceof BilobaError && error.code === "TIMEOUT") return undefined; throw error; }
  }
  async waitForTab(query: TabQuery, options: PollOptions = {}): Promise<Session> { return await this.#waitForHandle("tab", query, options); }
  async waitForFrame(query: TabQuery, options: PollOptions = {}): Promise<Session> { return await this.#waitForHandle("frame", query, options); }

  async addInitScript(script: string, options: CommandOptions = {}): Promise<void> {
    this.#assertOpen();
    assertCancellationOptions(options, "addInitScript");
    const response = await this.#transport.addInitScript({sessionId: this.id, script}, options);
    operationResult(response, "add init script operation", new Error().stack);
    await this.#flushWarnings();
  }

  async activate(options: CommandOptions = {}): Promise<void> {
    this.#assertOpen();
    assertCancellationOptions(options, "activate");
    const response = await this.#transport.activate({sessionId: this.id}, options);
    operationResult(response, "activate tab operation", new Error().stack);
    await this.#flushWarnings();
  }

  async prepare(options: WaitingCommandOptions = {}): Promise<{readonly invalidatedSessionIds: readonly string[]}> {
    this.#assertOpen();
    assertWaitingCommandOptions(options, "prepare");
    const response = await this.#transport.prepareSession({sessionId: this.id}, options);
    this.#eventGeneration++;
    this.#clearCallbacks();
    this.#warningCount = 0;
    const ids = response.invalidatedSessionIds ?? [];
    this.#invalidateSessions(ids);
    return {invalidatedSessionIds: ids};
  }

  async navigate(url: string, options: WaitingCommandOptions = {}): Promise<void> {
    this.#assertOpen();
    assertWaitingCommandOptions(options, "navigate");
    await this.#transport.navigate({sessionId: this.id, url}, options);
    await this.#flushWarnings();
  }

  // Mirrors the Go runner's NavigateWithStatus.  navigate() asserting 200 is what makes a broken
  // fixture fail at the navigation rather than as a baffling assertion three lines later; this is
  // the way to say the 4xx/5xx page is the page you meant to load.
  async navigateWithStatus(url: string, expectedStatus: number, options: WaitingCommandOptions = {}): Promise<void> {
    this.#assertOpen();
    assertWaitingCommandOptions(options, "navigateWithStatus");
    await this.#transport.navigate({sessionId: this.id, url, expectedStatus}, options);
    await this.#flushWarnings();
  }

  async sendKeys(keys: string, options: WindowKeyboardOptions = {}): Promise<void> {
    this.#assertOpen();
    assertWindowKeyboardOptions(options);
    const response = options.modifiers?.length
      ? await this.dom({kind: "SEND_KEYS", keys, modifiers: [...options.modifiers]}, {...options, mode: "immediate"})
      : await this.#transport.sendKeys({sessionId: this.id, keys}, options);
    operationResult(response, "send keys operation", new Error().stack);
    await this.#flushWarnings();
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

  async setCookies(cookies: readonly Cookie[], options: CommandOptions = {}): Promise<void> {
    this.#assertOpen();
    assertCancellationOptions(options, "setCookies");
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
    }, options);
    await this.#flushWarnings();
  }

  async getCookies(options: CommandOptions = {}): Promise<readonly Cookie[]> {
    this.#assertOpen();
    assertCancellationOptions(options, "getCookies");
    const response = await this.#transport.getCookies({sessionId: this.id}, options);
    await this.#flushWarnings();
    return response.cookies.map(cookieFromWire);
  }
  async clearCookies(options: CommandOptions = {}): Promise<void> {
    this.#assertOpen();
    assertCancellationOptions(options, "clearCookies");
    operationResult(await this.#transport.clearCookies({sessionId: this.id}, options), "clear cookies operation", new Error().stack);
    await this.#flushWarnings();
  }
  async findCookie(query: CookieQuery, options: CommandOptions = {}): Promise<Cookie | undefined> {
    return (await this.getCookies(options)).find((cookie) => currentCookieMatches(cookie, query));
  }
  async expectCookie(query: CookieQuery, options: PollOptions = {}): Promise<Cookie> {
    const response = await this.lifecycleOperation({kind: "COOKIE_QUERY", cookie: wireCookieQuery(query)}, options);
    operationResult(response, "cookie assertion", new Error().stack);
    return cookieFromWire(parseJson(response.observedJson) as import("../generated/protocol.js").Cookie);
  }
  async expectCookieCount(expected: number | Expectation, query: CookieQuery = {}, options: PollOptions = {}): Promise<AssertionResult> {
    const response = await this.lifecycleOperation({kind: "COOKIE_QUERY", cookie: wireCookieQuery(query), count: true}, options, wireExpectation(expected));
    return assertionResult(response, new Error().stack);
  }
  localStorage(): BrowserStorage { return new ClientStorage(this, "localStorage"); }
  sessionStorage(): BrowserStorage { return new ClientStorage(this, "sessionStorage"); }

  // invoke tells the daemon what expression means instead of letting it infer that from the
  // argument count: passing an args array - even an empty one - is the caller saying "call this",
  // so evaluate("(a) => a + 1", []) runs the function while evaluate("window.appState") reads the
  // expression.  Without it the daemon falls back to the pre-invoke inference, where an empty array
  // silently turned a function call back into a function-source read.
  async evaluate<T = unknown>(
    expression: string,
    args?: readonly SerializableValue[],
    options: CommandOptions = {},
  ): Promise<T> {
    this.#assertOpen();
    assertCancellationOptions(options, "evaluate");
    const response = await this.#transport.evaluate({
      sessionId: this.id,
      expression,
      argumentsJson: JSON.stringify(args ?? []),
      invoke: args !== undefined,
    }, options);
    await this.#flushWarnings();
    return parseJson(response.observedJson) as T;
  }

  async evaluateAsync<T = unknown>(
    expression: string,
    args?: readonly SerializableValue[],
    options: WaitingCommandOptions = {},
  ): Promise<T> {
    this.#assertOpen();
    assertWaitingCommandOptions(options, "evaluateAsync");
    const response = await this.#transport.evaluate({
      sessionId: this.id,
      expression,
      argumentsJson: JSON.stringify(args ?? []),
      invoke: args !== undefined,
      awaitPromise: true,
    }, options);
    await this.#flushWarnings();
    return parseJson(response.observedJson) as T;
  }

  async waitForDefined<T = unknown>(expression: string, options: PollOptions = {}): Promise<T> {
    const response = await this.lifecycleOperation({kind: "WAIT_FOR_DEFINED", expression}, options);
    operationResult(response, "wait for defined JavaScript value", new Error().stack);
    return parseJson(response.observedJson) as T;
  }

  async setWindowSize(width: number, height: number, options: CommandOptions = {}): Promise<void> {
    this.#assertOpen();
    assertCancellationOptions(options, "setWindowSize");
    const response = await this.#transport.setWindowSize({sessionId: this.id, width, height}, options);
    operationResult(response, "set window size operation", new Error().stack);
    await this.#flushWarnings();
  }

  async windowSize(options: CommandOptions = {}): Promise<WindowSize> { return await this.#lifecycleValue<WindowSize>({kind: "WINDOW_SIZE"}, options); }
  async title(options: CommandOptions = {}): Promise<string> { return await this.#lifecycleValue<string>({kind: "TITLE"}, options); }
  async expectTitle(expected: ExpectedValue, options: PollOptions = {}): Promise<AssertionResult> { return assertionResult(await this.lifecycleOperation({kind: "TITLE"}, options, wireExpectation(expected)), new Error().stack); }
  async outline(options: CommandOptions = {}): Promise<string> { return await this.#lifecycleValue<string>({kind: "OUTLINE"}, options); }
  async accessibilityOutline(options: CommandOptions = {}): Promise<string> { return await this.#lifecycleValue<string>({kind: "ACCESSIBILITY_OUTLINE"}, options); }
  async consoleMessages(options: CommandOptions = {}): Promise<readonly ConsoleMessage[]> { return await this.#lifecycleValue<readonly ConsoleMessage[]>({kind: "CONSOLE_MESSAGES"}, options); }
  onConsoleMessage(listener: (message: ConsoleMessage) => void): () => void { return this.#addLiveListener("console", listener); }
  onWarning(listener: (warning: BilobaWarning) => void): () => void { return this.#addLiveListener("warning", listener); }
  async expectConsoleMessage(expected: ExpectedValue, options: PollOptions & {type?: string} = {}): Promise<ConsoleMessage> {
    const {type, ...poll} = options;
    const response = await this.lifecycleOperation({kind: "CONSOLE_MESSAGES", ...(type !== undefined && {key: type})}, poll, wireExpectation(expected));
    operationResult(response, "console message assertion", new Error().stack);
    return parseJson(response.observedJson) as ConsoleMessage;
  }
  async setDeviceMetrics(metrics: DeviceMetrics, options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "SET_DEVICE_METRICS", device: {width: metrics.width, height: metrics.height, deviceScaleFactor: metrics.deviceScaleFactor ?? 1, ...(metrics.mobile !== undefined && {mobile: metrics.mobile})}}, options); }
  async clearDeviceMetrics(options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "CLEAR_DEVICE_METRICS"}, options); }
  async setGeolocation(location: Geolocation, options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "SET_GEOLOCATION", geolocation: {latitude: location.latitude, longitude: location.longitude, ...(location.accuracy !== undefined && {accuracy: location.accuracy})}}, options); }
  async clearGeolocation(options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "CLEAR_GEOLOCATION"}, options); }
  async setPermissions(origin: string, permissions: Readonly<Partial<Record<Permission, PermissionState>>>, options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "SET_PERMISSIONS", origin, permissions: {...permissions} as Record<string, string>}, options); }
  async resetPermissions(options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "RESET_PERMISSIONS"}, options); }
  async setLocale(locale: string, options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "SET_LOCALE", locale}, options); }
  async clearLocale(options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "CLEAR_LOCALE"}, options); }
  async setTimezone(timezone: string, options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "SET_TIMEZONE", timezone}, options); }
  async clearTimezone(options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "CLEAR_TIMEZONE"}, options); }
  async setMedia(media: MediaEmulation, options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "SET_MEDIA", media: {...(media.type !== undefined && {type: media.type}), ...(media.colorScheme !== undefined && {colorScheme: media.colorScheme}), ...(media.reducedMotion !== undefined && {reducedMotion: media.reducedMotion})}}, options); }
  async clearMedia(options: CommandOptions = {}): Promise<void> { await this.#lifecycleCommand({kind: "CLEAR_MEDIA"}, options); }

  async #listHandles(kind: "tabs" | "frames", spawnedOnly: boolean, options: CommandOptions): Promise<readonly Session[]> {
    this.#assertOpen();
    assertCancellationOptions(options, kind);
    const request = {sessionId: this.id, ...(spawnedOnly && {spawnedOnly: true})};
    const response = kind === "tabs" ? await this.#transport.listTabs(request, options) : await this.#transport.listFrames(request, options);
    await this.#flushWarnings();
    return response.handles.map(this.#registerSibling);
  }
  async #waitForHandle(kind: "tab" | "frame", query: TabQuery, options: PollOptions): Promise<Session> {
    this.#assertOpen();
    assertPollOptions(options, `waitFor${kind === "tab" ? "Tab" : "Frame"}`);
    const request = {sessionId: this.id, query: wireTabQuery(query), poll: pollPolicy(options)};
    const transportOptions = {...options, ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100})};
    const response = kind === "tab" ? await this.#transport.waitForTab(request, transportOptions) : await this.#transport.waitForFrame(request, transportOptions);
    await this.#flushWarnings();
    return this.#registerSibling(response);
  }
  async lifecycleOperation(operation: WireLifecycleOperation, options: WaitOptions, expectation?: WireExpectation): Promise<DriverOperationResult> {
    this.#assertOpen();
    assertPollOptions(options, "lifecycle operation");
    const result = await this.#transport.lifecycle({sessionId: this.id, operation, ...(expectation && {expectation}), poll: pollPolicy(options)}, {...options, ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100})});
    await this.#flushWarnings();
    return result;
  }
  async #lifecycleValue<T>(operation: WireLifecycleOperation, options: CommandOptions): Promise<T> {
    assertCancellationOptions(options, "lifecycle state read");
    const response = await this.lifecycleOperation(operation, {...options, mode: "immediate"});
    operationResult(response, "lifecycle state read", new Error().stack);
    return parseJson(response.observedJson) as T;
  }
  async #lifecycleCommand(operation: WireLifecycleOperation, options: CommandOptions): Promise<void> {
    assertCancellationOptions(options, "lifecycle command");
    operationResult(await this.lifecycleOperation(operation, {...options, mode: "immediate"}), "lifecycle command", new Error().stack);
  }

  invalidate(): void {
    if (this.closed) return;
    this.closed = true;
    this.#clearCallbacks();
    this.#clearLiveListeners();
    this.#warningCount = 0;
    this.#onClose();
  }

  async close(): Promise<void> {
    if (this.closed) return;
    const response = await this.#transport.closeSession({sessionId: this.id});
    this.closed = true;
    this.#clearCallbacks();
    this.#clearLiveListeners();
    this.#warningCount = 0;
    this.#invalidateSessions(response.invalidatedSessionIds ?? []);
    this.#onClose();
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

  async handleDialogs(type: import("../index.js").DialogType, options: DialogHandlerOptions = {}): Promise<DialogHandler> {
    const result = await this.eventfulValue<{id: string}>({kind: "REGISTER_DIALOG_HANDLER", dialogType: type, ...(options.message !== undefined && {message: wireExpectation(options.message)}), ...(options.accept !== undefined && {accept: options.accept}), ...(options.promptText !== undefined && {promptText: options.promptText})}, {});
    return new ClientDialogHandler(this.id, result.id, this.#transport);
  }
  async dialogs(query: DialogQuery = {}, options: CommandOptions = {}): Promise<readonly Dialog[]> { return await this.eventfulValue({kind: "DIALOGS", ...eventfulQuery(query)}, options); }
  async warnings(options: CommandOptions = {}): Promise<readonly BilobaWarning[]> { return await this.eventfulValue({kind: "WARNINGS"}, options); }
  async downloads(query: DownloadQuery = {}, options: CommandOptions = {}): Promise<readonly Download[]> { return (await this.eventfulValue<readonly DownloadRecord[]>({kind: "DOWNLOADS", ...eventfulQuery(query)}, options)).map((download) => new ClientDownload(this.id, download, this.#transport)); }
  async expectDownload(query: DownloadQuery = {}, options: PollOptions = {}): Promise<Download> { return new ClientDownload(this.id, await this.eventfulValue<DownloadRecord>({kind: "WAIT_FOR_DOWNLOAD", ...eventfulQuery(query)}, options, true), this.#transport); }
  async waitForDownload(query: DownloadQuery = {}, options: PollOptions = {}): Promise<Download> { return await this.expectDownload(query, options); }
  async downloadContent(id: string, options: CommandOptions & {maxBytes?: number} = {}): Promise<Uint8Array> { const result = await this.eventfulValue<{bodyBase64: string}>({kind: "DOWNLOAD_CONTENT", id, ...(options.maxBytes !== undefined && {maxBodyBytes: options.maxBytes})}, options); return decodeBytes(result.bodyBase64); }
  async cancelDownload(id: string, options: CommandOptions = {}): Promise<void> { await this.eventfulValue({kind: "CANCEL_DOWNLOAD", id}, options); }
  async requests(query: RequestQuery = {}, options: CommandOptions = {}): Promise<readonly NetworkRequest[]> { return (await this.eventfulValue<readonly WireNetworkRecord[]>({kind: "REQUESTS", ...eventfulQuery(query)}, options)).map(networkRequest); }
  async waitForRequest(query: RequestQuery, options: PollOptions = {}): Promise<NetworkRequest> { return networkRequest(await this.eventfulValue<WireNetworkRecord>({kind: "WAIT_FOR_REQUEST", ...eventfulQuery(query)}, options, true)); }
  async responses(query: ResponseQuery = {}, options: CommandOptions = {}): Promise<readonly NetworkResponse[]> { return (await this.eventfulValue<readonly WireNetworkRecord[]>({kind: "RESPONSES", ...eventfulQuery(query)}, options)).map(networkResponse); }
  async expectNetworkIdle(options: PollOptions = {}): Promise<AssertionResult> { return await this.eventfulValue({kind: "WAIT_FOR_NETWORK_IDLE"}, options, true); }
  async waitForNetworkIdle(options: PollOptions = {}): Promise<void> { await this.expectNetworkIdle(options); }
  async stubRequest(query: ExpectedValue, response: FulfillResponse, options: NetworkHandlerOptions = {}): Promise<NetworkHandler> { return await this.registerNetworkHandler(query, "fulfill", response, options); }
  async fulfill(query: ExpectedValue, response: FulfillResponse, options: NetworkHandlerOptions = {}): Promise<NetworkHandler> { return await this.stubRequest(query, response, options); }
  async abortRequest(query: ExpectedValue, options: NetworkHandlerOptions = {}): Promise<NetworkHandler> { return await this.registerNetworkHandler(query, "abort", {}, options); }
  async abortRequests(query: ExpectedValue, options: NetworkHandlerOptions = {}): Promise<NetworkHandler> { return await this.abortRequest(query, options); }
  async modifyRequest(query: ExpectedValue, override: RequestOverride, options: NetworkHandlerOptions = {}): Promise<NetworkHandler> { return await this.registerNetworkHandler(query, "request", override, options); }
  async modifyResponse(query: ExpectedValue, override: ResponseOverride, options: NetworkHandlerOptions = {}): Promise<NetworkHandler> { return await this.registerNetworkHandler(query, "response", override, options); }
  async routeResponse(query: ExpectedValue, callback: (response: InterceptedResponse) => ResponseOverride | Promise<ResponseOverride>, options: NetworkHandlerOptions = {}): Promise<NetworkHandler> {
    const callbackId = `${this.id}-callback-${++callbackSequence}`;
    this.#transport.registerResponseCallback(callbackId, async (payload) => wireNetworkOverride(await callback(interceptedResponse(payload))));
    this.#callbackIDs.add(callbackId);
    try { return await this.registerNetworkHandler(query, "callback", {}, options, callbackId); }
    catch (error) { this.#transport.removeResponseCallback(callbackId); this.#callbackIDs.delete(callbackId); throw error; }
  }
  async networkShadowDiagnostics(options: CommandOptions = {}): Promise<readonly NetworkShadowDiagnostic[]> { return await this.eventfulValue({kind: "NETWORK_SHADOWS"}, options); }
  async holdResponse(expectedUrl: ExpectedValue, options: CommandOptions & {limit?: number; maxBodyBytes?: number} = {}): Promise<ResponseHold> {
    assertOptionKeys(options, "holdResponse", new Set(["signal", "limit", "maxBodyBytes"]));
    const callsite = new Error().stack ?? "";
    const result = await this.eventfulValue<{id: string}>({kind: "HOLD_RESPONSE", url: wireExpectation(expectedUrl), callsite, ...(options.limit !== undefined && {limit: options.limit}), ...(options.maxBodyBytes !== undefined && {maxBodyBytes: options.maxBodyBytes})}, options);
    return new ClientResponseHold(this.id, result.id, this.#transport);
  }
  async setNetworkState(state: NetworkState, options: CommandOptions = {}): Promise<void> { await this.eventfulValue({kind: "SET_NETWORK_STATE", network: definedEntries(state)}, options); }
  async setOffline(offline = true, options: CommandOptions = {}): Promise<void> { await this.setNetworkState({...(await this.networkState(options)), offline}, options); }
  async resetNetworkState(options: CommandOptions = {}): Promise<void> { await this.setNetworkState({}, options); }
  async networkState(options: CommandOptions = {}): Promise<NetworkState> { return await this.eventfulValue({kind: "NETWORK_STATE"}, options); }
  async setCacheEnabled(enabled: boolean, options: CommandOptions = {}): Promise<void> { await this.eventfulValue({kind: "SET_CACHE_ENABLED", cacheEnabled: enabled}, options); }

  async registerNetworkHandler(query: ExpectedValue, action: string, override: RequestOverride | ResponseOverride, options: NetworkHandlerOptions, callbackId?: string): Promise<NetworkHandler> {
    const callsite = new Error().stack ?? "";
    const result = await this.eventfulValue<{id: string}>({kind: "REGISTER_NETWORK_HANDLER", url: wireExpectation(query), action, callsite, override: wireNetworkOverride(override), ...(callbackId !== undefined && {callbackId}), ...(options.maxBodyBytes !== undefined && {maxBodyBytes: options.maxBodyBytes}), ...(options.timeoutMs !== undefined && {transformTimeoutMs: options.timeoutMs})}, {});
    return new ClientNetworkHandler(this.id, result.id, this.#transport, callbackId, (id) => this.#callbackIDs.delete(id));
  }

  async eventfulValue<T = unknown>(operation: import("../generated/protocol.js").EventfulOperation, options: WaitOptions, polling = false): Promise<T> {
    this.#assertOpen();
    const result = await this.#transport.eventful<T>({sessionId: this.id, operation, ...(polling && {poll: pollPolicy(options)})}, polling ? {...options, ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100})} : options);
    if (operation.kind !== "WARNINGS") await this.#flushWarnings();
    return result;
  }
  async #flushWarnings(): Promise<void> {
    if (!this.#warningSink) return;
    if (this.#warningFlush) return await this.#warningFlush;
    this.#warningFlush = (async () => { const warnings = await this.#transport.eventful<readonly Warning[]>({sessionId: this.id, operation: {kind: "WARNINGS"}}, {}); for (const warning of warnings.slice(this.#warningCount)) this.#warningSink?.(warning); this.#warningCount = warnings.length; })();
    try { await this.#warningFlush; } finally { this.#warningFlush = undefined; }
  }
  #clearCallbacks(): void { for (const id of this.#callbackIDs) this.#transport.removeResponseCallback(id); this.#callbackIDs.clear(); }

  #addLiveListener(kind: "console", listener: (message: ConsoleMessage) => void): () => void;
  #addLiveListener(kind: "warning", listener: (warning: BilobaWarning) => void): () => void;
  #addLiveListener(kind: "console" | "warning", listener: ((message: ConsoleMessage) => void) | ((warning: BilobaWarning) => void)): () => void {
    this.#assertOpen();
    const listeners = kind === "console" ? this.#consoleListeners : this.#warningListeners;
    (listeners as Set<typeof listener>).add(listener);
    if (!this.#liveSubscriptions.has(kind)) {
      const pending = (this.#liveTeardown.get(kind) ?? Promise.resolve()).then(async () => await this.#transport.subscribeEvents({sessionId: this.id, types: [kind]})).then(({subscriptionId}) => {
        this.#transport.registerEventListener(subscriptionId, (event, envelope) => {
          if ((envelope.generation ?? 0) < this.#eventGeneration) return;
          this.#eventGeneration = Math.max(this.#eventGeneration, envelope.generation ?? 0);
          const current = kind === "console" ? this.#consoleListeners : this.#warningListeners;
          if (event === "eventsDropped") {
            const warning = envelope.payload as BilobaWarning;
            for (const callback of this.#warningListeners) { try { callback(warning); } catch { /* listeners are isolated */ } }
            return;
          }
          for (const callback of current as Set<(value: unknown) => void>) { try { callback(envelope.payload); } catch { /* listeners are isolated */ } }
        });
        return subscriptionId;
      });
      this.#liveSubscriptions.set(kind, pending);
    }
    let removed = false;
    return () => {
      if (removed) return; removed = true; (listeners as Set<typeof listener>).delete(listener);
      if (listeners.size === 0) void this.#removeLiveSubscription(kind);
    };
  }

  async #removeLiveSubscription(kind: "console" | "warning"): Promise<void> {
    const pending = this.#liveSubscriptions.get(kind); if (!pending) return; this.#liveSubscriptions.delete(kind);
    const teardown = (async () => { try { const id = await pending; this.#transport.removeEventListener(id); await this.#transport.unsubscribeEvents({subscriptionId: id}); } catch { /* teardown is best effort */ } })();
    this.#liveTeardown.set(kind, teardown);
    await teardown;
    if (this.#liveTeardown.get(kind) === teardown) this.#liveTeardown.delete(kind);
  }

  #clearLiveListeners(): void { this.#consoleListeners.clear(); this.#warningListeners.clear(); void this.#removeLiveSubscription("console"); void this.#removeLiveSubscription("warning"); }

  async expectURL(expected: ExpectedValue, options: PollOptions = {}): Promise<AssertionResult> { return assertionResult(await this.lifecycleOperation({kind: "URL"}, options, wireExpectation(expected)), new Error().stack); }
  async url(options: CommandOptions = {}): Promise<string> { return await this.#lifecycleValue<string>({kind: "URL"}, options); }

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
      await this.#flushWarnings();
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
    const result = await this.#transport.dom({
      sessionId: this.id,
      operation,
      ...(expectation && {expectation}),
      poll: pollPolicy(options),
    } satisfies DOMRequest, {
      ...options,
      ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100}),
    });
    await this.#flushWarnings();
    return result;
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
    await this.#flushWarnings();
  }

  async setUpload(
    locator: WireLocator,
    paths: readonly string[],
    options: WaitOptions,
  ): Promise<DriverOperationResult> {
    this.#assertOpen();
    const result = await this.#transport.setUpload({
      sessionId: this.id,
      locator,
      paths: [...paths],
      poll: pollPolicy(options),
    } satisfies SetUploadRequest, {
      ...options,
      ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100}),
    });
    await this.#flushWarnings();
    return result;
  }

  async dragTo(
    source: WireLocator,
    target: WireLocator,
    options: WaitOptions,
    realistic = false,
  ): Promise<DriverOperationResult> {
    this.#assertOpen();
    const result = await this.#transport.dragTo({
      sessionId: this.id,
      source,
      target,
    ...(realistic && {realistic: true}),
      poll: pollPolicy(options),
    } satisfies DragToRequest, {
      ...options,
      ...(options.timeoutMs !== undefined && {deadlineMs: options.timeoutMs + 2_100}),
    });
    await this.#flushWarnings();
    return result;
  }

  #assertOpen(): void {
    if (this.closed) throw new BilobaError({code: "DRIVER_CLOSED", message: `Biloba session ${this.id} is closed`});
  }
}

class ClientStorage implements BrowserStorage {
  constructor(private readonly session: ClientSession, private readonly area: StorageArea) {}
  async set(key: string, value: SerializableValue, options: CommandOptions = {}): Promise<void> { operationResult(await this.session.lifecycleOperation({kind: "STORAGE_SET", area: this.area, key, valueJson: JSON.stringify(value)}, {...options, mode: "immediate"}), "storage set", new Error().stack); }
  async get<T = unknown>(key: string, options: CommandOptions = {}): Promise<StorageItem<T>> { const response = await this.session.lifecycleOperation({kind: "STORAGE_GET", area: this.area, key}, {...options, mode: "immediate"}); operationResult(response, "storage get", new Error().stack); const item = parseJson(response.observedJson) as {found: boolean; value?: T}; return item.found ? {found: true, value: item.value as T} : {found: false}; }
  async getAll(options: CommandOptions = {}): Promise<Readonly<Record<string, unknown>>> { const response = await this.session.lifecycleOperation({kind: "STORAGE_GET_ALL", area: this.area}, {...options, mode: "immediate"}); return parseJson(response.observedJson) as Readonly<Record<string, unknown>>; }
  async remove(key: string, options: CommandOptions = {}): Promise<void> { operationResult(await this.session.lifecycleOperation({kind: "STORAGE_REMOVE", area: this.area, key}, {...options, mode: "immediate"}), "storage remove", new Error().stack); }
  async clear(options: CommandOptions = {}): Promise<void> { operationResult(await this.session.lifecycleOperation({kind: "STORAGE_CLEAR", area: this.area}, {...options, mode: "immediate"}), "storage clear", new Error().stack); }
  async length(options: CommandOptions = {}): Promise<number> { const response = await this.session.lifecycleOperation({kind: "STORAGE_LENGTH", area: this.area}, {...options, mode: "immediate"}); return parseJson(response.observedJson) as number; }
  async expectItem<T = unknown>(key: string, expected: ExpectedValue = {kind: "anything"}, options: PollOptions = {}): Promise<T> { const response = await this.session.lifecycleOperation({kind: "STORAGE_GET", area: this.area, key}, options, wireExpectation(expected)); operationResult(response, "storage item assertion", new Error().stack); return parseJson(response.observedJson) as T; }
  async expectLength(expected: number | Expectation, options: PollOptions = {}): Promise<AssertionResult> { return assertionResult(await this.session.lifecycleOperation({kind: "STORAGE_LENGTH", area: this.area}, options, wireExpectation(expected)), new Error().stack); }
}

class ClientResponseHold implements ResponseHold {
  constructor(
    private readonly sessionId: string,
    private readonly holdId: string,
    private readonly transport: DriverTransport,
  ) {}

  async await(options: WaitingCommandOptions = {}): Promise<HeldResponse> {
    assertWaitingCommandOptions(options, "responseHold.await");
    const response = await this.transport.eventful<{id: string; url: string; status: number; headers: readonly HeaderEntry[]; bodyBase64: string}>({sessionId: this.sessionId, operation: {kind: "AWAIT_RESPONSE_HOLD", id: this.holdId}}, options);
    return {id: response.id, url: response.url, status: response.status, headers: response.headers, headerMap: headerMap(response.headers), body: decodeBytes(response.bodyBase64)};
  }
  async release(options?: CommandOptions): Promise<void>;
  async release(response: HeldResponse | string, options?: CommandOptions): Promise<void>;
  async release(responseOrOptions: HeldResponse | string | CommandOptions = {}, options: CommandOptions = {}): Promise<void> {
    if (typeof responseOrOptions === "string" || "id" in responseOrOptions) { await this.releaseResponse(typeof responseOrOptions === "string" ? responseOrOptions : responseOrOptions.id, options); return; }
    await this.releaseAll(responseOrOptions);
  }
  async releaseResponse(responseId: string, options: CommandOptions = {}): Promise<void> { await this.transport.eventful({sessionId: this.sessionId, operation: {kind: "RELEASE_HELD_RESPONSE", id: this.holdId, responseId}}, options); }
  async releaseNext(options: CommandOptions = {}): Promise<void> { await this.transport.eventful({sessionId: this.sessionId, operation: {kind: "RELEASE_NEXT_RESPONSE", id: this.holdId}}, options); }
  async stats(options: CommandOptions = {}): Promise<ResponseHoldStats> { return await this.transport.eventful<ResponseHoldStats>({sessionId: this.sessionId, operation: {kind: "RESPONSE_HOLD_STATS", id: this.holdId}}, options); }

  async releaseAll(options: CommandOptions = {}): Promise<void> {
    assertCancellationOptions(options, "responseHold.release");
    await this.transport.eventful({sessionId: this.sessionId, operation: {kind: "RELEASE_RESPONSE_HOLD", id: this.holdId}}, options);
  }
}

class ClientDialogHandler implements DialogHandler {
  constructor(readonly sessionId: string, readonly id: string, private readonly transport: DriverTransport) {}
  async remove(options: CommandOptions = {}): Promise<void> { await this.transport.eventful({sessionId: this.sessionId, operation: {kind: "REMOVE_DIALOG_HANDLER", id: this.id}}, options); }
}

type DownloadRecord = Omit<Download, "content" | "cancel">;
class ClientDownload implements Download {
  readonly id; readonly url; readonly filename; readonly state; readonly receivedBytes; readonly totalBytes; readonly startedAt; readonly completedAt?: number;
  constructor(private readonly sessionId: string, value: DownloadRecord, private readonly transport: DriverTransport) { this.id = value.id; this.url = value.url; this.filename = value.filename; this.state = value.state; this.receivedBytes = value.receivedBytes; this.totalBytes = value.totalBytes; this.startedAt = value.startedAt; if (value.completedAt !== undefined) this.completedAt = value.completedAt; }
  async content(options: CommandOptions & {maxBytes?: number} = {}): Promise<Uint8Array> { const result = await this.transport.eventful<{bodyBase64: string}>({sessionId: this.sessionId, operation: {kind: "DOWNLOAD_CONTENT", id: this.id, ...(options.maxBytes !== undefined && {maxBodyBytes: options.maxBytes})}}, options); return decodeBytes(result.bodyBase64); }
  async cancel(options: CommandOptions = {}): Promise<void> { await this.transport.eventful({sessionId: this.sessionId, operation: {kind: "CANCEL_DOWNLOAD", id: this.id}}, options); }
}

class ClientNetworkHandler implements NetworkHandler {
  constructor(readonly sessionId: string, readonly id: string, private readonly transport: DriverTransport, private readonly callbackId?: string, private readonly onCallbackRemoved?: (id: string) => void) {}
  async stats(options: CommandOptions = {}): Promise<NetworkHandlerStats> { return await this.transport.eventful<NetworkHandlerStats>({sessionId: this.sessionId, operation: {kind: "NETWORK_HANDLER_STATS", id: this.id}}, options); }
  async count(options: CommandOptions = {}): Promise<number> { return (await this.stats(options)).count; }
  async remove(options: CommandOptions = {}): Promise<void> { await this.transport.eventful({sessionId: this.sessionId, operation: {kind: "REMOVE_NETWORK_HANDLER", id: this.id}}, options); if (this.callbackId) { this.transport.removeResponseCallback(this.callbackId); this.onCallbackRemoved?.(this.callbackId); } }
}

function eventfulQuery(query: DialogQuery | DownloadQuery | RequestQuery | ResponseQuery): Record<string, unknown> {
  const result: Record<string, unknown> = {};
  for (const [name, value] of Object.entries(query)) {
    if (value === undefined) continue;
    if (name === "content" && value instanceof Uint8Array) { result.contentBase64 = encodeBinaryBody(value); continue; }
    result[name] = typeof value === "string" && name === "type" ? value : wireExpectation(value as ExpectedValue);
  }
  return result;
}
function wireNetworkOverride(value: RequestOverride | ResponseOverride): import("../generated/protocol.js").NetworkOverride {
  const {body, headers, ...rest} = value;
  return definedEntries({...rest, ...(headers !== undefined && {headers: [...headers]}), ...(body !== undefined && {bodyBase64: encodeBinaryBody(body)})}) as import("../generated/protocol.js").NetworkOverride;
}
const maxDecodedBodySize = 16 * 1024 * 1024;
export function resolveUpdateScreenshots(explicit: boolean | undefined, raw: string | undefined, warn: (message: string) => void): boolean {
  if (explicit !== undefined) return explicit;
  if (raw === undefined || raw.trim() === "") return false;
  const normalized = raw.trim().toLowerCase();
  if (["1", "t", "true", "y", "yes", "on"].includes(normalized)) return true;
  if (["0", "f", "false", "n", "no", "off"].includes(normalized)) return false;
  warn(`BILOBA_UPDATE_SCREENSHOTS has unrecognized value ${JSON.stringify(raw)}; screenshots will not be updated`);
  return false;
}
export function resolveVisualConnectOptions(
  options: Pick<ConnectOptions, "artifactDir" | "screenshotBaselinesDir" | "updateScreenshots" | "screenshotPixelTolerance" | "screenshotChannelTolerance" | "maxScreenshotBytes">,
  environment: Readonly<Record<string, string | undefined>>,
  warn: (message: string) => void,
): Required<Pick<ConnectOptions, "artifactDir" | "screenshotBaselinesDir" | "updateScreenshots" | "screenshotPixelTolerance" | "screenshotChannelTolerance" | "maxScreenshotBytes">> {
  const maxBytes = options.maxScreenshotBytes ?? maxScreenshotBytes;
  const pixelTolerance = options.screenshotPixelTolerance ?? 0;
  const channelTolerance = options.screenshotChannelTolerance ?? 0;
  if (!Number.isSafeInteger(maxBytes) || maxBytes <= 0 || maxBytes > maxScreenshotBytes) throw new BilobaError({code: "INVALID_ARGUMENT", message: `maxScreenshotBytes must be a positive integer no greater than ${maxScreenshotBytes}`});
  if (!Number.isFinite(pixelTolerance) || pixelTolerance < 0 || pixelTolerance > 1) throw new BilobaError({code: "INVALID_ARGUMENT", message: "screenshotPixelTolerance must be between 0 and 1"});
  if (!Number.isInteger(channelTolerance) || channelTolerance < 0 || channelTolerance > 255) throw new BilobaError({code: "INVALID_ARGUMENT", message: "screenshotChannelTolerance must be an integer between 0 and 255"});
  return {
    artifactDir: resolve(options.artifactDir ?? environment.BILOBA_SCREENSHOTS_DIR ?? "biloba-screenshots"),
    screenshotBaselinesDir: resolve(options.screenshotBaselinesDir ?? environment.BILOBA_SCREENSHOT_BASELINES_DIR ?? "biloba-baselines"),
    updateScreenshots: resolveUpdateScreenshots(options.updateScreenshots, environment.BILOBA_UPDATE_SCREENSHOTS, warn),
    screenshotPixelTolerance: pixelTolerance,
    screenshotChannelTolerance: channelTolerance,
    maxScreenshotBytes: maxBytes,
  };
}
export function encodeBinaryBody(value: Uint8Array): string {
  if (value.byteLength > maxDecodedBodySize) throw new BilobaError({code: "INVALID_ARGUMENT", message: `binary body exceeds decoded limit ${maxDecodedBodySize}`});
  return Buffer.from(value.buffer, value.byteOffset, value.byteLength).toString("base64");
}
export function decodeBinaryBody(value: string, limit = maxDecodedBodySize): Uint8Array {
  if (!Number.isSafeInteger(limit) || limit <= 0 || limit > maxDecodedBodySize) throw new BilobaError({code: "INVALID_ARGUMENT", message: `decoded binary limit must be between 1 and ${maxDecodedBodySize}`});
  if (value.length % 4 !== 0) throw new BilobaError({code: "DRIVER_ERROR", message: "daemon returned malformed base64 body"});
  if (Buffer.byteLength(value, "ascii") > Math.ceil(limit / 3) * 4) throw new BilobaError({code: "DRIVER_ERROR", message: `daemon body exceeds decoded limit ${limit}`});
  const decoded = Buffer.from(value, "base64");
  if (decoded.length > limit || decoded.toString("base64") !== value) throw new BilobaError({code: "DRIVER_ERROR", message: decoded.length > limit ? `daemon body exceeds decoded limit ${limit}` : "daemon returned malformed base64 body"});
  return new Uint8Array(decoded);
}
function driverShapeError(message: string): BilobaError { return new BilobaError({code: "DRIVER_ERROR", message}); }
// The daemon computes the same diagnosis Go renders - pixel counts, the amplitude verdict, the shape
// of the changed region - and writes the actual/diff PNGs.  It rode along as an error property,
// where no runner shows it: Vitest prints message and stack, not arbitrary own properties.  So a
// mismatch read as "did not match" and the diff image sat on disk unmentioned.  Fold it into the
// message, which is the part a human and a CI log actually see.
function visualFailureMessage(name: string, visual: VisualResult): string {
  const lines = [`Biloba screenshot ${JSON.stringify(name)} did not match`];
  for (const scheme of visual.schemes) {
    if (scheme.match) continue;
    const heading = scheme.scheme ? `${scheme.scheme} scheme:` : undefined;
    if (heading) lines.push(heading);
    if (scheme.diagnosis) lines.push(scheme.diagnosis);
    for (const [label, path] of [["baseline", scheme.baselinePath], ["actual", scheme.actualPath], ["diff", scheme.diffPath]] as const) {
      if (path) lines.push(`${label}: ${path}`);
    }
  }
  // A comparison that produced no per-scheme detail still has to say where to look.
  if (lines.length === 1) lines.push(...visualArtifactPaths(visual));
  return lines.join("\n");
}

function visualArtifactPaths(visual: VisualResult): readonly string[] { return [...new Set(visual.schemes.flatMap((scheme) => [scheme.baselinePath, scheme.actualPath, scheme.diffPath].filter((path): path is string => Boolean(path))))]; }
function decodeBytes(value: string): Uint8Array { return decodeBinaryBody(value); }
type WireNetworkRecord = {url: string; method?: string; status?: number; headers: readonly HeaderEntry[]; resourceType: string};
function headerMap(headers: readonly HeaderEntry[]): Readonly<Record<string, string>> { return Object.fromEntries(headers.map(({name, value}) => [name, value])); }
function networkRequest(value: WireNetworkRecord): NetworkRequest { return {url: value.url, method: value.method ?? "", headers: value.headers, headerMap: headerMap(value.headers), resourceType: value.resourceType}; }
function networkResponse(value: WireNetworkRecord): NetworkResponse { return {url: value.url, status: value.status ?? 0, headers: value.headers, headerMap: headerMap(value.headers), resourceType: value.resourceType}; }
function interceptedResponse(value: unknown): InterceptedResponse { const response = value as {url: string; status: number; headers: readonly HeaderEntry[]; bodyBase64: string}; return {url: response.url, status: response.status, headers: response.headers, headerMap: headerMap(response.headers), body: decodeBytes(response.bodyBase64)}; }
function definedEntries<T extends object>(value: T): {[K in keyof T]?: Exclude<T[K], undefined>} { return Object.fromEntries(Object.entries(value).filter(([, entry]) => entry !== undefined)) as {[K in keyof T]?: Exclude<T[K], undefined>}; }

class ClientLocator implements Locator {
  readonly #session: ClientSession;
  readonly #locator: WireLocator;
  readonly #realistic: boolean;

  constructor(session: ClientSession, locator: WireLocator, realistic = false) {
    this.#session = session;
    this.#locator = locator;
    this.#realistic = realistic;
  }

  async captureScreenshot(options?: ScreenshotBytesOptions): Promise<Uint8Array>;
  async captureScreenshot(options: ScreenshotPathOptions): Promise<string>;
  async captureScreenshot(options: ScreenshotBytesOptions | ScreenshotPathOptions = {}): Promise<Uint8Array | string> {
    return await this.#session.captureScreenshotTarget(this.#locator, options);
  }

  async expectScreenshot(name: string, options: VisualScreenshotOptions = {}): Promise<VisualResult> {
    return await this.#session.expectScreenshotTarget(this.#locator, name, options);
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

  belongsTo(session: ClientSession): boolean { return this.#session === session; }
}

function wireLocator(locator: Locator | string): WireLocator {
  if (typeof locator === "string") {
    return {kind: "CSS", value: locator, match: "EXACT", first: false};
  }
  if (locator instanceof ClientLocator) return locator.wireLocator();
  throw new BilobaError({code: "INVALID_ARGUMENT", message: "locator belongs to a different Biloba client"});
}

function wireScreenshotSubject(session: ClientSession, locator: Locator | string): WireLocator {
  if (typeof locator === "string") return {kind: "CSS", value: locator, match: "EXACT", first: false};
  if (locator instanceof ClientLocator && locator.belongsTo(session)) return locator.wireLocator();
  throw new BilobaError({code: "INVALID_ARGUMENT", message: "screenshot mask locator belongs to a different session"});
}

function wireTabQuery(query: TabQuery): import("../generated/protocol.js").TabQueryRequest {
  return {
    ...(query.title !== undefined && {title: wireExpectation(query.title)}),
    ...(query.url !== undefined && {url: wireExpectation(query.url)}),
    ...(query.has !== undefined && {has: wireLocator(query.has)}),
  };
}

function wireCookieQuery(query: CookieQuery): import("../generated/protocol.js").CookieQuery {
  return {
    ...(query.name !== undefined && {name: wireExpectation(query.name)}),
    ...(query.value !== undefined && {value: wireExpectation(query.value)}),
    ...(query.domain !== undefined && {domain: wireExpectation(query.domain)}),
    ...(query.path !== undefined && {path: wireExpectation(query.path)}),
    ...(query.sameSite !== undefined && {sameSite: wireExpectation(query.sameSite)}),
    ...(query.secure !== undefined && {secure: query.secure}),
    ...(query.httpOnly !== undefined && {httpOnly: query.httpOnly}),
  };
}

function cookieFromWire(cookie: import("../generated/protocol.js").Cookie): Cookie {
  return {
    name: cookie.name,
    value: cookie.value,
    ...(cookie.domain !== undefined && {domain: cookie.domain}),
    ...(cookie.path !== undefined && {path: cookie.path}),
    ...(cookie.expiresUnix !== undefined && cookie.expiresUnix !== 0 && {expires: new Date(cookie.expiresUnix * 1_000)}),
    ...(cookie.secure !== undefined && {secure: cookie.secure}),
    ...(cookie.httpOnly !== undefined && {httpOnly: cookie.httpOnly}),
    ...(cookie.sameSite !== undefined && {sameSite: cookie.sameSite}),
    ...(cookie.session !== undefined && {session: cookie.session}),
  };
}

function currentCookieMatches(cookie: Cookie, query: CookieQuery): boolean {
  for (const [expected, observed] of [[query.name, cookie.name], [query.value, cookie.value], [query.domain, cookie.domain ?? ""], [query.path, cookie.path ?? ""], [query.sameSite, cookie.sameSite ?? ""]] as const) {
    if (expected !== undefined && !currentValueMatches(observed, expected)) return false;
  }
  return (query.secure === undefined || cookie.secure === query.secure) && (query.httpOnly === undefined || cookie.httpOnly === query.httpOnly);
}

function currentValueMatches(observed: string, expected: ExpectedValue): boolean {
  if (expected instanceof RegExp) return expected.test(observed);
  if (!isExpectation(expected)) return observed === expected;
  switch (expected.kind) {
    case "equal": return observed === expected.expected;
    case "contains": return observed.includes(expected.expected);
    case "prefix": return observed.startsWith(expected.expected);
    case "suffix": return observed.endsWith(expected.expected);
    case "regexp": return new RegExp(expected.expected).test(observed);
    case "anything": return true;
    case "not": return !currentValueMatches(observed, expected.child);
    case "all": return expected.children.every((child) => currentValueMatches(observed, child));
    case "any": return expected.children.some((child) => currentValueMatches(observed, child));
    default: return false;
  }
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
function assertOptionKeys(options: object, operation: string, allowed: ReadonlySet<string>): void { for (const key of Object.keys(options)) if (!allowed.has(key)) throw new BilobaError({code: "INVALID_ARGUMENT", message: `${operation} received unsupported option ${key}`}); }

function assertWaitingCommandOptions(options: WaitingCommandOptions, operation: string): void {
  for (const key of Object.keys(options)) {
    if (key !== "signal" && key !== "timeoutMs") {
      throw new BilobaError({code: "INVALID_ARGUMENT", message: `${operation} only accepts timeout and cancellation options`});
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
  options: Pick<ConnectOptions, "signal" | "warningSink" | "maxScreenshotBytes" | "onScreenshotWarning" | "debugLog" | "diagnostics"> = {},
  stopDaemon?: () => Promise<void>,
): Promise<Browser> {
  try {
    const handshake = await transport.handshake(
      {protocolVersion: "2"},
      options.signal ? {signal: options.signal} : {},
    );
    if (handshake.protocolVersion !== "2") {
      throw new BilobaError({
        code: "PROTOCOL_MISMATCH",
        message: `Biloba protocol mismatch: client 2, daemon ${handshake.protocolVersion}`,
      });
    }
    return new ClientBrowser(
      transport,
      handshake.protocolVersion ?? "",
      handshake.capabilities ?? [],
      launchFromWire(handshake.launch),
      stopDaemon,
      options.warningSink,
      {maxBytes: options.maxScreenshotBytes ?? maxScreenshotBytes, ...(options.onScreenshotWarning && {warningSink: options.onScreenshotWarning})},
      options.debugLog,
      resolveDiagnosticsPolicy(options.diagnostics ?? {}, process.env, automationDetected(process.env)),
    );
  } catch (error) {
    transport.close();
    await stopDaemon?.();
    throw error;
  }
}

export function launchFromWire(value: import("../generated/protocol.js").LaunchMetadata): LaunchMetadata {
  const invalid = (message: string): never => { throw new BilobaError({code: "DRIVER_ERROR", message: `bilobad returned invalid launch metadata: ${message}`}); };
  if (!value || !Array.isArray(value.chromeArgs) || !value.chromeArgs.every((arg) => typeof arg === "string" && /^--[A-Za-z0-9][A-Za-z0-9-]*(=.*)?$/.test(arg)) || typeof value.attached !== "boolean" || typeof value.autoInstalled !== "boolean") return invalid("invalid arguments or booleans");
  const width = value.width || 1024, height = value.height || 768;
  if (!Number.isInteger(width) || !Number.isInteger(height) || width <= 0 || height <= 0) return invalid("window dimensions must be positive integers");
  const size = {width, height}; const args = [...value.chromeArgs];
  if (value.attached && value.mode === undefined) return {attached: true, source: "external", chromeArgs: [], windowSize: size, autoInstalled: false};
  if (!["headless-shell", "headless", "headful"].includes(value.mode ?? "") || typeof value.executablePath !== "string" || value.executablePath.length === 0) return invalid("resolved launch fields are incomplete");
  if (value.attached) return {mode: value.mode as import("../index.js").ChromeMode, executablePath: value.executablePath, chromeArgs: args, windowSize: size, autoInstalled: value.autoInstalled, attached: true, source: "shared-host"};
  return {mode: value.mode as import("../index.js").ChromeMode, executablePath: value.executablePath, chromeArgs: args, windowSize: size, autoInstalled: value.autoInstalled, attached: false};
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
      ...(diagnostics.context && {diagnostics: diagnosticsFromWire(diagnostics.context)}),
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

function diagnosticsFromWire(value: import("../generated/protocol.js").ContextDiagnosticsResponse): ContextDiagnostics {
  return {purpose: value.purpose as ContextDiagnostics["purpose"], ...(value.artifactDir && {artifactDir: value.artifactDir}), tabs: value.tabs.map((tab) => ({...(tab.sessionId && {sessionId: tab.sessionId}), targetId: tab.targetId, title: tab.title, ...(tab.screenshotPath && {screenshotPath: tab.screenshotPath}), ...(tab.screenshotBase64 && {screenshot: decodeBinaryBody(tab.screenshotBase64)}), ...(tab.outlinePath && {outlinePath: tab.outlinePath}), ...(tab.domOutline && {domOutline: tab.domOutline}), errors: tab.errors as import("../index.js").DiagnosticsArtifactError[]}))};
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

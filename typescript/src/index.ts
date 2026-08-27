import {connectWithTransport, stripStackHeader} from "./internal/client.js";
import {
  startSharedBrowser as spawnSharedBrowser,
  type SharedBrowserProcess,
  type StartSharedBrowserOptions,
} from "./internal/browser-manager.js";
import {
  startDaemon as spawnDaemon,
  type DaemonProcess,
  type StartDaemonOptions,
} from "./internal/daemon-manager.js";

export type {DaemonProcess, StartDaemonOptions};
export type {SharedBrowserProcess, StartSharedBrowserOptions};

export const Keys = {
  Enter: "\r",
  Escape: "\x1b",
} as const;

export type SerializableValue =
  | null
  | boolean
  | number
  | string
  | readonly SerializableValue[]
  | {readonly [key: string]: SerializableValue};

// Option bags the caller *builds* spell their optional members `?: T | undefined` throughout.  The
// project compiles with exactOptionalPropertyTypes, under which a bare `?: T` rejects an explicit
// undefined - so `{timeoutMs: config.timeout}` or `{daemonExecutable: process.env.FOO}` would not
// compile, and every caller threading a maybe-value through would have to write a conditional
// spread.  Types Biloba *returns* (AssertionResult, PollObservation, BilobaError) keep the strict
// form, where distinguishing absent from present-but-undefined is worth having.
export interface WaitOptions {
  timeoutMs?: number | undefined;
  intervalMs?: number | undefined;
  signal?: AbortSignal | undefined;
  mode?: "eventually" | "immediate" | "consistently" | undefined;
}

export type NumericOperator = "=" | "==" | "!=" | ">" | ">=" | "<" | "<=";

export type Expectation =
  | {readonly kind: "equal"; readonly expected: SerializableValue}
  | {readonly kind: "contains" | "prefix" | "suffix"; readonly expected: string}
  | {readonly kind: "regexp"; readonly expected: string}
  | {readonly kind: "number"; readonly operator: NumericOperator; readonly expected: number}
  | {readonly kind: "empty" | "anything"}
  | {readonly kind: "all" | "any"; readonly children: readonly Expectation[]}
  | {readonly kind: "not"; readonly child: Expectation};

export type ExpectedValue = SerializableValue | RegExp | Expectation;

export const equalTo = (expected: SerializableValue): Expectation => ({kind: "equal", expected});
export const contains = (expected: string): Expectation => ({kind: "contains", expected});
export const matches = (expected: RegExp | string): Expectation => ({
  kind: "regexp",
  expected: typeof expected === "string" ? expected : expected.source,
});
export const startsWith = (expected: string): Expectation => ({kind: "prefix", expected});
export const endsWith = (expected: string): Expectation => ({kind: "suffix", expected});
export const numeric = (operator: NumericOperator, expected: number): Expectation => ({kind: "number", operator, expected});
export const empty = (): Expectation => ({kind: "empty"});
export const anything = (): Expectation => ({kind: "anything"});
export const allOf = (...children: readonly Expectation[]): Expectation => ({kind: "all", children});
export const anyOf = (...children: readonly Expectation[]): Expectation => ({kind: "any", children});
export const not = (child: Expectation): Expectation => ({kind: "not", child});

export interface Cookie {
  name: string;
  value: string;
  domain?: string | undefined;
  path?: string | undefined;
  expires?: Date | number | undefined;
  secure?: boolean | undefined;
  httpOnly?: boolean | undefined;
  sameSite?: string | undefined;
}

export interface PollObservation {
  readonly attempt: number;
  readonly elapsedMs: number;
  readonly observed: unknown;
  readonly retryReason?: string;
}

export interface AssertionResult {
  readonly observed: unknown;
  readonly attemptCount: number;
  readonly trajectory: readonly PollObservation[];
  readonly rpcRequestCount: number;
  readonly rpcResponseCount: number;
  readonly elapsedMs?: number;
}

export type BilobaErrorCode =
  | "INVALID_ARGUMENT"
  | "TIMEOUT"
  | "TARGET_NOT_FOUND"
  | "TARGET_NOT_READY"
  /** A navigation landed on an HTTP status the caller did not ask for. Retrying will not change it -
   *  pass `expectedStatus` to `navigate` if the error page is what you meant to test. */
  | "NAVIGATION"
  | "JAVASCRIPT_ERROR"
  | "PROTOCOL_MISMATCH"
  | "DRIVER_CLOSED"
  | "DRIVER_ERROR"
  | "CANCELLED"
  /** The shared Chrome exited or crashed underneath this worker. */
  | "BROWSER_GONE"
  /** This session's page crashed; the browser is fine. Navigate again to recover. */
  | "PAGE_CRASHED";

interface BilobaErrorOptions {
  code: BilobaErrorCode;
  message: string;
  locator?: string;
  expected?: unknown;
  observed?: unknown;
  trajectory?: readonly PollObservation[];
  domOutline?: string;
  screenshotPath?: string;
  daemonDetail?: string;
  rpcRequestCount?: number;
  rpcResponseCount?: number;
  callsiteStack?: string;
}

export class BilobaError extends Error {
  readonly code: BilobaErrorCode;
  readonly locator?: string;
  readonly expected?: unknown;
  readonly observed?: unknown;
  readonly trajectory: readonly PollObservation[];
  readonly domOutline?: string;
  readonly screenshotPath?: string;
  readonly daemonDetail?: string;
  readonly rpcRequestCount?: number;
  readonly rpcResponseCount?: number;

  constructor(options: BilobaErrorOptions) {
    super(options.message);
    this.name = "BilobaError";
    this.code = options.code;
    if (options.locator !== undefined) this.locator = options.locator;
    if (options.expected !== undefined) this.expected = options.expected;
    if (options.observed !== undefined) this.observed = options.observed;
    this.trajectory = options.trajectory ?? [];
    if (options.domOutline !== undefined) this.domOutline = options.domOutline;
    if (options.screenshotPath !== undefined) this.screenshotPath = options.screenshotPath;
    if (options.daemonDetail !== undefined) this.daemonDetail = options.daemonDetail;
    if (options.rpcRequestCount !== undefined) this.rpcRequestCount = options.rpcRequestCount;
    if (options.rpcResponseCount !== undefined) this.rpcResponseCount = options.rpcResponseCount;
    if (options.callsiteStack) {
      this.stack = `${this.name}: ${this.message}\n${stripStackHeader(options.callsiteStack)}`;
    }
  }
}

export interface Locator {
  realistic(): Locator;
  first(): Locator;
  last(): Locator;
  nth(index: number): Locator;
  and(other: Locator | string): Locator;
  or(other: Locator | string): Locator;
  within(scope: Locator | string): Locator;
  notWithin(scope: Locator | string): Locator;
  filter(options: {
    hasText?: string | undefined;
    notHasText?: string | undefined;
    has?: Locator | string | undefined;
    notHas?: Locator | string | undefined;
  }): Locator;
  click(options?: WaitOptions): Promise<void>;
  setValue(value: SerializableValue, options?: WaitOptions): Promise<void>;
  type(keys: string, options?: WaitOptions): Promise<void>;
  setUploadFiles(paths: readonly string[], options?: WaitOptions): Promise<void>;
  dragTo(target: Locator | string, options?: WaitOptions): Promise<void>;
  expectVisible(options?: WaitOptions): Promise<AssertionResult>;
  expectNotVisible(options?: WaitOptions): Promise<AssertionResult>;
  expectExists(options?: WaitOptions): Promise<AssertionResult>;
  expectNotExists(options?: WaitOptions): Promise<AssertionResult>;
  expectEnabled(options?: WaitOptions): Promise<AssertionResult>;
  expectNotEnabled(options?: WaitOptions): Promise<AssertionResult>;
  expectClickable(options?: WaitOptions): Promise<AssertionResult>;
  expectNotClickable(options?: WaitOptions): Promise<AssertionResult>;
  expectText(expected: ExpectedValue, options?: WaitOptions & {exact?: boolean | undefined}): Promise<AssertionResult>;
  expectNotText(expected: ExpectedValue, options?: WaitOptions & {exact?: boolean | undefined}): Promise<AssertionResult>;
  expectCount(expected: number | Expectation, options?: WaitOptions): Promise<AssertionResult>;
  expectAttribute(name: string, expected: ExpectedValue, options?: WaitOptions & {exact?: boolean | undefined}): Promise<AssertionResult>;
  expectNotAttribute(name: string, expected: ExpectedValue, options?: WaitOptions & {exact?: boolean | undefined}): Promise<AssertionResult>;
  expectProperty(name: string, expected: ExpectedValue, options?: WaitOptions & {exact?: boolean | undefined}): Promise<AssertionResult>;
  expectValue(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectAllText(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  text(): Promise<string>;
  count(): Promise<number>;
  getAttribute(name: string): Promise<string | null>;
  getProperty<T = unknown>(name: string): Promise<T>;
  value<T = unknown>(): Promise<T>;
  exists(): Promise<boolean>;
}

export interface Session {
  readonly id: string;
  newTab(): Promise<Session>;
  addInitScript(script: string, options?: WaitOptions): Promise<void>;
  activate(options?: WaitOptions): Promise<void>;
  prepare(): Promise<void>;
  navigate(url: string, options?: WaitOptions): Promise<void>;
  navigateWithStatus(url: string, expectedStatus: number, options?: WaitOptions): Promise<void>;
  setCookies(cookies: readonly Cookie[]): Promise<void>;
  evaluate<T = unknown>(expression: string, args?: readonly SerializableValue[], options?: WaitOptions): Promise<T>;
  evaluateAsync<T = unknown>(expression: string, args?: readonly SerializableValue[], options?: WaitOptions): Promise<T>;
  setWindowSize(width: number, height: number, options?: WaitOptions): Promise<void>;
  sendKeys(keys: string, options?: WaitOptions): Promise<void>;
  close(): Promise<void>;
  locator(css: string): Locator;
  getByTestId(value: string): Locator;
  getByText(value: string, options?: {exact?: boolean | undefined}): Locator;
  getByRole(role: string, options?: {name?: string | undefined; exact?: boolean | undefined}): Locator;
  expectUrl(expected: ExpectedValue, options?: WaitOptions & {exact?: boolean | undefined; pathname?: boolean | undefined}): Promise<AssertionResult>;
  url(): Promise<string>;
  expectEvaluation(expression: string, expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectRequest(expectedUrl: ExpectedValue, options?: WaitOptions & {method?: string | undefined}): Promise<AssertionResult>;
  holdResponse(expectedUrl: ExpectedValue, options?: WaitOptions): Promise<ResponseHold>;
}

export interface HeldResponse {
  readonly url: string;
  readonly status: number;
}

export interface ResponseHold {
  await(options?: WaitOptions): Promise<HeldResponse>;
  release(options?: WaitOptions): Promise<void>;
}

export interface Browser {
  readonly protocolVersion: string;
  readonly capabilities: ReadonlySet<string>;
  openSession(): Promise<Session>;
  close(): Promise<void>;
}

export interface ConnectOptions {
  daemonExecutable?: string | undefined;
  chromePath?: string | undefined;
  chromeWsUrl?: string | undefined;
  artifactDir?: string | undefined;
  signal?: AbortSignal | undefined;
}

export async function connect(options: ConnectOptions = {}): Promise<Browser> {
  const executable = options.daemonExecutable ?? process.env.BILOBA_DAEMON_EXECUTABLE;
  if (!executable) {
    throw new BilobaError({
      code: "INVALID_ARGUMENT",
      message: "connect requires daemonExecutable",
    });
  }
  const daemon = await spawnDaemon({
    executable,
    ...(options.chromePath && {chromePath: options.chromePath}),
    ...(options.chromeWsUrl && {chromeWsUrl: options.chromeWsUrl}),
    ...(options.artifactDir && {artifactDir: options.artifactDir}),
  });
  return await connectWithTransport(daemon.transport, options, () => daemon.stop());
}

export async function startDaemon(options: StartDaemonOptions): Promise<DaemonProcess> {
  return await spawnDaemon(options);
}

export async function startSharedBrowser(options: StartSharedBrowserOptions): Promise<SharedBrowserProcess> {
  return await spawnSharedBrowser(options);
}

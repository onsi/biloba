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
}

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
  first(): Locator;
  click(options?: WaitOptions): Promise<void>;
  setValue(value: SerializableValue, options?: WaitOptions): Promise<void>;
  expectVisible(options?: WaitOptions): Promise<AssertionResult>;
  expectText(expected: string, options?: WaitOptions & {exact?: boolean | undefined}): Promise<AssertionResult>;
  expectCount(expected: number, options?: WaitOptions): Promise<AssertionResult>;
  expectAttribute(name: string, expected: string, options?: WaitOptions & {exact?: boolean | undefined}): Promise<AssertionResult>;
  expectValue(expected: SerializableValue, options?: WaitOptions): Promise<AssertionResult>;
}

export interface Session {
  readonly id: string;
  prepare(): Promise<void>;
  navigate(url: string, options?: WaitOptions): Promise<void>;
  navigateWithStatus(url: string, expectedStatus: number, options?: WaitOptions): Promise<void>;
  setCookies(cookies: readonly Cookie[]): Promise<void>;
  evaluate<T = unknown>(expression: string, args?: readonly SerializableValue[], options?: WaitOptions): Promise<T>;
  close(): Promise<void>;
  locator(css: string): Locator;
  getByTestId(value: string): Locator;
  getByText(value: string, options?: {exact?: boolean | undefined}): Locator;
  getByRole(role: string, options?: {name?: string | undefined; exact?: boolean | undefined}): Locator;
  expectUrl(expected: string, options?: WaitOptions & {exact?: boolean | undefined; pathname?: boolean | undefined}): Promise<AssertionResult>;
  expectEvaluation(expression: string, expected: SerializableValue, options?: WaitOptions): Promise<AssertionResult>;
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

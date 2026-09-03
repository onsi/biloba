// Code generated from the Go protocol definition; DO NOT EDIT.

export type ErrorCode =
  | "INVALID_ARGUMENT"
  | "TIMEOUT"
  | "TARGET_NOT_FOUND"
  | "TARGET_NOT_READY"
  | "JAVASCRIPT_ERROR"
  | "PROTOCOL_MISMATCH"
  | "DRIVER_CLOSED"
  | "DRIVER_ERROR"
  | "CANCELLED"
  | "BROWSER_GONE"
  | "PAGE_CRASHED"
;

export interface Diagnostics {
  locator?: string;
  expected?: string;
  domOutline?: string;
  screenshotPath?: string;
  daemonDetail?: string;
}

export interface ProtocolError {
  code: ErrorCode;
  message: string;
  diagnostics?: Diagnostics;
}

export interface Request {
  id: number;
  method: string;
  params?: unknown;
  timeoutMs?: number;
}

export interface HandshakeRequest {
  protocolVersion: string;
}

export interface HandshakeResponse {
  protocolVersion: string;
  capabilities: string[];
}

export interface OpenSessionResponse {
  sessionId: string;
}

export interface SessionRequest {
  sessionId: string;
}

export interface NavigateRequest {
  sessionId: string;
  url: string;
}

export interface PollOptions {
  timeoutMs?: number;
  intervalMs?: number;
}

export interface Locator {
  kind: "CSS" | "TEST_ID" | "TEXT" | "ROLE";
  value?: string;
  role?: string;
  name?: string;
  match?: "EXACT" | "CONTAINS";
  first: boolean;
}

export interface Cookie {
  name: string;
  value: string;
  domain?: string;
  path?: string;
  expiresUnix?: number;
  secure?: boolean;
  httpOnly?: boolean;
  sameSite?: string;
}

export interface SetCookiesRequest {
  sessionId: string;
  cookies: Cookie[];
}

export interface LocatorRequest {
  sessionId: string;
  locator?: Locator;
  poll?: PollOptions;
}

export interface SetValueRequest {
  sessionId: string;
  locator?: Locator;
  valueJson: string;
  poll?: PollOptions;
}

export interface EvaluateRequest {
  sessionId: string;
  expression: string;
  argumentsJson?: string;
  invoke?: boolean;
}

export interface Assertion {
  kind: "VISIBLE" | "TEXT" | "COUNT" | "ATTRIBUTE" | "VALUE" | "URL" | "EVALUATE";
  locator?: Locator;
  attribute?: string;
  expression?: string;
  expectedString?: string;
  expectedCount?: number;
  expectedJson?: string;
  match?: "EXACT" | "CONTAINS";
}

export interface AssertRequest {
  sessionId: string;
  assertion?: Assertion;
  poll?: PollOptions;
}

export interface PollObservation {
  attempt: number;
  elapsedMs: number;
  observedJson?: string;
  retryReason?: string;
}

export interface Timings {
  startedUnixMs: number;
  elapsedMs: number;
}

export interface OperationResult {
  matched: boolean;
  observedJson?: string;
  attemptCount: number;
  trajectory?: PollObservation[];
  timings: Timings;
  diagnostics?: Diagnostics;
  rpcRequestCount: number;
  rpcResponseCount: number;
}

export interface Response<Result = unknown> {
  id: number;
  result?: Result;
  error?: ProtocolError;
}

export type OpenSessionRequest = Record<string, never>;

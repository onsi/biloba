// Code generated from the Go protocol definition; DO NOT EDIT.

export type ErrorCode =
  | "INVALID_ARGUMENT"
  | "TIMEOUT"
  | "TARGET_NOT_FOUND"
  | "TARGET_NOT_READY"
  | "NAVIGATION"
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
  expectedStatus?: number;
}

export interface PollOptions {
  timeoutMs?: number;
  intervalMs?: number;
  mode?: "EVENTUALLY" | "IMMEDIATE" | "CONSISTENTLY";
}

export interface Locator {
  kind: "CSS" | "TEST_ID" | "TEXT" | "ROLE" | "AND" | "OR";
  value?: string;
  role?: string;
  name?: string;
  match?: "EXACT" | "CONTAINS";
  operands?: Locator[];
  within?: Locator;
  filters?: LocatorFilter[];
  level?: number;
  levelSet?: boolean;
  states?: string[];
  nth?: number;
  nthSet?: boolean;
  first: boolean;
}

export interface LocatorFilter {
  kind: "CONTAINS_TEXT" | "CONTAINS" | "WITHIN";
  value?: string;
  match?: "EXACT" | "CONTAINS";
  selector?: Locator;
  negate?: boolean;
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
  realistic?: boolean;
}

export interface SetValueRequest {
  sessionId: string;
  locator?: Locator;
  valueJson: string;
  poll?: PollOptions;
  realistic?: boolean;
}

export interface TypeRequest {
  sessionId: string;
  locator?: Locator;
  keys: string;
  poll?: PollOptions;
  realistic?: boolean;
}

export interface SendKeysRequest {
  sessionId: string;
  keys: string;
}

export interface SetWindowSizeRequest {
  sessionId: string;
  width: number;
  height: number;
}

export interface SetUploadRequest {
  sessionId: string;
  locator?: Locator;
  paths: string[];
  poll?: PollOptions;
}

export interface DragToRequest {
  sessionId: string;
  source?: Locator;
  target?: Locator;
  poll?: PollOptions;
}

export interface AddInitScriptRequest {
  sessionId: string;
  script: string;
}

export interface HoldResponseRequest {
  sessionId: string;
  expectation?: Expectation;
}

export interface ResponseHoldRequest {
  sessionId: string;
  holdId: string;
}

export interface EvaluateRequest {
  sessionId: string;
  expression: string;
  argumentsJson?: string;
  invoke?: boolean;
  awaitPromise?: boolean;
}

export interface Expectation {
  kind: "EQUAL" | "CONTAINS" | "REGEXP" | "PREFIX" | "SUFFIX" | "NUMBER" | "EMPTY" | "ALL" | "ANY" | "NOT" | "ANYTHING";
  expectedJson?: string;
  operator?: "=" | "==" | "!=" | ">" | ">=" | "<" | "<=";
  children?: Expectation[];
}

export interface Assertion {
  kind: "VISIBLE" | "TEXT" | "COUNT" | "ATTRIBUTE" | "VALUE" | "URL" | "EVALUATE" | "EXISTS" | "ENABLED" | "CLICKABLE" | "PROPERTY" | "ALL_TEXT" | "REQUEST";
  locator?: Locator;
  attribute?: string;
  property?: string;
  method?: string;
  expression?: string;
  expectedString?: string;
  expectedCount?: number;
  expectedJson?: string;
  match?: "EXACT" | "CONTAINS";
  expectation?: Expectation;
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

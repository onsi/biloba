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
  kind: "CSS" | "XPATH" | "TEST_ID" | "TEXT" | "ROLE" | "LABEL" | "PLACEHOLDER" | "ALT_TEXT" | "TITLE" | "AND" | "OR";
  value?: string;
  role?: string;
  name?: string;
  attribute?: string;
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

export interface NameSpec {
  name: string;
  allowMissing?: boolean;
}

export interface Point {
  x: number;
  y: number;
}

export interface DOMOperation {
  kind: "TEXT" | "TEXTS" | "CLASSES" | "CLASSES_FOR_EACH" | "DISTINCT_ATTRIBUTE_COUNT" | "ATTRIBUTES" | "ATTRIBUTES_FOR_EACH" | "JSON_ATTRIBUTE" | "PROPERTIES" | "PROPERTIES_FOR_EACH" | "PROPERTY_FOR_EACH" | "VALUES" | "STATE" | "ALL_STATE" | "SET_PROPERTY" | "FOCUS" | "BLUR" | "HOVER" | "TYPE" | "SEND_KEYS" | "CLICK" | "CLICK_EACH" | "TAP" | "DRAG" | "SCROLL_INTO_VIEW" | "SCROLL_WHEEL" | "SELECT" | "CLEAR_SELECTION" | "INVOKE_METHOD" | "INVOKE_FUNCTION" | "INVOKE_METHOD_FOR_EACH" | "INVOKE_FUNCTION_FOR_EACH" | "BOUNDING_BOX" | "SCROLL_OFFSET" | "OFFSET_WITHIN" | "RELATIVE_BOXES" | "GEOMETRY_RELATION" | "GAP_BETWEEN" | "IN_VIEWPORT" | "DOCUMENT_ORDER" | "COMPUTED_STYLE" | "COMPUTED_STYLE_NUMBER" | "NORMALIZE_COLOR";
  locator?: Locator;
  target?: Locator;
  container?: Locator;
  textMode?: "INNER_TEXT" | "TEXT_CONTENT" | "NORMALIZED_TEXT";
  names?: NameSpec[];
  name?: string;
  valueJson?: string;
  all?: boolean;
  every?: boolean;
  projectName?: string;
  state?: "visible" | "enabled" | "clickable" | "checked" | "focused";
  realistic?: boolean;
  button?: "left" | "right" | "middle";
  clickCount?: number;
  offset?: Point;
  modifiers?: ("Shift" | "Control" | "Alt" | "Meta")[];
  keys?: string;
  topOffset?: number;
  hasTopOffset?: boolean;
  deltaX?: number;
  deltaY?: number;
  substring?: string;
  occurrence?: number;
  start?: number;
  end?: number;
  range?: boolean;
  method?: string;
  expression?: string;
  argumentsJson?: string;
  fully?: boolean;
  relation?: "above" | "below" | "leftOf" | "rightOf" | "encloses" | "overlaps";
}

export interface DOMRequest {
  sessionId: string;
  operation?: DOMOperation;
  expectation?: Expectation;
  poll?: PollOptions;
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
  realistic?: boolean;
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

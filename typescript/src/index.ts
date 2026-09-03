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
  Backspace: "\b",
  Tab: "\t",
  Enter: "\r",
  Escape: "\x1b",
  Space: " ",
  Delete: "\x7f",
  Insert: "\u0407",
  ArrowDown: "\u0301", ArrowLeft: "\u0302", ArrowRight: "\u0303", ArrowUp: "\u0304",
  End: "\u0305", Home: "\u0306", PageDown: "\u0307", PageUp: "\u0308",
  CapsLock: "\u0104", NumLock: "\u010a", ScrollLock: "\u010c",
  ContextMenu: "\u0505", PrintScreen: "\u0608", Pause: "\u0509", Help: "\u0508", Clear: "\u0401",
  F1: "\u0801", F2: "\u0802", F3: "\u0803", F4: "\u0804", F5: "\u0805", F6: "\u0806",
  F7: "\u0807", F8: "\u0808", F9: "\u0809", F10: "\u080a", F11: "\u080b", F12: "\u080c",
  F13: "\u080d", F14: "\u080e", F15: "\u080f", F16: "\u0810", F17: "\u0811", F18: "\u0812",
  F19: "\u0813", F20: "\u0814", F21: "\u0815", F22: "\u0816", F23: "\u0817", F24: "\u0818",
} as const;

export type SerializableValue =
  | null
  | boolean
  | number
  | string
  | readonly SerializableValue[]
  | ValueLabel
  | {readonly [key: string]: SerializableValue};

export interface XPathExpression {
  toString(): string;
  hasAttr(attribute: string): XPathExpression;
  withAttr(attribute: string, value: string): XPathExpression;
  withAttrStartsWith(attribute: string, value: string): XPathExpression;
  withAttrContains(attribute: string, value: string): XPathExpression;
  withText(value: string): XPathExpression;
  withTextStartsWith(value: string): XPathExpression;
  withTextContains(value: string): XPathExpression;
  withID(id: string): XPathExpression;
  withClass(className: string): XPathExpression;
  not(predicate: XPathExpression): XPathExpression;
  or(...predicates: readonly XPathExpression[]): XPathExpression;
  and(...predicates: readonly XPathExpression[]): XPathExpression;
  child(tag?: string): XPathExpression;
  parent(): XPathExpression;
  descendant(tag?: string): XPathExpression;
  ancestor(tag?: string): XPathExpression;
  descendantNotSelf(tag?: string): XPathExpression;
  ancestorNotSelf(tag?: string): XPathExpression;
  followingSibling(tag?: string): XPathExpression;
  precedingSibling(tag?: string): XPathExpression;
  withChildMatching(childPath: XPathExpression): XPathExpression;
  first(): XPathExpression;
  nth(position: number): XPathExpression;
  last(): XPathExpression;
}

class XPathBuilder implements XPathExpression {
  constructor(private readonly expression: string) {}

  toString(): string { return this.expression; }
  hasAttr(attribute: string): XPathExpression { return this.append(`[@${attribute}]`); }
  withAttr(attribute: string, value: string): XPathExpression { return this.append(`[@${attribute}=${quoteXPath(value)}]`); }
  withAttrStartsWith(attribute: string, value: string): XPathExpression {
    return this.append(`[starts-with(@${attribute}, ${quoteXPath(value)})]`);
  }
  withAttrContains(attribute: string, value: string): XPathExpression {
    return this.append(`[contains(@${attribute}, ${quoteXPath(value)})]`);
  }
  withText(value: string): XPathExpression { return this.append(`[text()=${quoteXPath(value)}]`); }
  withTextStartsWith(value: string): XPathExpression {
    return this.append(`[starts-with(text(), ${quoteXPath(value)})]`);
  }
  withTextContains(value: string): XPathExpression {
    return this.append(`[contains(text(), ${quoteXPath(value)})]`);
  }
  withID(id: string): XPathExpression { return this.withAttr("id", id); }
  withClass(className: string): XPathExpression {
    return this.append(`[contains(concat(' ',normalize-space(@class),' '),${quoteXPath(` ${className} `)})]`);
  }
  not(predicate: XPathExpression): XPathExpression {
    return this.append(`[not(${predicateContent(predicate)})]`);
  }
  or(...predicates: readonly XPathExpression[]): XPathExpression {
    return this.booleanPredicate("or", predicates);
  }
  and(...predicates: readonly XPathExpression[]): XPathExpression {
    return this.booleanPredicate("and", predicates);
  }
  child(tag?: string): XPathExpression { return this.append(tag === undefined ? "/*" : `/${tag}`); }
  parent(): XPathExpression { return this.append("/.."); }
  descendant(tag?: string): XPathExpression { return this.append(tag === undefined ? "//*" : `//${tag}`); }
  ancestor(tag?: string): XPathExpression {
    return this.append(tag === undefined ? "/ancestor-or-self::*" : `/ancestor-or-self::${tag}`);
  }
  descendantNotSelf(tag?: string): XPathExpression {
    return this.append(tag === undefined ? "/descendant::*" : `/descendant::${tag}`);
  }
  ancestorNotSelf(tag?: string): XPathExpression {
    return this.append(tag === undefined ? "/ancestor::*" : `/ancestor::${tag}`);
  }
  followingSibling(tag?: string): XPathExpression {
    return this.append(tag === undefined ? "/following-sibling::*" : `/following-sibling::${tag}`);
  }
  precedingSibling(tag?: string): XPathExpression {
    return this.append(tag === undefined ? "/preceding-sibling::*" : `/preceding-sibling::${tag}`);
  }
  withChildMatching(childPath: XPathExpression): XPathExpression { return this.append(`[${childPath.toString()}]`); }
  first(): XPathExpression { return this.append("[1]"); }
  nth(position: number): XPathExpression {
    if (!Number.isInteger(position)) {
      throw new BilobaError({code: "INVALID_ARGUMENT", message: "XPath.nth requires an integer position"});
    }
    return this.append(`[${position}]`);
  }
  last(): XPathExpression { return this.append("[last()]"); }

  private append(suffix: string): XPathExpression { return new XPathBuilder(this.expression + suffix); }
  private booleanPredicate(operator: "and" | "or", predicates: readonly XPathExpression[]): XPathExpression {
    return this.append(`[${predicates.map((predicate) => `(${predicateContent(predicate)})`).join(` ${operator} `)}]`);
  }
}

function quoteXPath(value: string): string {
  if (!value.includes(`"`)) return `"${value}"`;
  return `concat(${value.split(`"`).map((component) => `"${component}"`).join(`,'"',`)})`;
}

function predicateContent(predicate: XPathExpression): string {
  const value = predicate.toString();
  return value.slice(1, -1);
}

function startsAsXPath(path: string): boolean {
  return path.startsWith("/") || path.startsWith("./") || path.startsWith("(") || path.startsWith("*");
}

export function xpath(path?: string): XPathExpression {
  if (path === undefined) return new XPathBuilder("//*");
  return new XPathBuilder(startsAsXPath(path) ? path : `//${path}`);
}

export function relativeXPath(path?: string): XPathExpression {
  if (path === undefined) return new XPathBuilder("./*");
  return new XPathBuilder(startsAsXPath(path) ? path : `./${path}`);
}

export function xPredicate(): XPathExpression {
  return new XPathBuilder("");
}

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

export interface CancellationOptions {
  signal?: AbortSignal | undefined;
}

export type Modifier = "Shift" | "Control" | "Alt" | "Meta";
export type TextMode = "innerText" | "textContent" | "normalizedText";
export type NameSpec = string | {readonly name: string; readonly allowMissing: true};
export type ElementState = "visible" | "enabled" | "clickable" | "checked" | "focused";
export type GeometryRelation = "above" | "below" | "leftOf" | "rightOf" | "encloses" | "overlaps";
export interface Point {readonly x: number; readonly y: number}
export interface PointerOptions extends WaitOptions {
  position?: Point | undefined;
  modifiers?: readonly Modifier[] | undefined;
}
export interface ClickOptions extends PointerOptions {
  button?: "left" | "right" | "middle" | undefined;
  clickCount?: 1 | 2 | undefined;
}
export interface KeyboardOptions extends WaitOptions {modifiers?: readonly Modifier[] | undefined}
export interface WindowKeyboardOptions extends CancellationOptions {modifiers?: readonly Modifier[] | undefined}
export interface ScrollIntoViewOptions extends WaitOptions {
  within?: Locator | string | undefined;
  topOffset?: number | undefined;
}
// Type aliases rather than interfaces: these are plain readonly records that come straight off the
// wire, and the natural thing to write with one is expectBoundingBox(equalTo(await box())).  An
// interface has no implicit index signature, so it does not satisfy SerializableValue and that line
// does not compile - which would leave the assertion reachable only through a cast.
export type Box = {
  readonly top: number; readonly left: number; readonly width: number; readonly height: number;
  readonly bottom: number; readonly right: number; readonly centerX: number; readonly centerY: number;
  readonly clientWidth: number; readonly clientHeight: number;
};
export type ScrollOffset = {readonly top: number; readonly left: number; readonly maxTop: number; readonly maxLeft: number};
export type Offset = {readonly top: number; readonly left: number};
export type BoxPair = {readonly subject: Box; readonly other: Box};
export type BoxDelta = {
  readonly top: number; readonly left: number; readonly width: number; readonly height: number;
  readonly bottom: number; readonly right: number; readonly centerX: number; readonly centerY: number;
};
export type DocumentOrder = "before" | "after" | "same" | "disconnected";
export interface ValueLabel {readonly __biloba_value_label: string}
export const optionLabel = (label: string): ValueLabel => ({__biloba_value_label: label});

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
  level(level: number): Locator;
  checked(): Locator;
  disabled(): Locator;
  expanded(): Locator;
  pressed(): Locator;
  selected(): Locator;
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
  click(options?: ClickOptions): Promise<void>;
  dblclick(options?: PointerOptions): Promise<void>;
  rightClick(options?: PointerOptions): Promise<void>;
  middleClick(options?: PointerOptions): Promise<void>;
  clickAll(options?: CancellationOptions): Promise<void>;
  tap(options?: PointerOptions): Promise<void>;
  focus(options?: WaitOptions): Promise<void>;
  blur(options?: WaitOptions): Promise<void>;
  hover(options?: WaitOptions): Promise<void>;
  setValue(value: SerializableValue, options?: WaitOptions): Promise<void>;
  selectOption(value: string | ValueLabel, options?: WaitOptions): Promise<void>;
  type(keys: string, options?: KeyboardOptions): Promise<void>;
  setUploadFiles(paths: readonly string[], options?: WaitOptions): Promise<void>;
  dragTo(target: Locator | string, options?: WaitOptions): Promise<void>;
  scrollIntoView(options?: ScrollIntoViewOptions): Promise<void>;
  scrollWheel(deltaX: number, deltaY: number, options?: WaitOptions): Promise<void>;
  selectText(options?: WaitOptions & {substring?: string | undefined; occurrence?: number | undefined}): Promise<void>;
  selectRange(start: number, end: number, options?: WaitOptions): Promise<void>;
  setProperty(name: string, value: SerializableValue, options?: WaitOptions): Promise<void>;
  setPropertyAll(name: string, value: SerializableValue, options?: CancellationOptions): Promise<void>;
  invokeMethod<T = unknown>(method: string, args?: readonly SerializableValue[], options?: WaitOptions): Promise<T>;
  invoke<T = unknown>(expression: string, args?: readonly SerializableValue[], options?: WaitOptions): Promise<T>;
  invokeMethodAll<T = unknown>(method: string, args?: readonly SerializableValue[], options?: CancellationOptions): Promise<readonly T[]>;
  invokeAll<T = unknown>(expression: string, args?: readonly SerializableValue[], options?: CancellationOptions): Promise<readonly T[]>;
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
  expectChecked(options?: WaitOptions): Promise<AssertionResult>;
  expectNotChecked(options?: WaitOptions): Promise<AssertionResult>;
  expectFocused(options?: WaitOptions): Promise<AssertionResult>;
  expectNotFocused(options?: WaitOptions): Promise<AssertionResult>;
  expectAllVisible(options?: WaitOptions): Promise<AssertionResult>;
  expectAllEnabled(options?: WaitOptions): Promise<AssertionResult>;
  expectClass(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectEachClass(name: string, options?: WaitOptions): Promise<AssertionResult>;
  expectInnerText(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectTextContent(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectNormalizedText(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectEachInnerText(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectEachTextContent(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectEachNormalizedText(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectAttributePresent(name: string, options?: WaitOptions): Promise<AssertionResult>;
  expectEachAttribute(name: string, expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectPropertyPresent(name: string, options?: WaitOptions): Promise<AssertionResult>;
  expectJSONAttribute(name: string, expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectEachProperty(name: string, expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectInnerHTML(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectDistinctAttributeCount(name: string, expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectInViewport(options?: WaitOptions & {fully?: boolean | undefined; negated?: boolean | undefined}): Promise<AssertionResult>;
  expectGeometry(relation: GeometryRelation, other: Locator | string, options?: WaitOptions & {negated?: boolean | undefined}): Promise<AssertionResult>;
  expectComputedStyle(name: string, expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectComputedStyleNumber(name: string, expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectComputedColor(name: string, expected: string, options?: WaitOptions): Promise<AssertionResult>;
  expectBoundingBox(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectScrollOffset(expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectOffsetWithin(container: Locator | string, expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectRelativeBoxes(other: Locator | string, expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectGapBetween(other: Locator | string, expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectDocumentOrder(other: Locator | string, expected: DocumentOrder, options?: WaitOptions): Promise<AssertionResult>;
  expectAbove(other: Locator | string, options?: WaitOptions): Promise<AssertionResult>;
  expectBelow(other: Locator | string, options?: WaitOptions): Promise<AssertionResult>;
  expectLeftOf(other: Locator | string, options?: WaitOptions): Promise<AssertionResult>;
  expectRightOf(other: Locator | string, options?: WaitOptions): Promise<AssertionResult>;
  expectEncloses(other: Locator | string, options?: WaitOptions): Promise<AssertionResult>;
  expectOverlaps(other: Locator | string, options?: WaitOptions): Promise<AssertionResult>;
  expectBefore(other: Locator | string, options?: WaitOptions): Promise<AssertionResult>;
  expectAfter(other: Locator | string, options?: WaitOptions): Promise<AssertionResult>;
  innerText(options?: WaitOptions): Promise<string>;
  textContent(options?: WaitOptions): Promise<string>;
  normalizedText(options?: WaitOptions): Promise<string>;
  innerHTML(options?: WaitOptions): Promise<string>;
  currentInnerTexts(): Promise<readonly string[]>;
  currentTextContents(): Promise<readonly string[]>;
  currentNormalizedTexts(): Promise<readonly string[]>;
  text(options?: WaitOptions): Promise<string>;
  count(): Promise<number>;
  classes(options?: WaitOptions): Promise<readonly string[]>;
  currentClasses(): Promise<readonly (readonly string[])[]>;
  attributes(names: readonly NameSpec[], options?: WaitOptions): Promise<Readonly<Record<string, unknown>>>;
  currentAttributes(names: readonly string[]): Promise<readonly Readonly<Record<string, string | null>>[]>;
  jsonAttribute<T = unknown>(name: string, options?: WaitOptions): Promise<T>;
  properties<T extends Readonly<Record<string, unknown>> = Readonly<Record<string, unknown>>>(names: readonly NameSpec[], options?: WaitOptions): Promise<T>;
  currentProperties(names: readonly string[]): Promise<readonly Readonly<Record<string, unknown>>[]>;
  currentProperty<T = unknown>(name: string): Promise<readonly T[]>;
  currentValues<T = unknown>(): Promise<readonly T[]>;
  getAttribute(name: string, options?: WaitOptions): Promise<string | null>;
  getProperty<T = unknown>(name: string, options?: WaitOptions): Promise<T>;
  value<T = unknown>(options?: WaitOptions): Promise<T>;
  exists(): Promise<boolean>;
  boundingBox(options?: WaitOptions): Promise<Box>;
  scrollOffset(options?: WaitOptions): Promise<ScrollOffset>;
  offsetWithin(container: Locator | string, options?: WaitOptions): Promise<Offset>;
  relativeBoxes(other: Locator | string, options?: WaitOptions): Promise<BoxPair>;
  gapBetween(other: Locator | string, options?: WaitOptions): Promise<BoxDelta>;
  documentOrder(other: Locator | string, options?: WaitOptions): Promise<DocumentOrder>;
  computedStyle(name: string, options?: WaitOptions): Promise<string>;
  computedStyleNumber(name: string, options?: WaitOptions): Promise<number>;
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
  sendKeys(keys: string, options?: WindowKeyboardOptions): Promise<void>;
  clearSelection(options?: CancellationOptions): Promise<void>;
  normalizeColor(color: string, options?: CancellationOptions): Promise<string>;
  close(): Promise<void>;
  locator(css: string): Locator;
  xpath(expression: string | XPathExpression): Locator;
  getByTestId(value: string, options?: {attribute?: string | undefined}): Locator;
  getByText(value: string, options?: {exact?: boolean | undefined}): Locator;
  getByRole(role: string, options?: {name?: string | undefined; exact?: boolean | undefined}): Locator;
  getByLabel(value: string, options?: {exact?: boolean | undefined}): Locator;
  getByPlaceholder(value: string, options?: {exact?: boolean | undefined}): Locator;
  getByAltText(value: string, options?: {exact?: boolean | undefined}): Locator;
  getByTitle(value: string, options?: {exact?: boolean | undefined}): Locator;
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

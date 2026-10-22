import {automationDetected, booleanEnvironment, connectWithTransport, resolveDiagnosticsPolicy, resolveVisualConnectOptions, stripStackHeader} from "./internal/client.js";
import {resolveDaemonExecutable} from "./internal/daemon-resolver.js";
import {
  startSharedBrowser as spawnSharedBrowser,
  type SharedBrowserProcess,
  type StartSharedBrowserOptions,
  type SharedBrowserConnection,
  type ResolvedLaunchMetadata,
} from "./internal/browser-manager.js";
import {
  startDaemon as spawnDaemon,
  type DaemonProcess,
  type StartDaemonOptions,
} from "./internal/daemon-manager.js";

export type {DaemonProcess, StartDaemonOptions};
export type {SharedBrowserProcess, StartSharedBrowserOptions, SharedBrowserConnection, ResolvedLaunchMetadata};

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
  /** Shorthand for mode: "immediate". Try exactly once and fail fast. */
  immediate?: boolean | undefined;
}
export type PollOptions = WaitOptions;

export interface CancellationOptions {
  signal?: AbortSignal | undefined;
}

export interface WaitingCommandOptions extends CancellationOptions {timeoutMs?: number | undefined}
export type CommandOptions = CancellationOptions;

export type ScreenshotColorScheme = "light" | "dark";
export type ScreenshotSubject = Locator | string;
export interface ScreenshotRenderingOptions {
  mask?: readonly ScreenshotSubject[] | undefined;
  /**
   * Capture the page the way expectScreenshot does: wait for web fonts, then turn off CSS animations
   * and transitions and hide the text caret for the duration of the capture.  A capture is not frozen
   * unless you ask.
   */
  frozen?: boolean | undefined;
  /** Hide the blinking text caret for the capture and leave everything else running. */
  hideCaret?: boolean | undefined;
  /**
   * Capture exactly what is on screen, never expanding the viewport.  On a locator, refuses an element
   * that is not entirely in view.
   */
  viewport?: boolean | undefined;
  colorScheme?: ScreenshotColorScheme | undefined;
  maxBytes?: number | undefined;
}
/** A region of the viewport, in CSS pixels from its top-left corner (the frame's viewport, on a frame handle). */
export interface ScreenshotClip {readonly x: number; readonly y: number; readonly width: number; readonly height: number}
export interface PageScreenshotOptions {
  /**
   * Capture one region of the viewport.  Like viewport it never resizes the viewport; a region that
   * reaches outside it is an INVALID_ARGUMENT error.
   */
  clip?: ScreenshotClip | undefined;
}
export interface ScreenshotBytesOptions extends ScreenshotRenderingOptions, WaitingCommandOptions {
  output?: "bytes" | undefined;
  name?: never;
}
export interface ScreenshotPathOptions extends ScreenshotRenderingOptions, WaitingCommandOptions {
  output: "path";
  name?: string | undefined;
}
export interface PageScreenshotBytesOptions extends ScreenshotBytesOptions, PageScreenshotOptions {}
export interface PageScreenshotPathOptions extends ScreenshotPathOptions, PageScreenshotOptions {}
export interface VisualScreenshotOptions extends PollOptions {
  mask?: readonly ScreenshotSubject[] | undefined;
  animated?: boolean | undefined;
  colorSchemes?: readonly ScreenshotColorScheme[] | undefined;
  pixelTolerance?: number | undefined;
  channelTolerance?: number | undefined;
  maxBytes?: number | undefined;
}
export interface ScreenshotPoint {readonly x: number; readonly y: number}
export interface ScreenshotRect {readonly min: ScreenshotPoint; readonly max: ScreenshotPoint}
export interface ScreenshotBounds {readonly width: number; readonly height: number}
export interface VisualRegion {readonly rect: ScreenshotRect; readonly differingPixels: number}
export interface VisualDiff {
  readonly match: boolean; readonly dimensionMismatch: boolean;
  readonly baseline: ScreenshotBounds; readonly actual: ScreenshotBounds;
  readonly totalPixels: number; readonly differingPixels: number; readonly fraction: number; readonly maxChannelDelta: number;
  readonly regions: readonly VisualRegion[]; readonly regionCount: number;
  readonly shifted: boolean; readonly shift?: ScreenshotPoint; readonly scattered: boolean; readonly rasterizationLikely: boolean; readonly unchanged?: string;
}
export type VisualSchemeStatus = "matched" | "missing" | "mismatched" | "created" | "updated" | "unchanged";
export interface VisualSchemeResult {
  readonly scheme?: ScreenshotColorScheme; readonly status: VisualSchemeStatus; readonly match: boolean;
  readonly baselinePath: string; readonly actualPath?: string; readonly diffPath?: string;
  readonly diff?: VisualDiff; readonly previousDiff?: VisualDiff; readonly diagnosis?: string;
  readonly warning?: string; readonly updateSummary?: string;
}
export interface VisualResult {
  readonly match: boolean; readonly updated: boolean; readonly warnings: readonly string[];
  readonly schemes: readonly VisualSchemeResult[]; readonly attemptCount: number; readonly elapsedMs: number;
}
export interface ScreenshotWarning {readonly sessionId: string; readonly operation: "captureScreenshot" | "expectScreenshot"; readonly message: string}

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
export type PointerType = "mouse" | "pen";
export interface PointerHandleOptions {
  /** "mouse" (the default) or "pen": every event the pointer dispatches reaches the page with this pointerType. */
  type?: PointerType | undefined;
}
export interface MoveToElementOptions extends WaitOptions {
  /** A point measured from the element's top-left corner, in CSS pixels.  The default is the element's center. */
  position?: Point | undefined;
}
/**
 * A real pointer you drive one event at a time, from session.pointer().  It remembers where it is
 * (starting at 0, 0) and whether its primary button is pressed; moves while it is pressed carry
 * buttons: 1, so the page sees a drag.  A pen reports a pressure of 0.5 while it touches and 0 while
 * it hovers.
 *
 * moveTo, down, and up dispatch real CDP input at once - one round trip each - with no scroll, no
 * stability wait, and no polling.  moveToElement polls until the element exists and is visible, then
 * dispatches; it does not scroll, so an element whose point is outside the viewport fails.  prepare()
 * releases a pointer left pressed and returns every pointer to (0, 0).
 */
export interface Pointer {
  readonly type: PointerType;
  /** Move to (x, y), in CSS pixels from the top-left of the viewport (the frame's viewport, on a frame handle). */
  moveTo(x: number, y: number, options?: CancellationOptions): Promise<void>;
  /** Move to the element's center, or to options.position from its top-left corner. */
  moveToElement(target: Locator | string, options?: MoveToElementOptions): Promise<void>;
  /** Press the primary button where the pointer is.  Pressing a pressed pointer is an INVALID_ARGUMENT error. */
  down(options?: CancellationOptions): Promise<void>;
  /** Release the primary button where the pointer is.  Releasing a released pointer is an INVALID_ARGUMENT error. */
  up(options?: CancellationOptions): Promise<void>;
}
/** A page-clock time: a Date, or milliseconds since the Unix epoch.  The clock has millisecond resolution. */
export type ClockTime = Date | number;
/**
 * A tab's page clock: fake timers for the page's own code, so a test can move time instead of waiting
 * for it.  session.installClock() installs it and returns one; session.clock() returns one for the
 * tab.  Every method fails with INVALID_ARGUMENT when the tab has no clock installed, or on a frame
 * handle.  Biloba itself keeps running on real time, so polling never advances a paused clock.
 */
export interface Clock {
  /**
   * Stop the page's time at `time`: no timer or animation frame runs until runFor, fastForward, or
   * resume.  A time ahead of the clock gets there the way fastForward does; a time behind it (time
   * flows after installClock, so pausing at the start time usually goes back a little) moves the clock
   * back without firing anything.
   */
  pauseAt(time: ClockTime, options?: CommandOptions): Promise<void>;
  /** Let the page's time flow again at the real rate, from wherever the clock is. */
  resume(options?: CommandOptions): Promise<void>;
  /**
   * Advance the clock by `ms`, running every timer and animation frame that falls due, in order, each
   * in its own task so promise callbacks run in between.  It gives up after `timeoutMs` of real time
   * (10 seconds by default) with a TIMEOUT that says how far it got.
   */
  runFor(ms: number, options?: WaitingCommandOptions): Promise<void>;
  /** Jump the clock ahead by `ms`, firing each timer that fell due along the way once, at the new time. */
  fastForward(ms: number, options?: CommandOptions): Promise<void>;
  /** Pin what the page's Date reports to `time`.  Timers and animation frames keep running on the clock's own time. */
  setFixedTime(time: ClockTime, options?: CommandOptions): Promise<void>;
  /** Move the clock to `time`, forwards or backwards, without firing any timer; unpins setFixedTime. */
  setSystemTime(time: ClockTime, options?: CommandOptions): Promise<void>;
}
export interface ScreencastOptions extends CancellationOptions {
  /** "jpeg" (the default) or "png". */
  format?: "jpeg" | "png" | undefined;
  /** JPEG quality, 0-100; 0 or omitted leaves it to Chrome.  PNG ignores it. */
  quality?: number | undefined;
  /** Shrink frames to fit, in pixels; Chrome keeps the aspect ratio and never enlarges a frame. */
  maxWidth?: number | undefined;
  maxHeight?: number | undefined;
  /** Keep one frame in every n. */
  everyNthFrame?: number | undefined;
}
/**
 * One screencast frame.  timestamp is when Chrome expects to display the frame, in milliseconds since
 * the Unix epoch (fractional), so on the machine running Chrome it compares directly with Date.now();
 * it is 0 if Chrome sent none.  From Chrome 156 that is about one frame after the page drew it, so the
 * last frame's timestamp can be a little later than stop() resolving; earlier versions stamp the frame
 * when they send it.  deviceWidth and deviceHeight are the viewport size the
 * image shows, in CSS pixels; scrollOffsetX and scrollOffsetY are the page's scroll position; offsetTop
 * is how far down the image the page starts.
 */
export interface ScreencastFrame {
  readonly data: Uint8Array; readonly timestamp: number;
  readonly deviceWidth: number; readonly deviceHeight: number; readonly pageScaleFactor: number;
  readonly scrollOffsetX: number; readonly scrollOffsetY: number; readonly offsetTop: number;
}
/** A screencast in progress, from session.startScreencast(). */
export interface ScreencastRecording {
  /**
   * Stop the screencast and return its frames in timestamp order.  It waits for Chrome to confirm, so
   * no frame arrives after it resolves; calling it again returns the same frames.
   */
  stop(options?: CancellationOptions): Promise<readonly ScreencastFrame[]>;
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
  readonly session?: boolean | undefined;
}

export interface CookieQuery {
  name?: ExpectedValue | undefined;
  value?: ExpectedValue | undefined;
  domain?: ExpectedValue | undefined;
  path?: ExpectedValue | undefined;
  sameSite?: ExpectedValue | undefined;
  secure?: boolean | undefined;
  httpOnly?: boolean | undefined;
}

export type StorageArea = "localStorage" | "sessionStorage";
export type StorageItem<T = unknown> = {readonly found: true; readonly value: T} | {readonly found: false};
export interface BrowserStorage {
  set(key: string, value: SerializableValue, options?: CommandOptions): Promise<void>;
  get<T = unknown>(key: string, options?: CommandOptions): Promise<StorageItem<T>>;
  getAll(options?: CommandOptions): Promise<Readonly<Record<string, unknown>>>;
  remove(key: string, options?: CommandOptions): Promise<void>;
  clear(options?: CommandOptions): Promise<void>;
  length(options?: CommandOptions): Promise<number>;
  expectItem<T = unknown>(key: string, expected?: ExpectedValue, options?: PollOptions): Promise<T>;
  expectLength(expected: number | Expectation, options?: PollOptions): Promise<AssertionResult>;
}

export interface TabQuery {
  title?: ExpectedValue | undefined;
  url?: ExpectedValue | undefined;
  has?: Locator | string | undefined;
}
export interface WindowSize {readonly width: number; readonly height: number}
export type ChromeMode = "headless-shell" | "headless" | "headful";
export interface ChromeLaunchOptions {mode?: ChromeMode | undefined; chromePath?: string | undefined; autoInstall?: boolean | undefined; chromeArgs?: readonly string[] | undefined; windowSize?: WindowSize | undefined; chromeSandbox?: boolean | undefined}
export type LaunchMetadata = (ResolvedLaunchMetadata & {readonly attached: false}) | (ResolvedLaunchMetadata & {readonly attached: true; readonly source: "shared-host"}) | {readonly attached: true; readonly source: "external"; readonly chromeArgs: readonly []; readonly windowSize: WindowSize; readonly autoInstalled: false};
export type DiagnosticsPurpose = "failure" | "progress" | "on-demand";
export interface DriverDebugEntry {readonly timestamp: string; readonly direction: "send" | "receive" | "internal"; readonly message: string; readonly truncated?: boolean}
export interface DriverDebugEventsDropped {readonly code: "EVENTS_DROPPED"; readonly message: string; readonly details: {readonly count: number}}
export type DriverDebugEvent = DriverDebugEntry | DriverDebugEventsDropped;
export interface CaptureDiagnosticsOptions extends CommandOptions {purpose?: DiagnosticsPurpose; name?: string; screenshots?: boolean; outlines?: boolean; viewport?: WindowSize; maxScreenshotBytes?: number; includeScreenshotBytes?: boolean}
export type DiagnosticsArtifact = "tab" | "title" | "outline" | "outline-write" | "screenshot" | "screenshot-write" | "viewport-restore";
export type DiagnosticsArtifactErrorCode = BilobaErrorCode | "IO_ERROR" | "ACTION_FAILED" | "DEADLINE_EXCEEDED";
export interface DiagnosticsArtifactError {readonly artifact: DiagnosticsArtifact; readonly code: DiagnosticsArtifactErrorCode; readonly message: string}
export interface TabDiagnostics {readonly sessionId?: string; readonly targetId: string; readonly title: string; readonly screenshotPath?: string; readonly screenshot?: Uint8Array; readonly outlinePath?: string; readonly domOutline?: string; readonly errors: readonly DiagnosticsArtifactError[]}
export interface ContextDiagnostics {readonly purpose: DiagnosticsPurpose; readonly artifactDir?: string; readonly tabs: readonly TabDiagnostics[]}
/** One frame of a page stack trace.  line and column are zero-based, as Chrome reports them. */
export interface ConsoleStackFrame {readonly url: string; readonly functionName: string; readonly line: number; readonly column: number}
/**
 * A console message.  source is "" for the page's own console calls; for a message Chrome logged
 * itself (an intervention, a violation, a failed resource load, a deprecation) it names the origin
 * ("network", "intervention", ...) and type is the level ("debug", "info", "warning", "error").
 */
export interface ConsoleMessage {readonly type: string; readonly source: string; readonly text: string; readonly args: readonly unknown[]; readonly timestamp: string; readonly stack: readonly ConsoleStackFrame[]}
/**
 * An uncaught exception or unhandled promise rejection.  message reads the way DevTools prints it
 * ("Uncaught TypeError: x is not a function"); url, line, and column locate where it was thrown, and
 * line and column are zero-based, as Chrome reports them.
 */
export interface PageError {readonly message: string; readonly url: string; readonly line: number; readonly column: number; readonly stack: readonly ConsoleStackFrame[]; readonly timestamp: string}
export type DialogType = "alert" | "beforeunload" | "confirm" | "prompt";
export interface Dialog {readonly type: DialogType; readonly message: string; readonly defaultPrompt: string; readonly accepted: boolean; readonly promptText: string; readonly autoHandled: boolean}
export interface DialogQuery {readonly type?: DialogType | undefined; readonly message?: ExpectedValue | undefined}
export interface DialogHandlerOptions {readonly message?: ExpectedValue | undefined; readonly accept?: boolean | undefined; readonly promptText?: string | undefined}
export interface DialogHandler {readonly id: string; remove(options?: CommandOptions): Promise<void>}
export interface Warning {readonly code: "dialog_auto_handled"; readonly message: string; readonly dialog: Dialog}
export interface BilobaWarning {readonly code: string; readonly message: string; readonly details?: Readonly<Record<string, unknown>>; readonly dialog?: Dialog}
export type WarningSink = (warning: Warning) => void;
export type DownloadState = "active" | "complete" | "cancelled";
export interface Download {readonly id: string; readonly url: string; readonly filename: string; readonly state: DownloadState; readonly receivedBytes: number; readonly totalBytes: number; readonly startedAt: number; readonly completedAt?: number; content(options?: CommandOptions & {maxBytes?: number | undefined}): Promise<Uint8Array>; cancel(options?: CommandOptions): Promise<void>}
export interface DownloadQuery {readonly state?: DownloadState | undefined; readonly filename?: ExpectedValue | undefined; readonly url?: ExpectedValue | undefined; readonly content?: Uint8Array | undefined; readonly contentText?: ExpectedValue | undefined}
/** status matches the response status (0 while there is none); failed keeps only requests that failed. */
export interface RequestQuery {readonly url?: ExpectedValue | undefined; readonly method?: ExpectedValue | undefined; readonly resourceType?: ExpectedValue | undefined; readonly status?: ExpectedValue | undefined; readonly failed?: boolean | undefined}
export interface HeaderEntry {readonly name: string; readonly value: string}
/**
 * A request the session observed.  status is the response's HTTP status: 0 until the headers arrive,
 * and 0 for a request that failed without one.  A redirect is one request per hop, and a hop that was
 * redirected has the redirect's status.  failure is "" unless the request failed, when it is Chrome's
 * error text (net::ERR_CONNECTION_REFUSED, ...).  An HTTP error status is not a failure.
 */
export interface NetworkRequest {readonly url: string; readonly method: string; readonly headers: readonly HeaderEntry[]; readonly headerMap: Readonly<Record<string, string>>; readonly resourceType: string; readonly status: number; readonly failure: string}
export interface ResponseQuery {readonly url?: ExpectedValue | undefined; readonly status?: ExpectedValue | undefined}
export interface NetworkResponse {readonly url: string; readonly status: number; readonly headers: readonly HeaderEntry[]; readonly headerMap: Readonly<Record<string, string>>; readonly resourceType: string}
export interface RequestOverride {readonly url?: string | undefined; readonly method?: string | undefined; readonly headers?: readonly HeaderEntry[] | undefined; readonly body?: Uint8Array | undefined}
export interface ResponseOverride {readonly status?: number | undefined; readonly headers?: readonly HeaderEntry[] | undefined; readonly body?: Uint8Array | undefined}
export type FulfillResponse = ResponseOverride;
export interface InterceptedResponse {readonly url: string; readonly status: number; readonly headers: readonly HeaderEntry[]; readonly headerMap: Readonly<Record<string, string>>; readonly body: Uint8Array}
export interface NetworkHandlerOptions {readonly maxBodyBytes?: number | undefined; readonly timeoutMs?: number | undefined}
export interface NetworkHandlerStats {readonly id: string; readonly callsite: string; readonly count: number; readonly shadowed: number; readonly lastError: string}
export interface NetworkOwnerProvenance {readonly kind: "handler" | "hold"; readonly id: string; readonly callsite: string; readonly count: number; readonly shadowed: number}
export interface NetworkShadowDiagnostic {readonly url: string; readonly stage: "request" | "response"; readonly winner: NetworkOwnerProvenance; readonly shadowed: readonly NetworkOwnerProvenance[]}
export interface NetworkHandler {readonly id: string; count(options?: CommandOptions): Promise<number>; stats(options?: CommandOptions): Promise<NetworkHandlerStats>; remove(options?: CommandOptions): Promise<void>}
export type ConnectionType = "none" | "cellular2g" | "cellular3g" | "cellular4g" | "bluetooth" | "ethernet" | "wifi" | "wimax" | "other";
export interface NetworkState {readonly offline?: boolean | undefined; readonly latencyMs?: number | undefined; readonly downloadThroughput?: number | undefined; readonly uploadThroughput?: number | undefined; readonly connectionType?: ConnectionType | undefined}
export interface DeviceMetrics {readonly width: number; readonly height: number; readonly deviceScaleFactor?: number | undefined; readonly mobile?: boolean | undefined; readonly touch?: boolean | undefined; readonly landscape?: boolean | undefined}
export interface Geolocation {readonly latitude: number; readonly longitude: number; readonly accuracy?: number | undefined}
/** A permission name, as navigator.permissions.query takes it ("camera", "clipboard-read", ...). */
export type Permission = import("./generated/protocol.js").PermissionName;
export type PermissionState = "granted" | "denied" | "prompt";
export interface MediaEmulation {readonly type?: "screen" | "print" | "" | undefined; readonly colorScheme?: "light" | "dark" | "no-preference" | undefined; readonly reducedMotion?: "reduce" | "no-preference" | undefined}

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
  | "PAGE_CRASHED"
  | "VISUAL_BASELINE";


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
  visual?: VisualResult;
  artifactPaths?: readonly string[];
  diagnostics?: ContextDiagnostics;
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
  readonly visual?: VisualResult;
  readonly artifactPaths: readonly string[];
  readonly diagnostics?: ContextDiagnostics;

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
    if (options.visual !== undefined) this.visual = options.visual;
    this.artifactPaths = options.artifactPaths ?? [];
    if (options.diagnostics !== undefined) this.diagnostics = options.diagnostics;
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
  captureScreenshot(options?: ScreenshotBytesOptions): Promise<Uint8Array>;
  captureScreenshot(options: ScreenshotPathOptions): Promise<string>;
  expectScreenshot(name: string, options?: VisualScreenshotOptions): Promise<VisualResult>;
}

export interface Session {
  readonly id: string;
  readonly contextId: string;
  readonly targetId: string;
  readonly frameId?: string;
  readonly openerId?: string;
  readonly ownsContext: boolean;
  readonly isFrame: boolean;
  readonly frameUrl?: string;
  newTab(options?: WaitingCommandOptions): Promise<Session>;
  tabs(options?: CommandOptions): Promise<readonly Session[]>;
  spawnedTabs(options?: CommandOptions): Promise<readonly Session[]>;
  findTab(query: TabQuery, options?: CommandOptions): Promise<Session | undefined>;
  waitForTab(query: TabQuery, options?: PollOptions): Promise<Session>;
  frames(options?: CommandOptions): Promise<readonly Session[]>;
  waitForFrame(query: TabQuery, options?: PollOptions): Promise<Session>;
  addInitScript(script: string, options?: CommandOptions): Promise<void>;
  activate(options?: CommandOptions): Promise<void>;
  prepare(options?: WaitingCommandOptions): Promise<{readonly invalidatedSessionIds: readonly string[]}>;
  navigate(url: string, options?: WaitingCommandOptions): Promise<void>;
  navigateWithStatus(url: string, expectedStatus: number, options?: WaitingCommandOptions): Promise<void>;
  captureScreenshot(options?: PageScreenshotBytesOptions): Promise<Uint8Array>;
  captureScreenshot(options: PageScreenshotPathOptions): Promise<string>;
  expectScreenshot(name: string, options?: VisualScreenshotOptions): Promise<VisualResult>;
  captureDiagnostics(options?: CaptureDiagnosticsOptions): Promise<ContextDiagnostics>;
  setCookies(cookies: readonly Cookie[], options?: CommandOptions): Promise<void>;
  getCookies(options?: CommandOptions): Promise<readonly Cookie[]>;
  clearCookies(options?: CommandOptions): Promise<void>;
  findCookie(query: CookieQuery, options?: CommandOptions): Promise<Cookie | undefined>;
  expectCookie(query: CookieQuery, options?: PollOptions): Promise<Cookie>;
  expectCookieCount(expected: number | Expectation, query?: CookieQuery, options?: PollOptions): Promise<AssertionResult>;
  localStorage(): BrowserStorage;
  sessionStorage(): BrowserStorage;
  evaluate<T = unknown>(expression: string, args?: readonly SerializableValue[], options?: CommandOptions): Promise<T>;
  evaluateAsync<T = unknown>(expression: string, args?: readonly SerializableValue[], options?: WaitingCommandOptions): Promise<T>;
  waitForDefined<T = unknown>(expression: string, options?: PollOptions): Promise<T>;
  setWindowSize(width: number, height: number, options?: CommandOptions): Promise<void>;
  windowSize(options?: CommandOptions): Promise<WindowSize>;
  title(options?: CommandOptions): Promise<string>;
  expectTitle(expected: ExpectedValue, options?: PollOptions): Promise<AssertionResult>;
  outline(options?: CommandOptions): Promise<string>;
  accessibilityOutline(options?: CommandOptions): Promise<string>;
  consoleMessages(options?: CommandOptions): Promise<readonly ConsoleMessage[]>;
  onConsoleMessage(listener: (message: ConsoleMessage) => void): () => void;
  warnings(options?: CommandOptions): Promise<readonly BilobaWarning[]>;
  onWarning(listener: (warning: BilobaWarning) => void): () => void;
  expectConsoleMessage(expected: ExpectedValue, options?: PollOptions & {type?: string | undefined}): Promise<ConsoleMessage>;
  pageErrors(options?: CommandOptions): Promise<readonly PageError[]>;
  onPageError(listener: (pageError: PageError) => void): () => void;
  expectPageError(expected: ExpectedValue, options?: PollOptions): Promise<PageError>;
  setDeviceMetrics(metrics: DeviceMetrics, options?: CommandOptions): Promise<void>;
  clearDeviceMetrics(options?: CommandOptions): Promise<void>;
  setGeolocation(location: Geolocation, options?: CommandOptions): Promise<void>;
  clearGeolocation(options?: CommandOptions): Promise<void>;
  setPermissions(origin: string, permissions: Readonly<Partial<Record<Permission, PermissionState>>>, options?: CommandOptions): Promise<void>;
  resetPermissions(options?: CommandOptions): Promise<void>;
  setLocale(locale: string, options?: CommandOptions): Promise<void>;
  clearLocale(options?: CommandOptions): Promise<void>;
  setTimezone(timezone: string, options?: CommandOptions): Promise<void>;
  clearTimezone(options?: CommandOptions): Promise<void>;
  setMedia(media: MediaEmulation, options?: CommandOptions): Promise<void>;
  clearMedia(options?: CommandOptions): Promise<void>;
  setUserAgent(userAgent: string, options?: CommandOptions): Promise<void>;
  clearUserAgent(options?: CommandOptions): Promise<void>;
  sendKeys(keys: string, options?: WindowKeyboardOptions): Promise<void>;
  /** A real mouse or pen to drive one event at a time; see Pointer. */
  pointer(options?: PointerHandleOptions): Pointer;
  /**
   * Record the frames Chrome draws for this tab until you stop the returned recording.  A tab records
   * one screencast at a time, and a frame handle refuses with INVALID_ARGUMENT.  prepare() and close()
   * stop a recording that is still running and discard it: stop it first to keep its frames.
   */
  startScreencast(options?: ScreencastOptions): Promise<ScreencastRecording>;
  /**
   * Install the page clock on this tab and its same-process frames, starting at `start` with time
   * flowing at the real rate, and return the Clock that controls it.  Install before navigate() to
   * cover every timer the page creates.  A second installClock() before prepare() fails with
   * INVALID_ARGUMENT, as does installClock() on a frame handle.  prepare() and close() remove it.
   */
  installClock(start: ClockTime, options?: CommandOptions): Promise<Clock>;
  /** This tab's page clock, installed earlier with installClock(), for code that wasn't handed it. */
  clock(): Clock;
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
  // One URL assertion, as Go has one HaveURL.  expectUrl is the superset: exact tightens the match,
  // pathname compares window.location.pathname instead of the whole URL.
  expectUrl(expected: ExpectedValue, options?: WaitOptions & {exact?: boolean | undefined; pathname?: boolean | undefined}): Promise<AssertionResult>;
  url(options?: CommandOptions): Promise<string>;
  expectEvaluation(expression: string, expected: ExpectedValue, options?: WaitOptions): Promise<AssertionResult>;
  expectRequest(expectedUrl: ExpectedValue, options?: WaitOptions & {method?: string | undefined}): Promise<AssertionResult>;
  handleDialogs(type: DialogType, options?: DialogHandlerOptions): Promise<DialogHandler>;
  dialogs(query?: DialogQuery, options?: CommandOptions): Promise<readonly Dialog[]>;
  downloads(query?: DownloadQuery, options?: CommandOptions): Promise<readonly Download[]>;
  expectDownload(query?: DownloadQuery, options?: PollOptions): Promise<Download>;
  requests(query?: RequestQuery, options?: CommandOptions): Promise<readonly NetworkRequest[]>;
  waitForRequest(query: RequestQuery, options?: PollOptions): Promise<NetworkRequest>;
  responses(query?: ResponseQuery, options?: CommandOptions): Promise<readonly NetworkResponse[]>;
  expectNetworkIdle(options?: PollOptions): Promise<AssertionResult>;
  stubRequest(query: ExpectedValue, response: FulfillResponse, options?: NetworkHandlerOptions): Promise<NetworkHandler>;
  abortRequest(query: ExpectedValue, options?: NetworkHandlerOptions): Promise<NetworkHandler>;
  modifyRequest(query: ExpectedValue, override: RequestOverride, options?: NetworkHandlerOptions): Promise<NetworkHandler>;
  modifyResponse(query: ExpectedValue, override: ResponseOverride, options?: NetworkHandlerOptions): Promise<NetworkHandler>;
  routeResponse(query: ExpectedValue, callback: (response: InterceptedResponse) => ResponseOverride | Promise<ResponseOverride>, options?: NetworkHandlerOptions): Promise<NetworkHandler>;
  networkShadowDiagnostics(options?: CommandOptions): Promise<readonly NetworkShadowDiagnostic[]>;
  holdResponse(expectedUrl: ExpectedValue, options?: CommandOptions & {limit?: number | undefined; maxBodyBytes?: number | undefined}): Promise<ResponseHold>;
  setNetworkState(state: NetworkState, options?: CommandOptions): Promise<void>;
  setOffline(offline?: boolean, options?: CommandOptions): Promise<void>;
  resetNetworkState(options?: CommandOptions): Promise<void>;
  networkState(options?: CommandOptions): Promise<NetworkState>;
  setCacheEnabled(enabled: boolean, options?: CommandOptions): Promise<void>;
}

export interface HeldResponse {
  readonly id: string;
  readonly url: string;
  readonly status: number;
  readonly headers: readonly HeaderEntry[];
  readonly headerMap: Readonly<Record<string, string>>;
  readonly body: Uint8Array;
}

export interface ResponseHoldStats {readonly count: number; readonly held: number; readonly passedThrough: number; readonly holding: number; readonly lastError: string}

export interface ResponseHold {
  await(options?: WaitingCommandOptions): Promise<HeldResponse>;
  release(options?: CommandOptions): Promise<void>;
  release(response: HeldResponse | string, options?: CommandOptions): Promise<void>;
  releaseResponse(responseId: string, options?: CommandOptions): Promise<void>;
  releaseNext(options?: CommandOptions): Promise<void>;
  stats(options?: CommandOptions): Promise<ResponseHoldStats>;
  releaseAll(options?: CommandOptions): Promise<void>;
}

export interface Browser {
  readonly protocolVersion: string;
  readonly capabilities: ReadonlySet<string>;
  readonly launch: LaunchMetadata;
  openSession(): Promise<Session>;
  close(): Promise<void>;
}

export interface DiagnosticsPolicyOptions {artifactDir?: string; failureScreenshots?: boolean; failureOutlines?: boolean; failureViewport?: WindowSize; progressScreenshots?: boolean; progressOutlines?: boolean; progressViewport?: WindowSize; pollTrajectory?: boolean; inlineScreenshots?: boolean | "auto"; maxScreenshotBytes?: number}
export interface ConnectOptions extends ChromeLaunchOptions {
  daemonExecutable?: string | undefined;
  chromePath?: string | undefined;
  chromeWsUrl?: string | undefined;
  chromeConnection?: SharedBrowserConnection | undefined;
  diagnostics?: DiagnosticsPolicyOptions | undefined;
  debugLog?: ((entry: DriverDebugEvent) => void) | undefined;
  artifactDir?: string | undefined;
  signal?: AbortSignal | undefined;
  warningSink?: WarningSink | undefined;
  screenshotBaselinesDir?: string | undefined;
  updateScreenshots?: boolean | undefined;
  screenshotPixelTolerance?: number | undefined;
  screenshotChannelTolerance?: number | undefined;
  maxScreenshotBytes?: number | undefined;
  onScreenshotWarning?: ((warning: ScreenshotWarning) => void) | undefined;
}

export async function connect(options: ConnectOptions = {}): Promise<Browser> {
  const executable = await resolveDaemonExecutable({explicit: options.daemonExecutable});
  validateLaunchOptions(options);
  if (options.artifactDir !== undefined && options.diagnostics?.artifactDir !== undefined && options.artifactDir !== options.diagnostics.artifactDir) throw new BilobaError({code: "INVALID_ARGUMENT", message: "artifactDir conflicts with diagnostics.artifactDir"});
  const diagnostics = resolveDiagnosticsPolicy({...options.diagnostics, ...(options.artifactDir !== undefined && {artifactDir: options.artifactDir})}, process.env, automationDetected(process.env));
  const launchMode = options.mode ?? (booleanEnvironment("BILOBA_INTERACTIVE", process.env) === true ? "headful" : undefined);
  const screenshotWarning = options.onScreenshotWarning;
  const visual = resolveVisualConnectOptions(options, process.env, (message) => {
    if (screenshotWarning) screenshotWarning({sessionId: "", operation: "expectScreenshot", message});
    else process.emitWarning(message, {code: "BILOBA_UPDATE_SCREENSHOTS"});
  });
  const daemon = await spawnDaemon({
    executable,
    ...(options.chromeConnection ? {chromeWsUrl: options.chromeConnection.wsURL, attachedLaunchMetadata: Buffer.from(JSON.stringify({mode: options.chromeConnection.launch.mode, executablePath: options.chromeConnection.launch.executablePath, chromeArgs: options.chromeConnection.launch.chromeArgs, width: options.chromeConnection.launch.windowSize.width, height: options.chromeConnection.launch.windowSize.height, attached: true, autoInstalled: options.chromeConnection.launch.autoInstalled})).toString("base64")} : options.chromeWsUrl ? {chromeWsUrl: options.chromeWsUrl} : {}),
    ...(options.chromePath && {chromePath: options.chromePath}),
    ...(launchMode && {mode: launchMode}),
    ...(options.chromeArgs && {chromeArgs: options.chromeArgs}),
    ...(options.autoInstall !== undefined && {autoInstall: options.autoInstall}),
    ...(options.chromeSandbox !== undefined && {chromeSandbox: options.chromeSandbox}),
    ...(options.windowSize && {windowSize: options.windowSize}),
    ...(options.debugLog && {debugLog: true}),
    ...(diagnostics.artifactDir && {artifactDir: diagnostics.artifactDir}),
    ...visual,
    ...(visual.artifactDir && {visualArtifactDir: visual.artifactDir}),
    artifactDir: diagnostics.artifactDir,
  });
  return await connectWithTransport(daemon.transport, {...options, diagnostics, maxScreenshotBytes: visual.maxScreenshotBytes, onScreenshotWarning: screenshotWarning}, () => daemon.stop());
}


function validateLaunchOptions(options: ConnectOptions): void {
  if (options.mode !== undefined && !["headless-shell", "headless", "headful"].includes(options.mode)) throw new BilobaError({code: "INVALID_ARGUMENT", message: "mode must be headless-shell, headless, or headful"});
  if (options.windowSize && (!Number.isInteger(options.windowSize.width) || !Number.isInteger(options.windowSize.height) || options.windowSize.width <= 0 || options.windowSize.height <= 0)) throw new BilobaError({code: "INVALID_ARGUMENT", message: "windowSize dimensions must be positive integers"});
  if (options.autoInstall && options.mode !== undefined && options.mode !== "headless-shell") throw new BilobaError({code: "INVALID_ARGUMENT", message: "autoInstall is only valid in headless-shell mode"});
  for (const argument of options.chromeArgs ?? []) if (!/^--[A-Za-z0-9][A-Za-z0-9-]*(=.*)?$/.test(argument)) throw new BilobaError({code: "INVALID_ARGUMENT", message: `invalid Chrome argument ${JSON.stringify(argument)}`});
  if (options.chromeConnection && (options.chromeWsUrl !== undefined || options.mode !== undefined || options.chromePath !== undefined || options.autoInstall !== undefined || options.chromeArgs !== undefined || options.windowSize !== undefined || options.chromeSandbox !== undefined)) throw new BilobaError({code: "INVALID_ARGUMENT", message: "chromeConnection conflicts with process launch options"});
}

export async function startDaemon(options: StartDaemonOptions): Promise<DaemonProcess> {
  return await spawnDaemon(options);
}

export async function startSharedBrowser(options: StartSharedBrowserOptions): Promise<SharedBrowserProcess> {
  return await spawnSharedBrowser(options);
}

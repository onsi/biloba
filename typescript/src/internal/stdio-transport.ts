import type {Readable, Writable} from "node:stream";

import {BilobaError, type WaitOptions} from "../index.js";
import type {
  AssertRequest,
  AddInitScriptRequest,
  DragToRequest,
  DOMRequest,
  EvaluateRequest,
  EventfulRequest,
  EventFrame,
  NetworkOverride as WireNetworkOverride,
  HandshakeRequest,
  HandshakeResponse,
  HandleListResponse,
  InvalidationResponse,
  LifecycleRequest,
  CookieListResponse,
  ListHandlesRequest,
  WaitForTabRequest,
  HoldResponseRequest,
  LocatorRequest,
  NavigateRequest,
  OpenSessionRequest,
  OpenSessionResponse,
  OperationResult,
  ProtocolError,
  Response,
  ResponseHoldRequest,
  SessionRequest,
  SetCookiesRequest,
  SetValueRequest,
  SetUploadRequest,
  SetWindowSizeRequest,
  TypeRequest,
  SendKeysRequest,
  ScreenshotRequest,
  CaptureDiagnosticsRequest,
  ContextDiagnosticsResponse,
  SubscribeEventsRequest,
  SubscribeEventsResponse,
  UnsubscribeEventsRequest,
  EventEnvelope,
} from "../generated/protocol.js";
import {encodeFrame, FrameDecoder} from "./framing.js";

interface TransportOptions extends WaitOptions { deadlineMs?: number }

// The non-polling operations carry no poll budget for the transport to inherit, so without a
// deadline of their own a wedged Chrome - or a daemon stuck mid-request - hangs the worker until
// the test runner's own timeout fires, with none of Biloba's diagnostics attached.  30s mirrors
// Go's navigationTimeout (navigation.go): generous enough for a real page load, a fresh browser
// context, or a slow evaluate, short enough to surface as a Biloba TIMEOUT.  A caller that knows
// better passes timeoutMs.  The polling operations (click/setValue/assert) deliberately get no
// default: their deadline is derived from the poll timeout so the client never gives up before the
// daemon's own poll does.
const defaultDeadlineMs = 30_000;

function withDefaultDeadline(options: TransportOptions): TransportOptions {
  return {...options, deadlineMs: options.deadlineMs ?? options.timeoutMs ?? defaultDeadlineMs};
}

interface PendingRequest {
  resolve(value: unknown): void;
  reject(reason: unknown): void;
  cleanup(): void;
}
type QueuedEvent = {event: string; envelope: EventEnvelope};
type EventListenerState = {listener?: (event: string, envelope: EventEnvelope) => void; queue: QueuedEvent[]; scheduled: boolean; dropped: number};
const eventQueueLimit = 256;

type ResponseCallback = (payload: unknown) => WireNetworkOverride | Promise<WireNetworkOverride>;

export class StdioTransport {
  readonly #input: Readable;
  readonly #output: Writable;
  readonly #decoder = new FrameDecoder();
  readonly #pending = new Map<number, PendingRequest>();
  readonly #callbacks = new Map<string, ResponseCallback>();
  readonly #eventListeners = new Map<string, EventListenerState>();
  #nextID = 1;
  #closed = false;
  #writeChain = Promise.resolve();

  constructor(input: Readable, output: Writable, options: {failOnEnd?: boolean} = {}) {
    this.#input = input;
    this.#output = output;
    input.pipe(this.#decoder);
    this.#decoder.on("data", (frame: Response | EventFrame) => {
      if ("event" in frame) void this.#receiveEvent(frame);
      else this.#receive(frame);
    });
    this.#decoder.on("error", (error: Error) => this.fail(error));
    input.on("error", (error: Error) => this.fail(error));
    output.on("error", (error: Error) => this.fail(error));
    if (options.failOnEnd !== false) {
      input.on("end", () => this.fail(new Error("bilobad closed stdout")));
    }
  }

  close(): void {
    this.fail(new Error("Biloba transport closed"));
  }

  fail(error: Error, daemonDetail?: string): void {
    if (this.#closed) return;
    this.#closed = true;
    this.#input.unpipe(this.#decoder);
    const failure = new BilobaError({
      code: "DRIVER_CLOSED",
      message: error.message,
      ...(daemonDetail && {daemonDetail}),
    });
    for (const request of this.#pending.values()) {
      request.cleanup();
      request.reject(failure);
    }
    this.#pending.clear();
    this.#callbacks.clear();
    this.#eventListeners.clear();
  }

  handshake(request: HandshakeRequest, options: TransportOptions = {}): Promise<HandshakeResponse> {
    return this.#request("handshake", request, withDefaultDeadline(options));
  }
  openSession(request: OpenSessionRequest, options: TransportOptions = {}): Promise<OpenSessionResponse> {
    return this.#request("openSession", request, withDefaultDeadline(options));
  }
  newTab(request: SessionRequest, options: TransportOptions = {}): Promise<OpenSessionResponse> {
    return this.#request("newTab", request, withDefaultDeadline(options));
  }
  addInitScript(request: AddInitScriptRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("addInitScript", request, withDefaultDeadline(options));
  }
  activate(request: SessionRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("activate", request, withDefaultDeadline(options));
  }
  prepareSession(request: SessionRequest, options: TransportOptions = {}): Promise<InvalidationResponse> {
    return this.#request("prepareSession", request, withDefaultDeadline(options));
  }
  closeSession(request: SessionRequest, options: TransportOptions = {}): Promise<InvalidationResponse> {
    return this.#request("closeSession", request, withDefaultDeadline(options));
  }
  navigate(request: NavigateRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("navigate", request, withDefaultDeadline(options));
  }
  setCookies(request: SetCookiesRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("setCookies", request, withDefaultDeadline(options));
  }
  getCookies(request: SessionRequest, options: TransportOptions = {}): Promise<CookieListResponse> {
    return this.#request("getCookies", request, withDefaultDeadline(options));
  }
  clearCookies(request: SessionRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("clearCookies", request, withDefaultDeadline(options));
  }
  listTabs(request: ListHandlesRequest, options: TransportOptions = {}): Promise<HandleListResponse> {
    return this.#request("listTabs", request, withDefaultDeadline(options));
  }
  listFrames(request: ListHandlesRequest, options: TransportOptions = {}): Promise<HandleListResponse> {
    return this.#request("listFrames", request, withDefaultDeadline(options));
  }
  waitForTab(request: WaitForTabRequest, options: TransportOptions = {}): Promise<OpenSessionResponse> {
    return this.#request("waitForTab", request, options);
  }
  waitForFrame(request: WaitForTabRequest, options: TransportOptions = {}): Promise<OpenSessionResponse> {
    return this.#request("waitForFrame", request, options);
  }
  lifecycle(request: LifecycleRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("lifecycle", request, options);
  }
  click(request: LocatorRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("click", request, options);
  }
  setValue(request: SetValueRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("setValue", request, options);
  }
  type(request: TypeRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("type", request, options);
  }
  sendKeys(request: SendKeysRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("sendKeys", request, withDefaultDeadline(options));
  }
  setWindowSize(request: SetWindowSizeRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("setWindowSize", request, withDefaultDeadline(options));
  }
  setUpload(request: SetUploadRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("setUpload", request, options);
  }
  dragTo(request: DragToRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("dragTo", request, options);
  }
  holdResponse(request: HoldResponseRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("holdResponse", request, withDefaultDeadline(options));
  }
  awaitResponseHold(request: ResponseHoldRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("awaitResponseHold", request, withDefaultDeadline(options));
  }
  releaseResponseHold(request: ResponseHoldRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("releaseResponseHold", request, withDefaultDeadline(options));
  }
  evaluate(request: EvaluateRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("evaluate", request, withDefaultDeadline(options));
  }
  assert(request: AssertRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("assert", request, options);
  }
  dom(request: DOMRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("dom", request, options);
  }
  screenshot(request: ScreenshotRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("screenshot", request, options);
  }
  captureContextDiagnostics(request: CaptureDiagnosticsRequest, options: TransportOptions = {}): Promise<ContextDiagnosticsResponse> { return this.#request("captureContextDiagnostics", request, withDefaultDeadline(options)); }
  subscribeEvents(request: SubscribeEventsRequest, options: TransportOptions = {}): Promise<SubscribeEventsResponse> { return this.#request("subscribeEvents", request, withDefaultDeadline(options)); }
  unsubscribeEvents(request: UnsubscribeEventsRequest, options: TransportOptions = {}): Promise<unknown> { return this.#request("unsubscribeEvents", request, withDefaultDeadline(options)); }
  registerEventListener(id: string, listener: (event: string, envelope: EventEnvelope) => void): void {
    const state = this.#eventListeners.get(id) ?? {queue: [], scheduled: false, dropped: 0};
    state.listener = listener; this.#eventListeners.set(id, state); this.#scheduleEventDrain(id, state);
  }
  removeEventListener(id: string): void { this.#eventListeners.delete(id); }
  eventful<Result = unknown>(request: EventfulRequest, options: TransportOptions = {}): Promise<Result> {
    return this.#request("eventful", request, options);
  }
  registerResponseCallback(id: string, callback: ResponseCallback): void { this.#callbacks.set(id, callback); }
  removeResponseCallback(id: string): void { this.#callbacks.delete(id); }
  registeredCallbackCount(): number { return this.#callbacks.size; }

  async #request<Result>(method: string, params: object, options: TransportOptions): Promise<Result> {
    if (this.#closed) throw new BilobaError({code: "DRIVER_CLOSED", message: "Biloba transport is closed"});
    if (options.signal?.aborted) throw abortedError(options.signal.reason);
    const id = this.#nextID++;
    return await new Promise<Result>((resolve, reject) => {
      let timer: NodeJS.Timeout | undefined;
      const abort = () => {
        this.#pending.delete(id);
        cleanup();
        this.#cancel(id);
        reject(abortedError(options.signal?.reason));
      };
      const cleanup = () => {
        if (timer) clearTimeout(timer);
        options.signal?.removeEventListener("abort", abort);
      };
      this.#pending.set(id, {
        resolve: (value) => resolve(value as Result),
        reject,
        cleanup,
      });
      options.signal?.addEventListener("abort", abort, {once: true});
      const deadlineMs = options.deadlineMs ?? options.timeoutMs;
      if (deadlineMs !== undefined) {
        timer = setTimeout(() => {
          this.#pending.delete(id);
          cleanup();
          this.#cancel(id);
          reject(new BilobaError({code: "TIMEOUT", message: `Biloba request timed out after ${deadlineMs}ms`}));
        }, deadlineMs);
      }
      void this.#write({
        id,
        method,
        params,
        ...(deadlineMs !== undefined && {timeoutMs: deadlineMs}),
      }).catch((error: unknown) => {
        const pending = this.#pending.get(id);
        if (!pending) return;
        this.#pending.delete(id);
        pending.cleanup();
        pending.reject(error);
      });
    });
  }

  // Cancellation is best effort: the caller's promise is already being rejected locally, so a cancel
  // frame that cannot be written (closed transport, broken pipe) is nothing to report - and an
  // unhandled rejection here would take the whole worker process down.
  #cancel(id: number): void {
    this.#write({id: 0, method: "cancel", params: {requestId: id}}).catch(() => undefined);
  }

  #receive(response: Response): void {
    const pending = this.#pending.get(response.id);
    if (!pending) return;
    this.#pending.delete(response.id);
    pending.cleanup();
    if (response.error) pending.reject(protocolError(response.error));
    else if (response.result === undefined) pending.reject(new BilobaError({code: "DRIVER_ERROR", message: "Biloba response contained no result"}));
    else pending.resolve(response.result);
  }

  async #receiveEvent(frame: EventFrame): Promise<void> {
    if (frame.params && typeof frame.params === "object" && "subscriptionId" in frame.params) {
      const envelope = frame.params as EventEnvelope;
      const state = this.#eventListeners.get(envelope.subscriptionId) ?? {queue: [], scheduled: false, dropped: 0};
      if (state.queue.length === eventQueueLimit) state.dropped++;
      else state.queue.push({event: frame.event, envelope});
      this.#eventListeners.set(envelope.subscriptionId, state);
      this.#scheduleEventDrain(envelope.subscriptionId, state);
      return;
    }
    if (frame.event !== "responseIntercepted" || !frame.invocationId || !frame.callbackId) return;
    const callback = this.#callbacks.get(frame.callbackId);
    if (!callback) {
      await this.#callbackResult({invocationId: frame.invocationId, error: "response callback is no longer registered"});
      return;
    }
    try {
      const result = await callback(frame.payload);
      await this.#callbackResult({invocationId: frame.invocationId, result});
    } catch (error) {
      await this.#callbackResult({invocationId: frame.invocationId, error: error instanceof Error ? error.message : String(error)});
    }
  }

  #scheduleEventDrain(id: string, state: EventListenerState): void {
    if (!state.listener || state.scheduled || (state.queue.length === 0 && state.dropped === 0)) return;
    state.scheduled = true;
    queueMicrotask(() => {
      state.scheduled = false;
      const listener = state.listener; if (!listener) return;
      if (state.dropped > 0) {
        const dropped = state.dropped; state.dropped = 0;
        try { listener("eventsDropped", {subscriptionId: id, sequence: 0, payload: {code: "EVENTS_DROPPED", message: `${dropped} live events were dropped`, details: {count: dropped}}}); } catch { /* listener isolation */ }
      }
      const queued = state.queue.splice(0);
      for (const item of queued) { try { listener(item.event, item.envelope); } catch { /* listener isolation */ } }
      this.#scheduleEventDrain(id, state);
    });
  }

  async #callbackResult(params: {invocationId: string; result?: WireNetworkOverride; error?: string}): Promise<void> {
    try { await this.#write({id: 0, method: "callbackResult", params}); }
    catch (error) {
      if (params.error !== undefined) return;
      await this.#write({id: 0, method: "callbackResult", params: {invocationId: params.invocationId, error: error instanceof Error ? error.message : String(error)}}).catch(() => undefined);
    }
  }

  #write(value: unknown): Promise<void> {
    const write = async () => {
      if (this.#closed) throw new BilobaError({code: "DRIVER_CLOSED", message: "Biloba transport is closed"});
      const frame = encodeFrame(value);
      if (this.#output.write(frame)) return;
      await new Promise<void>((resolve, reject) => {
        const cleanup = () => { this.#output.removeListener("drain", drained); this.#output.removeListener("error", failed); };
        const drained = () => { cleanup(); resolve(); };
        const failed = (error: Error) => { cleanup(); reject(error); };
        this.#output.once("drain", drained);
        this.#output.once("error", failed);
      });
    };
    const operation = this.#writeChain.then(write);
    this.#writeChain = operation.catch(() => undefined);
    return operation;
  }
}

function protocolError(error: ProtocolError): BilobaError {
  return new BilobaError({
    code: error.code,
    message: error.message,
    ...(error.diagnostics?.locator && {locator: error.diagnostics.locator}),
    ...(error.diagnostics?.expected && {expected: error.diagnostics.expected}),
    ...(error.diagnostics?.domOutline && {domOutline: error.diagnostics.domOutline}),
    ...(error.diagnostics?.screenshotPath && {screenshotPath: error.diagnostics.screenshotPath}),
    ...(error.diagnostics?.daemonDetail && {daemonDetail: error.diagnostics.daemonDetail}),
    ...(error.diagnostics?.context && {diagnostics: decodeContextDiagnostics(error.diagnostics.context)}),
    ...(error.diagnostics?.visual && {visual: error.diagnostics.visual as import("../index.js").VisualResult, artifactPaths: visualArtifactPaths(error.diagnostics.visual as import("../index.js").VisualResult)}),
  });
}

function decodeContextDiagnostics(value: import("../generated/protocol.js").ContextDiagnosticsResponse): import("../index.js").ContextDiagnostics {
  return {purpose: value.purpose as import("../index.js").DiagnosticsPurpose, ...(value.artifactDir && {artifactDir: value.artifactDir}), tabs: value.tabs.map((tab) => ({...(tab.sessionId && {sessionId: tab.sessionId}), targetId: tab.targetId, title: tab.title, ...(tab.screenshotPath && {screenshotPath: tab.screenshotPath}), ...(tab.screenshotBase64 && {screenshot: decodeDiagnosticBase64(tab.screenshotBase64)}), ...(tab.outlinePath && {outlinePath: tab.outlinePath}), ...(tab.domOutline && {domOutline: tab.domOutline}), errors: tab.errors as import("../index.js").DiagnosticsArtifactError[]}))};
}

function decodeDiagnosticBase64(value: string): Uint8Array {
  if (!/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(value)) throw new BilobaError({code: "DRIVER_ERROR", message: "diagnostic screenshot is malformed base64"});
  const bytes = Buffer.from(value, "base64"); if (bytes.byteLength > 16 * 1024 * 1024) throw new BilobaError({code: "DRIVER_ERROR", message: "diagnostic screenshot exceeds decoded limit"}); return bytes;
}

function visualArtifactPaths(visual: import("../index.js").VisualResult): readonly string[] {
  return [...new Set(visual.schemes.flatMap((scheme) => [scheme.baselinePath, scheme.actualPath, scheme.diffPath].filter((path): path is string => Boolean(path))))];
}

function abortedError(reason: unknown): BilobaError {
  const suffix = reason === undefined ? "" : `: ${String(reason)}`;
  return new BilobaError({code: "CANCELLED", message: `Biloba operation cancelled${suffix}`});
}

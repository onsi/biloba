import type {Readable, Writable} from "node:stream";

import {BilobaError, type WaitOptions} from "../index.js";
import type {
  AssertRequest,
  EvaluateRequest,
  HandshakeRequest,
  HandshakeResponse,
  LocatorRequest,
  NavigateRequest,
  OpenSessionRequest,
  OpenSessionResponse,
  OperationResult,
  ProtocolError,
  Response,
  SessionRequest,
  SetCookiesRequest,
  SetValueRequest,
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

export class StdioTransport {
  readonly #input: Readable;
  readonly #output: Writable;
  readonly #decoder = new FrameDecoder();
  readonly #pending = new Map<number, PendingRequest>();
  #nextID = 1;
  #closed = false;
  #writeChain = Promise.resolve();

  constructor(input: Readable, output: Writable, options: {failOnEnd?: boolean} = {}) {
    this.#input = input;
    this.#output = output;
    input.pipe(this.#decoder);
    this.#decoder.on("data", (response: Response) => this.#receive(response));
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
  }

  handshake(request: HandshakeRequest, options: TransportOptions = {}): Promise<HandshakeResponse> {
    return this.#request("handshake", request, withDefaultDeadline(options));
  }
  openSession(request: OpenSessionRequest, options: TransportOptions = {}): Promise<OpenSessionResponse> {
    return this.#request("openSession", request, withDefaultDeadline(options));
  }
  prepareSession(request: SessionRequest, options: TransportOptions = {}): Promise<Record<string, never>> {
    return this.#request("prepareSession", request, withDefaultDeadline(options));
  }
  closeSession(request: SessionRequest, options: TransportOptions = {}): Promise<Record<string, never>> {
    return this.#request("closeSession", request, withDefaultDeadline(options));
  }
  navigate(request: NavigateRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("navigate", request, withDefaultDeadline(options));
  }
  setCookies(request: SetCookiesRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("setCookies", request, withDefaultDeadline(options));
  }
  click(request: LocatorRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("click", request, options);
  }
  setValue(request: SetValueRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("setValue", request, options);
  }
  evaluate(request: EvaluateRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("evaluate", request, withDefaultDeadline(options));
  }
  assert(request: AssertRequest, options: TransportOptions = {}): Promise<OperationResult> {
    return this.#request("assert", request, options);
  }

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
  });
}

function abortedError(reason: unknown): BilobaError {
  const suffix = reason === undefined ? "" : `: ${String(reason)}`;
  return new BilobaError({code: "CANCELLED", message: `Biloba operation cancelled${suffix}`});
}

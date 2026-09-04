import {Transform, type TransformCallback} from "node:stream";

export const MAX_FRAME_SIZE = 32 * 1024 * 1024;

export function encodeFrame(value: unknown): Buffer {
  const payload = Buffer.from(JSON.stringify(value), "utf8");
  if (payload.length === 0) throw new Error("Protocol frame is empty");
  if (payload.length > MAX_FRAME_SIZE) {
    throw new Error(`Protocol frame is ${payload.length} bytes; maximum is ${MAX_FRAME_SIZE}`);
  }
  const frame = Buffer.allocUnsafe(4 + payload.length);
  frame.writeUInt32LE(payload.length, 0);
  payload.copy(frame, 4);
  return frame;
}

export class FrameDecoder extends Transform {
  #pending = Buffer.alloc(0);

  constructor() {
    super({readableObjectMode: true});
  }

  override _transform(chunk: Buffer | string, encoding: BufferEncoding, callback: TransformCallback): void {
    try {
      const incoming = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk, encoding);
      this.#pending = this.#pending.length === 0
        ? incoming
        : Buffer.concat([this.#pending, incoming]);
      this.#decodeAvailable();
      callback();
    } catch (error) {
      callback(error as Error);
    }
  }

  override _flush(callback: TransformCallback): void {
    if (this.#pending.length === 0) {
      callback();
      return;
    }
    callback(new Error(`Protocol stream ended with ${this.#pending.length} incomplete frame bytes`));
  }

  #decodeAvailable(): void {
    while (this.#pending.length >= 4) {
      const length = this.#pending.readUInt32LE(0);
      if (length === 0) throw new Error("Protocol frame is empty");
      if (length > MAX_FRAME_SIZE) {
        throw new Error(`Protocol frame is ${length} bytes; maximum is ${MAX_FRAME_SIZE}`);
      }
      if (this.#pending.length < 4 + length) return;
      const payload = this.#pending.subarray(4, 4 + length);
      this.push(JSON.parse(payload.toString("utf8")) as unknown);
      this.#pending = this.#pending.subarray(4 + length);
    }
  }
}

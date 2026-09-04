import {once} from "node:events";
import {describe, expect, it} from "vitest";

import {encodeFrame, FrameDecoder, MAX_FRAME_SIZE} from "../src/internal/framing.js";

describe("length-prefixed JSON framing", () => {
  it("round-trips multiline values fragmented across arbitrary chunks", async () => {
    const original = {id: 17, method: "evaluate", script: "return `first\nsecond`\n"};
    const frame = encodeFrame(original);
    const decoder = new FrameDecoder();
    const decoded: unknown[] = [];
    decoder.on("data", (value: unknown) => decoded.push(value));

    for (const byte of frame) decoder.write(Buffer.of(byte));
    decoder.end();
    await once(decoder, "end");

    expect(decoded).toEqual([original]);
  });

  it("decodes consecutive frames from one chunk", async () => {
    const decoder = new FrameDecoder();
    const decoded: unknown[] = [];
    decoder.on("data", (value: unknown) => decoded.push(value));
    decoder.end(Buffer.concat([encodeFrame({id: 1}), encodeFrame({id: 2})]));
    await once(decoder, "end");

    expect(decoded).toEqual([{id: 1}, {id: 2}]);
  });

  it("rejects an oversized frame before buffering its payload", async () => {
    const decoder = new FrameDecoder();
    const error = once(decoder, "error");
    const header = Buffer.alloc(4);
    header.writeUInt32LE(MAX_FRAME_SIZE + 1);
    decoder.end(header);

    await expect(error).resolves.toMatchObject([{message: expect.stringContaining("maximum")}]);
  });
});

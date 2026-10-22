import { describe, expect, it } from "vitest";

import { encodeAttachment, fileToBase64 } from "@/lib/attachments/encode";

describe("attachments encode", () => {
  it("encodes a 10 MiB buffer without a stack overflow and round-trips", async () => {
    const bytes = new Uint8Array(10 * 1024 * 1024);
    for (let i = 0; i < bytes.length; i++) bytes[i] = (i * 31 + 7) & 0xff;
    const b64 = await fileToBase64(new Blob([bytes]));
    expect(b64.length).toBe(Math.ceil(bytes.length / 3) * 4);
    const back = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    expect(back.length).toBe(bytes.length);
    expect(back[0]).toBe(bytes[0]);
    expect(back[back.length - 1]).toBe(bytes[bytes.length - 1]);
    expect(back[1234567]).toBe(bytes[1234567]);
  });

  it("matches a single-shot encoding for a size that is not a chunk multiple", async () => {
    const bytes = Uint8Array.from({ length: 32 * 1024 * 2 + 5 }, (_, i) => i & 0xff);
    expect(await fileToBase64(new Blob([bytes]))).toBe(btoa(String.fromCharCode(...bytes.subarray(0, 0)) + Array.from(bytes, (b) => String.fromCharCode(b)).join("")));
  });

  it("builds the wire request", async () => {
    const req = await encodeAttachment({ kind: "image", name: "a.png", mimeType: "image/png", file: new Blob(["hi"]) });
    expect(req).toEqual({ kind: "image", name: "a.png", mimeType: "image/png", data: "aGk=" });
  });
});

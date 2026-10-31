import { describe, expect, it } from "vitest";

import { DEFAULT_CHUNK_BYTES, FileChangedError, FileTooLargeError, decodeBase64, readFileBytes } from "@/lib/file-bytes";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";

function b64(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s);
}

const tick = () => new Promise((r) => setTimeout(r, 0));

describe("readFileBytes", () => {
  it("assembles_sequential_chunks_pinned_to_ifMtime", async () => {
    const data = Uint8Array.from({ length: 25 }, (_, i) => i * 7);
    const client = new FakeWsClient();
    const done = readFileBytes(client, { taskId: 3, path: "a.pdf", size: 25, mtime: "T0", chunkBytes: 10 });
    for (let i = 0; i < 3; i++) {
      await tick();
      expect(client.calls).toHaveLength(i + 1); // one chunk in flight at a time
      const part = data.subarray(i * 10, Math.min(25, i * 10 + 10));
      expect(client.nth("file.readRange", i).params).toEqual({ taskId: 3, path: "a.pdf", offset: i * 10, length: 10, ifMtime: "T0" });
      client.nth("file.readRange", i).resolve({ offset: i * 10, length: part.length, eof: i === 2, data: b64(part), mtime: "T0" });
    }
    expect(await done).toEqual(data);
  });

  it("refuses_over_cap_before_asking_for_a_byte", async () => {
    const client = new FakeWsClient();
    await expect(readFileBytes(client, { taskId: 1, path: "x", size: 51, mtime: "T", maxBytes: 50 })).rejects.toBeInstanceOf(FileTooLargeError);
    expect(client.calls).toHaveLength(0);
  });

  it("maps_conflict_and_too_large_codes_and_short_files", async () => {
    let client = new FakeWsClient();
    let p = readFileBytes(client, { taskId: 1, path: "x", size: 5, mtime: "T" });
    await tick();
    client.nth("file.readRange", 0).reject(new RpcError("changed", "conflict"));
    await expect(p).rejects.toBeInstanceOf(FileChangedError);

    client = new FakeWsClient();
    p = readFileBytes(client, { taskId: 1, path: "x", size: 5, mtime: "T" });
    await tick();
    client.nth("file.readRange", 0).reject(new RpcError("big", "too_large"));
    await expect(p).rejects.toBeInstanceOf(FileTooLargeError);

    // the file ended before its stat'd size
    client = new FakeWsClient();
    p = readFileBytes(client, { taskId: 1, path: "x", size: 10, mtime: "T" });
    await tick();
    client.nth("file.readRange", 0).resolve({ offset: 0, length: 4, eof: true, data: b64(new Uint8Array(4)), mtime: "T" });
    await expect(p).rejects.toBeInstanceOf(FileChangedError);

    // an empty non-final chunk can not loop forever
    client = new FakeWsClient();
    p = readFileBytes(client, { taskId: 1, path: "x", size: 10, mtime: "T" });
    await tick();
    client.nth("file.readRange", 0).resolve({ offset: 0, length: 0, eof: false, data: "", mtime: "T" });
    await expect(p).rejects.toBeInstanceOf(FileChangedError);
  });

  it("defaults_to_the_relay_chunk_and_decodes_base64", () => {
    expect(DEFAULT_CHUNK_BYTES).toBe(512 * 1024);
    expect(decodeBase64(btoa("hiÿ"))).toEqual(Uint8Array.from([104, 105, 255]));
    expect(decodeBase64("")).toEqual(new Uint8Array(0));
  });

  it("stops_when_aborted", async () => {
    const client = new FakeWsClient();
    const ctl = new AbortController();
    ctl.abort();
    await expect(readFileBytes(client, { taskId: 1, path: "x", size: 1, mtime: "T", signal: ctl.signal })).rejects.toThrow("aborted");
    expect(client.calls).toHaveLength(0);
  });
});

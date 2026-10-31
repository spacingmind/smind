import type { FileReadRangeResult } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";
import { RpcError } from "@/lib/ws-client";

/** What a client assumes before `daemon.info.files` says otherwise: the relay's ceiling, the smallest transport. */
export const DEFAULT_CHUNK_BYTES = 512 * 1024;
export const DEFAULT_MAX_READ_BYTES = 50 * 1024 * 1024;

/** The file is larger than the daemon (or this client) will read. */
export class FileTooLargeError extends Error {
  constructor(
    readonly size: number,
    readonly limit: number,
  ) {
    super(`file is ${size} bytes, over the ${limit} byte read limit`);
    this.name = "FileTooLargeError";
  }
}

/** The file changed (or ended early) while it was being read. */
export class FileChangedError extends Error {
  constructor() {
    super("file changed while it was being read");
    this.name = "FileChangedError";
  }
}

export interface ReadFileBytesOptions {
  taskId: number;
  path: string;
  /** `file.stat`'s size and mtime: the read is pinned to this version of the file. */
  size: number;
  mtime: string;
  chunkBytes?: number;
  maxBytes?: number;
  signal?: AbortSignal;
  onProgress?: (read: number, total: number) => void;
}

/** base64 -> bytes without the per-char array of `Uint8Array.from(atob(...))`. */
export function decodeBase64(b64: string): Uint8Array {
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/**
 * Reads a whole file through `file.readRange`, one chunk at a time (sequential, so terminal input on the same socket is not queued behind a
 * bulk read). Every chunk carries `ifMtime`, so a file rewritten mid-read ends in FileChangedError instead of a mixed-version buffer.
 * Over-limit files are refused before the first byte is requested.
 */
export async function readFileBytes(client: WsClientLike, o: ReadFileBytesOptions): Promise<Uint8Array> {
  const limit = o.maxBytes ?? DEFAULT_MAX_READ_BYTES;
  if (o.size > limit) throw new FileTooLargeError(o.size, limit);
  const chunkBytes = Math.max(1, o.chunkBytes ?? DEFAULT_CHUNK_BYTES);

  const chunks: Uint8Array[] = [];
  let read = 0;
  for (;;) {
    if (o.signal?.aborted) throw new DOMException("aborted", "AbortError");
    let res: FileReadRangeResult;
    try {
      res = await client.call<FileReadRangeResult>(
        "file.readRange",
        { taskId: o.taskId, path: o.path, offset: read, length: chunkBytes, ifMtime: o.mtime },
        { signal: o.signal },
      );
    } catch (err) {
      if (err instanceof RpcError && err.code === "too_large") throw new FileTooLargeError(o.size, limit);
      if (err instanceof RpcError && err.code === "conflict") throw new FileChangedError();
      throw err;
    }
    const bytes = decodeBase64(res.data ?? "");
    if (bytes.length > 0) chunks.push(bytes);
    read += bytes.length;
    if (read > limit) throw new FileTooLargeError(read, limit);
    o.onProgress?.(read, o.size);
    if (res.eof) break;
    // A non-final empty chunk would loop forever.
    if (bytes.length === 0) throw new FileChangedError();
  }
  if (read !== o.size) throw new FileChangedError();

  const out = new Uint8Array(read);
  let at = 0;
  for (const c of chunks) {
    out.set(c, at);
    at += c.length;
  }
  return out;
}

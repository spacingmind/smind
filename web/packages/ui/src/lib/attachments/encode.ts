import type { AttachmentRequest } from "@/lib/types";

/** A multiple of 3 (30 KiB), so every chunk but the last encodes without padding and the pieces concatenate into one valid string. */
const CHUNK = 30 * 1024;

/** Standard base64 of a file's bytes. Chunked so a 10 MiB file never spreads into one `String.fromCharCode` call. */
export async function fileToBase64(file: Blob): Promise<string> {
  const bytes = new Uint8Array(await file.arrayBuffer());
  let out = "";
  for (let i = 0; i < bytes.length; i += CHUNK) {
    out += btoa(String.fromCharCode(...bytes.subarray(i, i + CHUNK)));
  }
  return out;
}

/**
 * Encodes one draft item at submit time (never on add: ADR-0038 §6).
 */
export async function encodeAttachment(
  item:
    | { kind: "image" | "file"; name: string; mimeType: string; file: Blob }
    | { kind: "pr" | "issue"; forge: "github"; number: number; repo: string },
): Promise<AttachmentRequest> {
  // A PR/issue is a reference the daemon resolves: nothing to read.
  if (!("file" in item)) return { kind: item.kind, forge: item.forge, number: item.number, repo: item.repo };
  return { kind: item.kind, name: item.name, mimeType: item.mimeType, data: await fileToBase64(item.file) };
}

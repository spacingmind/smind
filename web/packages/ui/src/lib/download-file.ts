import { readFileBytes, type ReadFileBytesOptions } from "@/lib/file-bytes";
import type { WsClientLike } from "@/lib/ws-client";

/** Above this size the UI shows download progress (ADR-0049 section 5.2). */
export const PROGRESS_THRESHOLD_BYTES = 5 * 1024 * 1024;
/** The longest name a download is saved under. */
export const MAX_DOWNLOAD_NAME = 120;

/**
 * The name a downloaded file is saved under: the basename only, with control characters, path separators and the characters Windows
 * forbids in a name stripped, no leading or trailing dots or spaces, at most 120 characters (the extension is kept when the name is
 * cut), and "download" when nothing is left. A path the agent chose can never steer where the browser puts the file.
 */
export function sanitizeDownloadName(path: string): string {
  const base = path.split(/[\\/]/).pop() ?? "";
  const clean = base
    .replace(/[\u0000-\u001f\u007f\u0080-\u009f<>:"|?*\\/]/g, "")
    .replace(/^[. ]+/, "")
    .replace(/[. ]+$/, "");
  if (clean === "") return "download";
  if (clean.length <= MAX_DOWNLOAD_NAME) return clean;
  const dot = clean.lastIndexOf(".");
  const ext = dot > 0 && clean.length - dot <= 16 ? clean.slice(dot) : "";
  return clean.slice(0, MAX_DOWNLOAD_NAME - ext.length) + ext;
}

/** Hands `blob` to the browser as a download named `name` and revokes the object URL straight away. */
export function saveBlob(blob: Blob, name: string): void {
  const url = URL.createObjectURL(blob);
  try {
    const a = document.createElement("a");
    a.href = url;
    a.download = name;
    a.rel = "noopener";
    a.style.display = "none";
    document.body.appendChild(a);
    a.click();
    a.remove();
  } finally {
    URL.revokeObjectURL(url);
  }
}

export type DownloadOptions = ReadFileBytesOptions;

/**
 * Downloads a task file: assembles it with `file.readRange` (every chunk pinned to the stat'd mtime, so a file rewritten mid-download
 * fails instead of being saved half old and half new; over the 50 MiB read limit it is refused before a byte is read), builds a Blob
 * and saves it under a sanitised basename. `save` is injectable for tests.
 */
export async function downloadFile(client: WsClientLike, o: DownloadOptions, save: (blob: Blob, name: string) => void = saveBlob): Promise<void> {
  const bytes = await readFileBytes(client, o);
  save(new Blob([bytes as BlobPart]), sanitizeDownloadName(o.path));
}

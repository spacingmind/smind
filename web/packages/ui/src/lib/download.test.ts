import { afterEach, describe, expect, it, vi } from "vitest";

import { MAX_DOWNLOAD_NAME, downloadFile, sanitizeDownloadName, saveBlob } from "@/lib/download-file";
import { FileChangedError, FileTooLargeError } from "@/lib/file-bytes";
import { RpcError } from "@/lib/ws-client";
import { FakeWsClient } from "@/test/fake-ws-client";

function b64(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s);
}
const tick = () => new Promise((r) => setTimeout(r, 0));

afterEach(() => vi.restoreAllMocks());

describe("download", () => {
  it("assembles_with_ifMtime", async () => {
    const data = Uint8Array.from({ length: 25 }, (_, i) => (i * 11) % 256);
    const client = new FakeWsClient();
    const saved: Array<{ blob: Blob; name: string }> = [];
    const progress: number[] = [];
    const done = downloadFile(client, { taskId: 4, path: "out/Q3 report.pdf", size: 25, mtime: "T0", chunkBytes: 10, onProgress: (r) => progress.push(r) }, (blob, name) => saved.push({ blob, name }));
    for (let i = 0; i < 3; i++) {
      await tick();
      // every chunk is pinned to the mtime of the stat the download started from
      expect(client.nth("file.readRange", i).params).toEqual({ taskId: 4, path: "out/Q3 report.pdf", offset: i * 10, length: 10, ifMtime: "T0" });
      const part = data.subarray(i * 10, Math.min(25, i * 10 + 10));
      client.nth("file.readRange", i).resolve({ offset: i * 10, length: part.length, eof: i === 2, data: b64(part), mtime: "T0" });
    }
    await done;
    expect(saved).toHaveLength(1);
    expect(saved[0].name).toBe("Q3 report.pdf");
    expect(new Uint8Array(await saved[0].blob.arrayBuffer())).toEqual(data);
    expect(progress).toEqual([10, 20, 25]);
  });

  it("a_file_rewritten_mid_download_is_not_saved", async () => {
    const client = new FakeWsClient();
    const save = vi.fn();
    const done = downloadFile(client, { taskId: 1, path: "a.pdf", size: 20, mtime: "T0", chunkBytes: 10 }, save);
    await tick();
    client.nth("file.readRange", 0).resolve({ offset: 0, length: 10, eof: false, data: b64(new Uint8Array(10)), mtime: "T0" });
    await tick();
    client.nth("file.readRange", 1).reject(new RpcError("changed", "conflict"));
    await expect(done).rejects.toBeInstanceOf(FileChangedError);
    expect(save).not.toHaveBeenCalled();
  });

  it("refuses_over_cap", async () => {
    const client = new FakeWsClient();
    const save = vi.fn();
    await expect(downloadFile(client, { taskId: 1, path: "big.pdf", size: 50 * 1024 * 1024 + 1, mtime: "T0" }, save)).rejects.toBeInstanceOf(FileTooLargeError);
    expect(client.calls).toHaveLength(0); // not one byte requested
    expect(save).not.toHaveBeenCalled();
    // exactly at the cap is allowed through to the first request
    const at = downloadFile(client, { taskId: 1, path: "edge.pdf", size: 50 * 1024 * 1024, mtime: "T0" }, save);
    await tick();
    expect(client.calls).toHaveLength(1);
    client.nth("file.readRange", 0).reject(new RpcError("x", "not_found"));
    await expect(at).rejects.toThrow();
  });

  it("revokes_url", () => {
    const create = vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:fake-1");
    const revoke = vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined);
    const clicks: Array<{ href: string; download: string }> = [];
    const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(function (this: HTMLAnchorElement) {
      clicks.push({ href: this.href, download: this.download });
    });
    const before = document.querySelectorAll("a").length;
    saveBlob(new Blob(["x"]), "file.pdf");
    expect(create).toHaveBeenCalledTimes(1);
    expect(click).toHaveBeenCalledTimes(1);
    expect(clicks[0]).toEqual({ href: "blob:fake-1", download: "file.pdf" });
    expect(revoke).toHaveBeenCalledWith("blob:fake-1"); // revoked straight away, not left for later
    expect(document.querySelectorAll("a").length).toBe(before); // the temporary link is removed
    // even when the click throws, the URL is revoked
    click.mockImplementation(() => {
      throw new Error("blocked");
    });
    revoke.mockClear();
    expect(() => saveBlob(new Blob(["y"]), "f.pdf")).toThrow("blocked");
    expect(revoke).toHaveBeenCalledTimes(1);
  });

  it("sanitises_filename", () => {
    const table: Array<[string, string]> = [
      ["out/report.pdf", "report.pdf"],
      [String.raw`C:\Users\u\wt\out\report.pdf`, "report.pdf"],
      ["a/b\\c/d.txt", "d.txt"],
      ["My report (final).pdf", "My report (final).pdf"],
      ["tab\there\u0000nul\u001fcr\rlf\n.pdf", "tabherenulcrlf.pdf"], // control characters stripped
      ["bad<>:\"|?*name.pdf", "badname.pdf"], // characters Windows forbids
      ["..", "download"],
      ["...", "download"],
      ["", "download"],
      ["out/", "download"], // a trailing separator leaves no basename
      [".hidden.pdf", "hidden.pdf"], // no leading dots
      ["name.pdf.", "name.pdf"], // no trailing dots
      ["  spaced  .pdf  ", "spaced  .pdf"],
      ["\u0085\u009f.pdf", "pdf"], // C1 controls stripped, then the leading dot
      ["../../etc/passwd", "passwd"],
    ];
    for (const [input, want] of table) expect(sanitizeDownloadName(input), JSON.stringify(input)).toBe(want);

    // cap: 120 characters, keeping the extension
    const long = "a".repeat(300) + ".pdf";
    const out = sanitizeDownloadName(long);
    expect(out.length).toBe(MAX_DOWNLOAD_NAME);
    expect(out.endsWith(".pdf")).toBe(true);
    expect(sanitizeDownloadName("b".repeat(300)).length).toBe(MAX_DOWNLOAD_NAME);
    // an absurdly long "extension" is not preserved, still capped
    expect(sanitizeDownloadName("c" + "." + "d".repeat(300)).length).toBeLessThanOrEqual(MAX_DOWNLOAD_NAME);
    // never a separator in the result, whatever the input
    for (const nasty of ["a/b", "a\\b", "/", "\\", "a/../b", "..\\..\\x"]) expect(sanitizeDownloadName(nasty)).not.toMatch(/[\\/]/);
  });
});

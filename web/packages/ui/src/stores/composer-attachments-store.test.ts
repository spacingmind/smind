import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { attachmentDraftKey, useComposerAttachmentsStore } from "@/stores/composer-attachments-store";
import { attachmentErrorMessage } from "@/lib/attachments/errors";
import { RpcError } from "@/lib/ws-client";
import type { AttachmentLimits } from "@/lib/types";

const limits: AttachmentLimits = {
  maxCount: 3,
  maxImageBytes: 100,
  maxFileBytes: 200,
  maxTotalBytes: 250,
  maxMessageBytes: 16 << 20,
  images: ["image/png", "image/jpeg", "image/gif", "image/webp"],
};
const key = attachmentDraftKey(1, 2);
const file = (name: string, type: string, size: number) => new File([new Uint8Array(size)], name, { type });
const items = () => useComposerAttachmentsStore.getState().byKey[key] ?? [];

describe("composer attachments store", () => {
  const revoked: string[] = [];
  beforeEach(() => {
    revoked.length = 0;
    let n = 0;
    vi.stubGlobal("URL", { createObjectURL: () => `blob:${++n}`, revokeObjectURL: (u: string) => revoked.push(u) });
    useComposerAttachmentsStore.setState({ byKey: {} });
  });
  afterEach(() => vi.unstubAllGlobals());

  it("classifies images by the allow-list and everything else as a file", () => {
    useComposerAttachmentsStore.getState().add(key, [file("a.png", "image/png", 10), file("b.svg", "image/svg+xml", 10), file("c.pdf", "application/pdf", 10)], limits);
    expect(items().map((i) => [i.kind, !!i.previewUrl, i.error])).toEqual([
      ["image", true, undefined],
      ["file", false, undefined],
      ["file", false, undefined],
    ]);
  });

  it("adds over-cap items with the reason instead of dropping them", () => {
    useComposerAttachmentsStore.getState().add(key, [file("big.png", "image/png", 150), file("f1", "text/plain", 150), file("f2", "text/plain", 150), file("f3", "text/plain", 1)], limits);
    expect(items().map((i) => i.error)).toEqual(["Larger than 100 B", undefined, "Over the 250 B total", undefined]);
    // The oversize image did not use the count or total: f3 is the third accepted item.
  });

  it("flags items past the count cap, and removing one frees the slot", () => {
    const s = useComposerAttachmentsStore.getState();
    s.add(key, [1, 2, 3, 4].map((n) => file(`f${n}`, "text/plain", 1)), limits);
    expect(items().map((i) => !!i.error)).toEqual([false, false, false, true]);
    s.remove(key, items()[0].localId, limits);
    expect(items().map((i) => !!i.error)).toEqual([false, false, false]);
  });

  it("names pasted images clipboard-<n> and revokes URLs on remove and clear", () => {
    const s = useComposerAttachmentsStore.getState();
    s.add(key, [file("image.png", "image/png", 5), file("image.png", "image/png", 5)], limits);
    expect(items().map((i) => i.name)).toEqual(["clipboard-1.png", "clipboard-2.png"]);
    s.remove(key, items()[0].localId);
    expect(revoked).toEqual(["blob:1"]);
    s.clear(key);
    expect(revoked).toEqual(["blob:1", "blob:2"]);
    expect(items()).toEqual([]);
  });
});

describe("attachmentErrorMessage", () => {
  it("maps codes to short text and falls back to the daemon message", () => {
    expect(attachmentErrorMessage(new RpcError("attachment 2 too big", "attachment_too_large"), limits)).toContain("too large");
    expect(attachmentErrorMessage(new RpcError("x", "mime_mismatch"), limits)).toContain("do not match");
    expect(attachmentErrorMessage(new RpcError("bad b64", "invalid_base64"), limits)).toBe("Could not read an attachment.");
    expect(attachmentErrorMessage(new RpcError("raw daemon text", "forge_cli_missing"), limits)).toBe("GitHub is unavailable: gh is not installed on the daemon host.");
    expect(attachmentErrorMessage(new RpcError("raw daemon text", "forge_unauthenticated"), limits)).toBe("GitHub is unavailable: gh is not logged in on the daemon host.");
    expect(attachmentErrorMessage(new RpcError("kept", "forge_error"), limits)).toBe("kept");
    expect(attachmentErrorMessage(new Error("boom"), null)).toBe("boom");
  });
});

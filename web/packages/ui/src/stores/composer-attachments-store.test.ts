import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { attachmentDraftKey, useComposerAttachmentsStore, type DraftFile } from "@/stores/composer-attachments-store";
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
const items = () => (useComposerAttachmentsStore.getState().byKey[key] ?? []) as DraftFile[];
const all = () => useComposerAttachmentsStore.getState().byKey[key] ?? [];
const pr = (number: number) => ({ kind: "pr" as const, forge: "github" as const, number, repo: "o/r", title: `PR ${number}`, url: `https://github.com/o/r/pull/${number}` });

describe("composer attachments store", () => {
  const revoked: string[] = [];
  beforeEach(() => {
    revoked.length = 0;
    let n = 0;
    vi.stubGlobal("URL", { createObjectURL: () => `blob:${++n}`, revokeObjectURL: (u: string) => revoked.push(u) });
    useComposerAttachmentsStore.setState({ byKey: {}, mentions: {} });
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

  it("holds PRs and issues next to files; they count toward maxCount but not the byte total", () => {
    useComposerAttachmentsStore.setState({ byKey: {}, mentions: {} });
    const s = useComposerAttachmentsStore.getState();
    s.addForgeRef(key, pr(1), limits);
    s.addForgeRef(key, { ...pr(2), kind: "issue" }, limits);
    s.add(key, [file("a.txt", "text/plain", 200), file("b.txt", "text/plain", 1)], limits);
    expect(all().map((i) => [i.kind, i.error])).toEqual([
      ["pr", undefined],
      ["issue", undefined],
      ["file", undefined],
      ["file", "Over the 3 attachment limit"],
    ]);
  });

  it("ignores a PR already in the tray and revalidates after removing one", () => {
    useComposerAttachmentsStore.setState({ byKey: {}, mentions: {} });
    const s = useComposerAttachmentsStore.getState();
    s.addForgeRef(key, pr(1), limits);
    s.addForgeRef(key, pr(1), limits);
    expect(all()).toHaveLength(1);
    s.add(key, [file("a", "text/plain", 1), file("b", "text/plain", 1), file("c", "text/plain", 1)], limits);
    expect(all()[3]?.error).toBeDefined();
    s.remove(key, all()[0].localId, limits);
    expect(all().every((i) => !i.error)).toBe(true);
  });

  it("remembers each mention once and clears them on request", () => {
    useComposerAttachmentsStore.setState({ byKey: {}, mentions: {} });
    const s = useComposerAttachmentsStore.getState();
    s.addMention(key, "src/a.go");
    s.addMention(key, "src/a.go");
    s.addMention(key, "b.md");
    expect(useComposerAttachmentsStore.getState().mentions[key]).toEqual([{ path: "src/a.go" }, { path: "b.md" }]);
    s.clearMentions(key);
    expect(useComposerAttachmentsStore.getState().mentions[key]).toBeUndefined();
  });

  it("keeps mentions when the tray is dropped on unmount: the draft text and its @tokens outlive the chat tab", () => {
    useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {} });
    const s = useComposerAttachmentsStore.getState();
    s.addMention(key, "src/a.go");
    s.clear(key);
    expect(useComposerAttachmentsStore.getState().mentions[key]).toEqual([{ path: "src/a.go" }]);
  });

  it("dedupes mentions on path, line and endLine", () => {
    useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {} });
    const s = useComposerAttachmentsStore.getState();
    s.rememberMention(key, { path: "a.go" });
    s.rememberMention(key, { path: "a.go", line: 1, endLine: 5 });
    s.rememberMention(key, { path: "a.go", line: 1, endLine: 5 });
    s.rememberMention(key, { path: "a.go", line: 7, endLine: 9 });
    expect(useComposerAttachmentsStore.getState().mentions[key]).toHaveLength(3);
  });

  it("queues inserts per key and hands them out once", () => {
    useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {} });
    const s = useComposerAttachmentsStore.getState();
    s.queueInsert(key, { path: "a.go" });
    s.queueInsert(key, { path: "a.go" });
    s.queueInsert(key, { path: "a.go", line: 2, endLine: 3 });
    s.queueInsert("other", { path: "z" });
    expect(s.takeInserts(key)).toEqual([{ path: "a.go" }, { path: "a.go", line: 2, endLine: 3 }]);
    expect(useComposerAttachmentsStore.getState().takeInserts(key)).toEqual([]);
    expect(useComposerAttachmentsStore.getState().pendingInserts.other).toHaveLength(1);
  });
});

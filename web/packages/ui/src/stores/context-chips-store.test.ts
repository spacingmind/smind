import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, renderHook } from "@testing-library/react";

import { attachmentDraftKey, useComposerAttachmentsStore, useContextChips } from "@/stores/composer-attachments-store";

// DT3-D AC17: chips live per task-and-chat key, survive the tray's clear(key) (the chat tab
// unmounts whenever another tab is in front) and are dropped only by their own removals or clearChips.

const key = attachmentDraftKey(1, 2);
const other = attachmentDraftKey(1, 3);
const terminal = { kind: "terminal" as const, body: "hello", blockLabel: "selection" as const, label: "Terminal selection · 1 line · 5 B" };
const fileRef = { kind: "file_ref" as const, ref: { path: "a.go" }, label: "a.go · 12 B" };

const chipsOf = (k: string) => useComposerAttachmentsStore.getState().chips[k] ?? [];

beforeEach(() => {
  useComposerAttachmentsStore.setState({ byKey: {}, mentions: {}, pendingInserts: {}, pendingTexts: {}, chips: {} });
});
afterEach(cleanup);

describe("context chips store (DT3-D)", () => {
  it("adds and removes chips per key, with local ids", () => {
    const s = useComposerAttachmentsStore.getState();
    s.addChip(key, terminal);
    s.addChip(key, fileRef);
    expect(chipsOf(key).map((c) => [c.localId, c.kind, c.label])).toEqual([
      ["chip-1", "terminal", "Terminal selection · 1 line · 5 B"],
      ["chip-2", "file_ref", "a.go · 12 B"],
    ]);

    s.removeChip(key, "chip-1");
    expect(chipsOf(key).map((c) => c.kind)).toEqual(["file_ref"]);
    // Removing the last chip drops the key entirely.
    s.removeChip(key, "chip-2");
    expect(useComposerAttachmentsStore.getState().chips[key]).toBeUndefined();
  });

  it("keeps each chat's chips independent", () => {
    const s = useComposerAttachmentsStore.getState();
    s.addChip(key, terminal);
    s.addChip(other, fileRef);
    expect(chipsOf(key)).toHaveLength(1);
    expect(chipsOf(other)).toHaveLength(1);
    s.removeChip(key, chipsOf(key)[0]!.localId);
    expect(chipsOf(other)).toHaveLength(1);
    expect(useComposerAttachmentsStore.getState().chips[key]).toBeUndefined();
  });

  it("chips survive the tray's clear(key) but not clearChips(key)", () => {
    const s = useComposerAttachmentsStore.getState();
    s.addChip(key, terminal);
    s.clear(key);
    expect(chipsOf(key)).toHaveLength(1);

    s.clearChips(key);
    expect(useComposerAttachmentsStore.getState().chips[key]).toBeUndefined();
  });

  it("clearChips leaves other keys alone", () => {
    const s = useComposerAttachmentsStore.getState();
    s.addChip(key, terminal);
    s.addChip(other, terminal);
    s.clearChips(key);
    expect(useComposerAttachmentsStore.getState().chips[key]).toBeUndefined();
    expect(chipsOf(other)).toHaveLength(1);
  });

  it("useContextChips returns a stable empty array when there is none, and the chips when there are", () => {
    const empty1 = renderHook(() => useContextChips(key)).result.current;
    const empty2 = renderHook(() => useContextChips(key)).result.current;
    expect(empty1).toEqual([]);
    expect(empty2).toBe(empty1);

    useComposerAttachmentsStore.getState().addChip(key, terminal);
    const held = renderHook(() => useContextChips(key)).result.current;
    expect(held).toHaveLength(1);
    expect(held[0]).toMatchObject({ kind: "terminal", body: "hello", blockLabel: "selection" });
  });
});

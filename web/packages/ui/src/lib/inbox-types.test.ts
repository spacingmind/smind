import { describe, expect, it } from "vitest";

import { groupInbox, type InboxItem, type InboxKind } from "@/lib/inbox-types";

describe("groupInbox", () => {
  it("an item with an unknown kind is simply not grouped", () => {
    const item = {
      key: "zzz:1",
      kind: "zzz" as unknown as InboxKind,
      title: "A kind this UI has never heard of",
      summary: "",
      createdAt: "2026-11-01T00:00:00Z",
      state: "new",
      attention: "normal",
      links: {},
    } as InboxItem;
    expect(groupInbox([item])).toEqual([]);
  });
});

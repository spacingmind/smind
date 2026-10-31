import { describe, expect, it } from "vitest";

import {
  canBack,
  canForward,
  dropEntry,
  emptyHistory,
  NAV_HISTORY_CAP,
  pushEntry,
  sameEntry,
  type NavEntry,
} from "@/lib/nav-history";

const task = (taskId: number): NavEntry => ({ view: "workspace", taskId });
const settings: NavEntry = { view: "settings", taskId: null };

describe("nav history model", () => {
  it("push appends and moves the cursor", () => {
    const h = pushEntry(pushEntry(emptyHistory(), task(1)), task(2));
    expect(h.entries).toEqual([task(1), task(2)]);
    expect(h.index).toBe(1);
    expect(sameEntry(h.entries[h.index], task(2))).toBe(true);
  });

  it("push after back truncates the forward tail", () => {
    const back = { entries: [task(1), task(2), task(3)], index: 1 };
    const h = pushEntry(back, task(4));
    expect(h.entries).toEqual([task(1), task(2), task(4)]);
    expect(h.index).toBe(2);
  });

  it("equal consecutive entry is not duplicated", () => {
    const once = pushEntry(emptyHistory(), task(1));
    expect(pushEntry(once, task(1))).toBe(once);
    const withSettings = pushEntry(once, settings);
    expect(withSettings.entries).toEqual([task(1), settings]);
    expect(pushEntry(withSettings, { view: "settings", taskId: null })).toBe(withSettings);
  });

  it("history is capped at 50 entries", () => {
    let h = emptyHistory();
    for (let i = 1; i <= 60; i++) h = pushEntry(h, task(i));
    expect(h.entries).toHaveLength(NAV_HISTORY_CAP);
    expect(h.entries[0]).toEqual(task(11));
    expect(h.index).toBe(NAV_HISTORY_CAP - 1);
  });

  it("can-back and can-forward reflect the cursor", () => {
    const empty = emptyHistory();
    expect(canBack(empty)).toBe(false);
    expect(canForward(empty)).toBe(false);
    const one = pushEntry(empty, task(1));
    expect(canBack(one)).toBe(false);
    expect(canForward(one)).toBe(false);
    const mid = { entries: [task(1), task(2), task(3)], index: 1 };
    expect(canBack(mid)).toBe(true);
    expect(canForward(mid)).toBe(true);
    expect(canBack({ ...mid, index: 0 })).toBe(false);
    expect(canForward({ ...mid, index: 2 })).toBe(false);
  });

  it("dropEntry keeps the cursor on the same entry", () => {
    const h = { entries: [task(1), task(2), task(3)], index: 2 };
    const dropped = dropEntry(h, 1);
    expect(dropped.entries).toEqual([task(1), task(3)]);
    expect(dropped.index).toBe(1);
    expect(dropped.entries[dropped.index]).toEqual(task(3));
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";

import { isValidSplitFraction, MAX_REMEMBERED_WORKSPACES, readSplitRatios, rememberSplitRatio } from "@/lib/split-ratios";
import { MIN_SPLIT_SIZE } from "@/lib/split-tree";
import { STORAGE_KEYS } from "@/lib/storage";

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

describe("split ratios per workspace (D3)", () => {
  it("split-ratio-is-remembered-per-workspace-and-direction", () => {
    expect(readSplitRatios(1)).toBeUndefined();
    rememberSplitRatio(1, "horizontal", 0.7);
    rememberSplitRatio(1, "vertical", 0.3);
    rememberSplitRatio(2, "horizontal", 0.6);
    expect(readSplitRatios(1)).toEqual({ horizontal: 0.7, vertical: 0.3 });
    expect(readSplitRatios(2)).toEqual({ horizontal: 0.6 });
    expect(readSplitRatios(3)).toBeUndefined();
    // the latest value for a direction wins, the other direction is kept
    rememberSplitRatio(1, "horizontal", 0.4);
    expect(readSplitRatios(1)).toEqual({ horizontal: 0.4, vertical: 0.3 });
  });

  it("a fraction closer than MIN_SPLIT_SIZE to an edge, or not a finite number, is ignored", () => {
    for (const bad of [0, 1, MIN_SPLIT_SIZE - 0.01, 1 - MIN_SPLIT_SIZE + 0.01, Number.NaN, Number.POSITIVE_INFINITY, -0.5]) {
      rememberSplitRatio(1, "horizontal", bad);
      expect(readSplitRatios(1), String(bad)).toBeUndefined();
      expect(window.localStorage.getItem(STORAGE_KEYS.splitRatios), `${bad} must not be written`).toBeNull();
    }
    for (const good of [MIN_SPLIT_SIZE, 0.5, 1 - MIN_SPLIT_SIZE]) {
      expect(isValidSplitFraction(good)).toBe(true);
    }
    expect(isValidSplitFraction("0.5")).toBe(false);
  });

  it("invalid-or-corrupt-storage-reads-as-no-ratio, and a bad entry loses only itself", () => {
    window.localStorage.setItem(STORAGE_KEYS.splitRatios, "{not json");
    expect(readSplitRatios(1)).toBeUndefined();
    window.localStorage.setItem(STORAGE_KEYS.splitRatios, JSON.stringify([1, 2]));
    expect(readSplitRatios(1)).toBeUndefined();
    window.localStorage.setItem(
      STORAGE_KEYS.splitRatios,
      JSON.stringify({ w1: { horizontal: 7, vertical: "x" }, w2: { horizontal: 0.6, vertical: 0.99 }, w3: "nope", w4: { horizontal: 0.25 } }),
    );
    expect(readSplitRatios(1)).toBeUndefined(); // nothing valid in it
    expect(readSplitRatios(2)).toEqual({ horizontal: 0.6 }); // the valid half survives
    expect(readSplitRatios(3)).toBeUndefined();
    expect(readSplitRatios(4)).toEqual({ horizontal: 0.25 });
  });

  it("a failing write or read never throws", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    expect(() => rememberSplitRatio(1, "horizontal", 0.6)).not.toThrow();
    vi.restoreAllMocks();
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied");
    });
    expect(readSplitRatios(1)).toBeUndefined();
  });

  it("the map is capped: the oldest workspace is dropped first, a re-set workspace becomes the newest", () => {
    for (let id = 1; id <= MAX_REMEMBERED_WORKSPACES; id++) rememberSplitRatio(id, "horizontal", 0.6);
    rememberSplitRatio(1, "horizontal", 0.7); // workspace 1 is now the newest
    rememberSplitRatio(MAX_REMEMBERED_WORKSPACES + 1, "horizontal", 0.6);
    expect(readSplitRatios(2)).toBeUndefined(); // the oldest went
    expect(readSplitRatios(1)).toEqual({ horizontal: 0.7 });
    expect(readSplitRatios(MAX_REMEMBERED_WORKSPACES + 1)).toEqual({ horizontal: 0.6 });
    expect(Object.keys(JSON.parse(window.localStorage.getItem(STORAGE_KEYS.splitRatios)!))).toHaveLength(MAX_REMEMBERED_WORKSPACES);
  });
});

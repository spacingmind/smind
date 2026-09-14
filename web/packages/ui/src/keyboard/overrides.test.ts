import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  clearOverride,
  readStoredOverrides,
  setOverride,
  SHORTCUT_OVERRIDES_STORAGE_KEY,
  writeStoredOverrides,
} from "@/keyboard/overrides";

beforeEach(() => {
  window.localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

describe("readStoredOverrides", () => {
  it("returns {} when nothing is stored", () => {
    expect(readStoredOverrides()).toEqual({});
  });

  it("round-trips a written map", () => {
    writeStoredOverrides({ "palette-open": "Mod+Shift+P" });
    expect(readStoredOverrides()).toEqual({ "palette-open": "Mod+Shift+P" });
  });

  it("returns {} for malformed JSON or the wrong shape rather than throwing", () => {
    window.localStorage.setItem(SHORTCUT_OVERRIDES_STORAGE_KEY, "{not json");
    expect(readStoredOverrides()).toEqual({});

    window.localStorage.setItem(SHORTCUT_OVERRIDES_STORAGE_KEY, JSON.stringify(["a"]));
    expect(readStoredOverrides()).toEqual({});

    window.localStorage.setItem(SHORTCUT_OVERRIDES_STORAGE_KEY, JSON.stringify({ a: 3 }));
    expect(readStoredOverrides()).toEqual({});
  });

  it("survives storage throwing", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("disabled");
    });
    expect(readStoredOverrides()).toEqual({});
  });
});

describe("writeStoredOverrides", () => {
  it("removes the key entirely for an empty map", () => {
    writeStoredOverrides({ "palette-open": "Mod+J" });
    writeStoredOverrides({});
    expect(window.localStorage.getItem(SHORTCUT_OVERRIDES_STORAGE_KEY)).toBeNull();
  });

  it("swallows a storage failure", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    expect(() => writeStoredOverrides({ a: "Mod+J" })).not.toThrow();
  });
});

describe("setOverride / clearOverride", () => {
  it("sets without mutating the input", () => {
    const base = { a: "Mod+J" };
    const next = setOverride(base, "b", "Mod+K");
    expect(base).toEqual({ a: "Mod+J" });
    expect(next).toEqual({ a: "Mod+J", b: "Mod+K" });
  });

  it("clears a present key and returns the same object for an absent one", () => {
    const base = { a: "Mod+J" };
    expect(clearOverride(base, "a")).toEqual({});
    expect(clearOverride(base, "zzz")).toBe(base);
  });
});

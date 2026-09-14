import { afterEach, describe, expect, it, vi } from "vitest";

import { readStored, writeStored } from "@/lib/storage";

const KEY = "smind:test-key";

function isNumber(value: unknown): value is number {
  return typeof value === "number";
}

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

describe("readStored", () => {
  it("returns the fallback when nothing is stored", () => {
    expect(readStored(KEY, 7, isNumber)).toBe(7);
  });

  it("round-trips a written value", () => {
    writeStored(KEY, 42);
    expect(readStored(KEY, 0, isNumber)).toBe(42);
  });

  it("returns the fallback for malformed JSON", () => {
    window.localStorage.setItem(KEY, "{not json");
    expect(readStored(KEY, 7, isNumber)).toBe(7);
  });

  it("returns the fallback for well-formed JSON of the wrong shape", () => {
    window.localStorage.setItem(KEY, JSON.stringify("a string"));
    expect(readStored(KEY, 7, isNumber)).toBe(7);
  });

  it("returns the fallback when storage throws", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("disabled");
    });
    expect(readStored(KEY, 7, isNumber)).toBe(7);
  });
});

describe("writeStored", () => {
  it("removes the key when the value is undefined", () => {
    writeStored(KEY, 5);
    writeStored(KEY, undefined);
    expect(window.localStorage.getItem(KEY)).toBeNull();
  });

  it("swallows a storage failure", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("quota");
    });
    expect(() => writeStored(KEY, 5)).not.toThrow();
  });
});

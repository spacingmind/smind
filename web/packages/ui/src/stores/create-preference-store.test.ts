import { afterEach, describe, expect, it, vi } from "vitest";

import { createPreferenceStore, reloadAllPreferenceStores } from "@/stores/create-preference-store";

describe("createPreferenceStore", () => {
  afterEach(() => {
    window.localStorage.clear();
  });

  function makeStore() {
    const read = vi.fn(() => window.localStorage.getItem("k") ?? "default");
    const write = vi.fn((v: string) => window.localStorage.setItem("k", v));
    return { store: createPreferenceStore<string>(read, write), read, write };
  }

  it("reads once at creation and persists every set through the pure writer", () => {
    const { store, read, write } = makeStore();
    expect(store.getState().value).toBe("default");
    store.getState().set("a");
    expect(store.getState().value).toBe("a");
    expect(write).toHaveBeenCalledWith("a");
    expect(read).toHaveBeenCalledTimes(1);
  });

  it("is one copy: two subscribers see the same change (the bug the per-component useState copies had)", () => {
    const { store } = makeStore();
    const seenA: string[] = [];
    const seenB: string[] = [];
    store.subscribe((s) => seenA.push(s.value));
    store.subscribe((s) => seenB.push(s.value));
    store.getState().set("x");
    expect(seenA).toEqual(["x"]);
    expect(seenB).toEqual(["x"]);
  });

  it("reload re-reads storage, and reloadAllPreferenceStores reloads every store", () => {
    const { store } = makeStore();
    store.getState().set("a");
    window.localStorage.clear();
    expect(store.getState().value).toBe("a");
    reloadAllPreferenceStores();
    expect(store.getState().value).toBe("default");
  });
});

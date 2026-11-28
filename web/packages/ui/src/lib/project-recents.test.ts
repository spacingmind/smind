import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { FakeWsClient } from "@/test/fake-ws-client";

type ProjectRecentsModule = typeof import("@/lib/project-recents");

/** An in-memory Storage the test owns: keys are inspectable, and `blocked` makes every access throw, like a browser refusing storage. */
class MemoryLocalStorage implements Storage {
  private map = new Map<string, string>();
  blocked = false;

  reset(): void {
    this.map.clear();
    this.blocked = false;
  }

  get length(): number {
    if (this.blocked) throw new Error("localStorage is blocked");
    return this.map.size;
  }

  key(index: number): string | null {
    if (this.blocked) throw new Error("localStorage is blocked");
    return [...this.map.keys()][index] ?? null;
  }

  getItem(key: string): string | null {
    if (this.blocked) throw new Error("localStorage is blocked");
    return this.map.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    if (this.blocked) throw new Error("localStorage is blocked");
    this.map.set(key, value);
  }

  removeItem(key: string): void {
    if (this.blocked) throw new Error("localStorage is blocked");
    this.map.delete(key);
  }

  clear(): void {
    if (this.blocked) throw new Error("localStorage is blocked");
    this.map.clear();
  }
}

const storage = new MemoryLocalStorage();
const realLocalStorage = window.localStorage;

/** A fresh module instance, so each test starts with the module-level session cache gone and state re-seeded from storage. */
async function freshRecents(): Promise<ProjectRecentsModule> {
  vi.resetModules();
  return import("@/lib/project-recents");
}

beforeEach(() => {
  storage.reset();
  Object.defineProperty(window, "localStorage", { value: storage, configurable: true, writable: true });
});

afterEach(() => {
  Object.defineProperty(window, "localStorage", { value: realLocalStorage, configurable: true, writable: true });
  vi.unstubAllGlobals();
});

describe("project-recents", () => {
  it("project_recents_are_kept_per_host", async () => {
    const recents = await freshRecents();
    recents.recordRecent("conn-a", { path: "/w/alpha", title: "Alpha" });
    expect(recents.loadRecents("conn-b")).toEqual([]);

    recents.recordRecent("conn-b", { path: "/w/beta", title: "Beta" });
    // The key differs per host: one storage entry each, not one shared list.
    const storedA = storage.getItem("smind:project-recents:conn-a");
    const storedB = storage.getItem("smind:project-recents:conn-b");
    expect(storedA).toContain("/w/alpha");
    expect(storedB).toContain("/w/beta");
    expect(storedA).not.toBe(storedB);

    // Reload: the module-level session cache is gone; both hosts' recents come back from storage.
    const reloaded = await freshRecents();
    expect(reloaded.loadRecents("conn-a").map((r) => r.path)).toEqual(["/w/alpha"]);
    expect(reloaded.loadRecents("conn-b").map((r) => r.path)).toEqual(["/w/beta"]);
  });

  it("project_recents_keep_removed_projects", async () => {
    const recents = await freshRecents();
    const one = { Path: "/w/one", Title: "One", CreatedAt: "2026-01-02T03:04:05Z" };
    const two = { Path: "/w/two", Title: "Two", CreatedAt: "2026-02-02T03:04:05Z" };
    recents.mergeWorkspaces("host", [one, two]);
    expect(recents.loadRecents("host").map((r) => r.path)).toEqual(["/w/two", "/w/one"]);

    // "one" is no longer a workspace: merging again without it must not drop it from recents.
    recents.mergeWorkspaces("host", [two]);
    const paths = recents.loadRecents("host").map((r) => r.path);
    expect(paths).toContain("/w/one");
    expect(paths).toContain("/w/two");

    // The removed project IS offered as a recent; the surviving workspace is not (it is in the sidebar).
    expect(recents.visibleRecents("host", [two]).map((r) => r.path)).toEqual(["/w/one"]);
  });

  it("project_recents_remove_does_not_delete_the_project", async () => {
    const recents = await freshRecents();
    const one = { Path: "/w/one", Title: "One", CreatedAt: "2026-01-02T03:04:05Z" };
    recents.mergeWorkspaces("host", [one]);
    expect(recents.loadRecents("host").map((r) => r.path)).toEqual(["/w/one"]);

    // removeRecent's signature is (hostKey, path) -- no client parameter anywhere in the module.
    expect(recents.removeRecent.length).toBe(2);
    const fetchSpy = vi.fn();
    const webSocketSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);
    vi.stubGlobal("WebSocket", webSocketSpy);
    const client = new FakeWsClient();
    const asHostKey = client as unknown as string;
    recents.loadRecents(asHostKey);
    recents.recordRecent(asHostKey, { path: "/w/probe", title: "Probe" });
    recents.mergeWorkspaces(asHostKey, [{ Path: "/w/probe", Title: "Probe", CreatedAt: "2026-01-01T00:00:00Z" }]);
    recents.visibleRecents(asHostKey, [{ Path: "/w/probe", Title: "Probe", CreatedAt: "2026-01-01T00:00:00Z" }]);
    recents.removeRecent(asHostKey, "/w/probe");

    recents.removeRecent("host", "/w/one");
    expect(recents.loadRecents("host")).toEqual([]);

    // The workspace still exists on the daemon; merging the list again must not resurrect the entry.
    recents.mergeWorkspaces("host", [one]);
    expect(recents.loadRecents("host")).toEqual([]);

    // Nothing reached the network: the fake client saw no calls, and neither fetch nor WebSocket was touched.
    expect(client.calls).toHaveLength(0);
    expect(fetchSpy).not.toHaveBeenCalled();
    expect(webSocketSpy).not.toHaveBeenCalled();
  });

  it("caps the list at the newest 8", async () => {
    const recents = await freshRecents();
    for (let i = 0; i < 10; i++) recents.recordRecent("host", { path: `/w/p${i}`, title: `P${i}` }, 1000 + i);
    const paths = recents.loadRecents("host").map((r) => r.path);
    expect(paths).toHaveLength(8);
    expect(paths[0]).toBe("/w/p9");
    expect(paths).not.toContain("/w/p0");
    expect(paths).not.toContain("/w/p1");
  });

  it("upserts by normalised path and moves the project to the top", async () => {
    const recents = await freshRecents();
    recents.recordRecent("host", { path: "/w/a", title: "A" }, 100);
    recents.recordRecent("host", { path: "/w/b", title: "B" }, 200);
    recents.recordRecent("host", { path: "/w/c", title: "C" }, 300);
    expect(recents.loadRecents("host").map((r) => r.path)).toEqual(["/w/c", "/w/b", "/w/a"]);

    recents.recordRecent("host", { path: "/w/a/", title: "A2" }, 400);
    const items = recents.loadRecents("host");
    expect(items.map((r) => r.path)).toEqual(["/w/a", "/w/c", "/w/b"]);
    expect(items[0].title).toBe("A2");
  });

  it("falls back to memory, never throwing, when storage is blocked", async () => {
    storage.blocked = true;
    const recents = await freshRecents();
    expect(() => recents.loadRecents("host")).not.toThrow();
    expect(() => recents.recordRecent("host", { path: "/w/a", title: "A" })).not.toThrow();
    expect(() => recents.recordRecent("host", { path: "/w/b", title: "B" })).not.toThrow();
    expect(() => recents.removeRecent("host", "/w/a")).not.toThrow();
    expect(recents.loadRecents("host").map((r) => r.path)).toEqual(["/w/b"]);
  });

  it("recording a dismissed project clears the dismissal", async () => {
    const recents = await freshRecents();
    const one = { Path: "/w/one", Title: "One", CreatedAt: "2026-01-02T03:04:05Z" };
    recents.mergeWorkspaces("host", [one]);
    recents.removeRecent("host", "/w/one");

    recents.recordRecent("host", { path: "/w/one", title: "One" }, 500);
    expect(recents.loadRecents("host").map((r) => r.path)).toEqual(["/w/one"]);
    recents.mergeWorkspaces("host", [one]);
    expect(recents.loadRecents("host")).toHaveLength(1);
  });

  it("reads a corrupt or stale stored payload as empty instead of throwing", async () => {
    storage.setItem("smind:project-recents:host", "{not json");
    storage.setItem("smind:project-recents:other", JSON.stringify({ v: 2, items: [{ path: "/w/x" }], dismissed: [] }));
    const recents = await freshRecents();
    expect(recents.loadRecents("host")).toEqual([]);
    expect(recents.loadRecents("other")).toEqual([]);
    recents.recordRecent("host", { path: "/w/a", title: "A" });
    expect(recents.loadRecents("host").map((r) => r.path)).toEqual(["/w/a"]);
  });
});

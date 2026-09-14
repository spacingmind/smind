import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { fileTab } from "@/components/tab-registry";
import { STORAGE_KEYS } from "@/lib/storage";
import { useTaskTabs } from "@/hooks/use-task-tabs";

afterEach(() => {
  window.localStorage.clear();
});

describe("useTaskTabs persistence", () => {
  it("restores a task's open tabs and active key across a fresh mount", () => {
    const first = renderHook(() => useTaskTabs());
    act(() => {
      first.result.current.ensureTask(1);
      first.result.current.openTab(1, fileTab(1, "src/app.ts"));
    });
    expect(first.result.current.tabsByTask.get(1)?.activeKey).toBe("1:file:src/app.ts");

    // A fresh hook instance against the same localStorage, as a reload produces.
    const second = renderHook(() => useTaskTabs());
    const state = second.result.current.tabsByTask.get(1);
    expect(state?.activeKey).toBe("1:file:src/app.ts");
    expect(state?.tabs.map((t) => t.key)).toEqual([
      "1:task",
      "1:files",
      "1:diff",
      "1:terminal",
      "1:file:src/app.ts",
    ]);
  });

  it("persists a close and an activate, not just an open", () => {
    const first = renderHook(() => useTaskTabs());
    act(() => {
      first.result.current.ensureTask(1);
      first.result.current.openTab(1, fileTab(1, "a.ts"));
      first.result.current.activate(1, "1:diff");
      first.result.current.closeTab(1, "1:file:a.ts");
    });

    const second = renderHook(() => useTaskTabs());
    const state = second.result.current.tabsByTask.get(1);
    expect(state?.tabs.map((t) => t.key)).toEqual(["1:task", "1:files", "1:diff", "1:terminal"]);
    expect(state?.activeKey).toBe("1:diff");
  });

  it("keeps two tasks' persisted state independent", () => {
    const first = renderHook(() => useTaskTabs());
    act(() => {
      first.result.current.openTab(1, fileTab(1, "a.ts"));
      first.result.current.openTab(2, fileTab(2, "b.ts"));
    });

    const second = renderHook(() => useTaskTabs());
    expect(second.result.current.tabsByTask.get(1)?.activeKey).toBe("1:file:a.ts");
    expect(second.result.current.tabsByTask.get(2)?.activeKey).toBe("2:file:b.ts");
  });

  it("ignores a persisted entry whose activeKey names a tab that isn't in its own list", () => {
    window.localStorage.setItem(
      STORAGE_KEYS.taskTabs,
      JSON.stringify({
        "1": {
          tabs: [{ kind: "task", key: "1:task", taskId: 1, title: "Chat", closable: false }],
          activeKey: "1:file:ghost.ts",
        },
      }),
    );

    const { result } = renderHook(() => useTaskTabs());
    expect(result.current.tabsByTask.has(1)).toBe(false);
  });

  it("ignores a persisted entry whose tab claims a different taskId than its map key", () => {
    window.localStorage.setItem(
      STORAGE_KEYS.taskTabs,
      JSON.stringify({
        "1": {
          tabs: [{ kind: "task", key: "2:task", taskId: 2, title: "Chat", closable: false }],
          activeKey: "2:task",
        },
      }),
    );

    const { result } = renderHook(() => useTaskTabs());
    expect(result.current.tabsByTask.has(1)).toBe(false);
  });

  it("falls back to an empty map for malformed storage instead of throwing", () => {
    window.localStorage.setItem(STORAGE_KEYS.taskTabs, "{not json");
    const { result } = renderHook(() => useTaskTabs());
    expect(result.current.tabsByTask.size).toBe(0);
  });

  it("caps the number of persisted tasks rather than growing without bound", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      for (let taskId = 1; taskId <= 55; taskId++) {
        result.current.ensureTask(taskId);
      }
    });

    const stored = JSON.parse(window.localStorage.getItem(STORAGE_KEYS.taskTabs)!) as Record<
      string,
      unknown
    >;
    expect(Object.keys(stored).length).toBeLessThanOrEqual(50);
    // The most recently touched task survived the eviction.
    expect(stored["55"]).toBeDefined();
  });
});

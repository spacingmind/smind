import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { baseTabForKind, fileTab } from "@/components/tab-registry";
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
    expect(first.result.current.tabsByTask.get(1)?.primary.activeKey).toBe("1:file:src/app.ts");

    // A fresh hook instance against the same localStorage, as a reload produces.
    const second = renderHook(() => useTaskTabs());
    const state = second.result.current.tabsByTask.get(1);
    expect(state?.primary.activeKey).toBe("1:file:src/app.ts");
    expect(state?.primary.tabs.map((t) => t.key)).toEqual(["1:task", "1:file:src/app.ts"]);
    expect(state?.side).toBeNull();
  });

  it("persists a close and an activate, not just an open", () => {
    const first = renderHook(() => useTaskTabs());
    act(() => {
      first.result.current.ensureTask(1);
      first.result.current.openTab(1, baseTabForKind(1, "diff"));
      first.result.current.openTab(1, fileTab(1, "a.ts"));
      first.result.current.activate(1, "1:diff");
      first.result.current.closeTab(1, "1:file:a.ts");
    });

    const second = renderHook(() => useTaskTabs());
    const state = second.result.current.tabsByTask.get(1);
    expect(state?.primary.tabs.map((t) => t.key)).toEqual(["1:task", "1:diff"]);
    expect(state?.primary.activeKey).toBe("1:diff");
  });

  it("keeps two tasks' persisted state independent", () => {
    const first = renderHook(() => useTaskTabs());
    act(() => {
      first.result.current.openTab(1, fileTab(1, "a.ts"));
      first.result.current.openTab(2, fileTab(2, "b.ts"));
    });

    const second = renderHook(() => useTaskTabs());
    expect(second.result.current.tabsByTask.get(1)?.primary.activeKey).toBe("1:file:a.ts");
    expect(second.result.current.tabsByTask.get(2)?.primary.activeKey).toBe("2:file:b.ts");
  });

  it("persists a moved-to-side tab and its pane", () => {
    const first = renderHook(() => useTaskTabs());
    act(() => {
      first.result.current.openTab(1, fileTab(1, "a.ts"));
      first.result.current.moveTab(1, "1:file:a.ts", "side");
    });

    const second = renderHook(() => useTaskTabs());
    const state = second.result.current.tabsByTask.get(1);
    expect(state?.primary.tabs.map((t) => t.key)).toEqual(["1:task"]);
    expect(state?.side?.tabs.map((t) => t.key)).toEqual(["1:file:a.ts"]);
    expect(state?.side?.activeKey).toBe("1:file:a.ts");
  });

  it("ignores a persisted entry whose activeKey names a tab that isn't in its own pane", () => {
    window.localStorage.setItem(
      STORAGE_KEYS.taskTabs,
      JSON.stringify({
        "1": {
          primary: {
            tabs: [{ kind: "task", key: "1:task", taskId: 1, title: "Chat", closable: false }],
            activeKey: "1:file:ghost.ts",
          },
          side: null,
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
          primary: {
            tabs: [{ kind: "task", key: "2:task", taskId: 2, title: "Chat", closable: false }],
            activeKey: "2:task",
          },
          side: null,
        },
      }),
    );

    const { result } = renderHook(() => useTaskTabs());
    expect(result.current.tabsByTask.has(1)).toBe(false);
  });

  it("ignores a persisted entry with the same key open in both panes at once", () => {
    window.localStorage.setItem(
      STORAGE_KEYS.taskTabs,
      JSON.stringify({
        "1": {
          primary: {
            tabs: [{ kind: "file", key: "1:file:a.ts", taskId: 1, title: "a.ts", closable: true }],
            activeKey: "1:file:a.ts",
          },
          side: {
            tabs: [{ kind: "file", key: "1:file:a.ts", taskId: 1, title: "a.ts", closable: true }],
            activeKey: "1:file:a.ts",
          },
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

describe("useTaskTabs panes (Item 6)", () => {
  it("openTab with placement \"side\" creates the side pane and leaves primary's active tab alone", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.openTab(1, fileTab(1, "a.ts"), "side");
    });

    const state = result.current.tabsByTask.get(1)!;
    expect(state.primary.activeKey).toBe("1:diff");
    expect(state.side?.tabs.map((t) => t.key)).toEqual(["1:file:a.ts"]);
    expect(state.side?.activeKey).toBe("1:file:a.ts");
  });

  it("openTab with placement \"prefer\" uses an existing side pane", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "side");
      result.current.openTab(1, fileTab(1, "b.ts"), "prefer");
    });

    const state = result.current.tabsByTask.get(1)!;
    expect(state.primary.tabs.some((t) => t.key === "1:file:b.ts")).toBe(false);
    expect(state.side?.tabs.map((t) => t.key)).toEqual(["1:file:a.ts", "1:file:b.ts"]);
  });

  it("openTab with placement \"prefer\" falls back to primary when there is no side pane", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "prefer");
    });

    const state = result.current.tabsByTask.get(1)!;
    expect(state.side).toBeNull();
    expect(state.primary.tabs.some((t) => t.key === "1:file:a.ts")).toBe(true);
  });

  it("an implicit \"prefer\" open never yanks a tab already placed in primary", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "primary");
      result.current.openTab(1, fileTab(1, "z.ts"), "side"); // give the task a side pane
      result.current.openTab(1, fileTab(1, "a.ts"), "prefer");
    });

    const state = result.current.tabsByTask.get(1)!;
    // a.ts stays in primary -- it was already placed there.
    expect(state.primary.tabs.some((t) => t.key === "1:file:a.ts")).toBe(true);
    expect(state.side?.tabs.some((t) => t.key === "1:file:a.ts")).toBe(false);
    expect(state.primary.activeKey).toBe("1:file:a.ts");
  });

  it("moveTab relocates an open tab and activates it in the destination", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "files"));
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.moveTab(1, "1:diff", "side");
    });

    const state = result.current.tabsByTask.get(1)!;
    expect(state.primary.tabs.map((t) => t.key)).toEqual(["1:task", "1:files"]);
    expect(state.side?.tabs.map((t) => t.key)).toEqual(["1:diff"]);
    expect(state.side?.activeKey).toBe("1:diff");
  });

  it("moving the primary pane's active tab activates its neighbor there", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "files"));
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.openTab(1, baseTabForKind(1, "terminal"));
      result.current.activate(1, "1:diff");
      result.current.moveTab(1, "1:diff", "side");
    });

    expect(result.current.tabsByTask.get(1)!.primary.activeKey).toBe("1:terminal");
  });

  it("moveTab back to primary is the inverse, and can empty the side pane", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.moveTab(1, "1:diff", "side");
      result.current.moveTab(1, "1:diff", "primary");
    });

    const state = result.current.tabsByTask.get(1)!;
    expect(state.side).toBeNull();
    expect(state.primary.tabs.map((t) => t.key)).toContain("1:diff");
  });

  it("closing the last side tab removes the pane", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "side");
      result.current.closeTab(1, "1:file:a.ts");
    });

    expect(result.current.tabsByTask.get(1)!.side).toBeNull();
  });

  it("closing one of several side tabs keeps the pane, active on a neighbor", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "side");
      result.current.openTab(1, fileTab(1, "b.ts"), "side");
      result.current.closeTab(1, "1:file:b.ts");
    });

    const state = result.current.tabsByTask.get(1)!;
    expect(state.side?.tabs.map((t) => t.key)).toEqual(["1:file:a.ts"]);
    expect(state.side?.activeKey).toBe("1:file:a.ts");
  });

  it("moveTab is a no-op for a tab that isn't open anywhere, or already at the target", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
    });
    const before = result.current.tabsByTask.get(1);

    act(() => {
      result.current.moveTab(1, "1:file:ghost.ts", "side");
    });
    expect(result.current.tabsByTask.get(1)).toBe(before);

    act(() => {
      result.current.moveTab(1, "1:diff", "primary"); // already in primary
    });
    expect(result.current.tabsByTask.get(1)).toBe(before);
  });

  it("activate finds the key in whichever pane holds it", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "side");
      result.current.openTab(1, baseTabForKind(1, "files")); // switch primary away from Chat first
      result.current.activate(1, "1:file:a.ts");
    });

    const state = result.current.tabsByTask.get(1)!;
    expect(state.side?.activeKey).toBe("1:file:a.ts");
    // Activating a side tab doesn't touch primary's own active tab.
    expect(state.primary.activeKey).toBe("1:files");
  });
});

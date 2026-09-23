import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";

import { baseTabForKind, fileTab } from "@/components/tab-registry";
import { useTaskTabs } from "@/hooks/use-task-tabs";
import { collectAllPanes, DEFAULT_PANE_ID, findPaneById, findPaneContainingTab, type SplitNode } from "@/lib/split-tree";
import { STORAGE_KEYS } from "@/lib/storage";

afterEach(() => {
  window.localStorage.clear();
});

/** The id of a pane other than the default one, if any exists yet. */
function otherPaneId(root: SplitNode): string | undefined {
  return collectAllPanes(root).find((pane) => pane.id !== DEFAULT_PANE_ID)?.id;
}

describe("useTaskTabs persistence", () => {
  it("restores a task's open tabs and active key across a fresh mount", () => {
    const first = renderHook(() => useTaskTabs());
    act(() => {
      first.result.current.ensureTask(1);
      first.result.current.openTab(1, fileTab(1, "src/app.ts"));
    });
    expect(findPaneById(first.result.current.tabsByTask.get(1)!.root, DEFAULT_PANE_ID)?.activeKey).toBe(
      "1:file:src/app.ts",
    );

    // A fresh hook instance against the same localStorage, as a reload produces.
    const second = renderHook(() => useTaskTabs());
    const layout = second.result.current.tabsByTask.get(1);
    expect(layout).toBeDefined();
    const primary = findPaneById(layout!.root, DEFAULT_PANE_ID);
    expect(primary?.activeKey).toBe("1:file:src/app.ts");
    expect(primary?.tabs.map((t) => t.key)).toEqual(["1:task", "1:file:src/app.ts"]);
    expect(collectAllPanes(layout!.root)).toHaveLength(1);
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
    const layout = second.result.current.tabsByTask.get(1)!;
    const primary = findPaneById(layout.root, DEFAULT_PANE_ID)!;
    expect(primary.tabs.map((t) => t.key)).toEqual(["1:task", "1:diff"]);
    expect(primary.activeKey).toBe("1:diff");
  });

  it("keeps two tasks' persisted state independent", () => {
    const first = renderHook(() => useTaskTabs());
    act(() => {
      first.result.current.openTab(1, fileTab(1, "a.ts"));
      first.result.current.openTab(2, fileTab(2, "b.ts"));
    });

    const second = renderHook(() => useTaskTabs());
    expect(findPaneById(second.result.current.tabsByTask.get(1)!.root, DEFAULT_PANE_ID)?.activeKey).toBe(
      "1:file:a.ts",
    );
    expect(findPaneById(second.result.current.tabsByTask.get(2)!.root, DEFAULT_PANE_ID)?.activeKey).toBe(
      "2:file:b.ts",
    );
  });

  it("persists a moved-to-a-second-pane tab and its pane", () => {
    const first = renderHook(() => useTaskTabs());
    act(() => {
      first.result.current.openTab(1, fileTab(1, "a.ts"));
      first.result.current.splitTab(1, "1:file:a.ts", DEFAULT_PANE_ID, "right");
    });
    const sidePaneId = otherPaneId(first.result.current.tabsByTask.get(1)!.root)!;

    const second = renderHook(() => useTaskTabs());
    const layout = second.result.current.tabsByTask.get(1)!;
    const primary = findPaneById(layout.root, DEFAULT_PANE_ID)!;
    const side = findPaneById(layout.root, sidePaneId)!;
    expect(primary.tabs.map((t) => t.key)).toEqual(["1:task"]);
    expect(side.tabs.map((t) => t.key)).toEqual(["1:file:a.ts"]);
    expect(side.activeKey).toBe("1:file:a.ts");
  });

  it("ignores a persisted entry whose tab claims a different taskId than its map key", () => {
    window.localStorage.setItem(
      STORAGE_KEYS.taskTabs,
      JSON.stringify({
        "1": {
          root: {
            kind: "pane",
            pane: {
              id: DEFAULT_PANE_ID,
              tabs: [{ kind: "task", key: "2:task", taskId: 2, title: "Chat", closable: false }],
              activeKey: "2:task",
            },
          },
          focusedPaneId: DEFAULT_PANE_ID,
        },
      }),
    );

    const { result } = renderHook(() => useTaskTabs());
    expect(result.current.tabsByTask.has(1)).toBe(false);
  });

  it("a legacy pre-tree {primary, side} blob has no root key, so normalizeLayout reseeds it to an empty default layout rather than throwing or leaking the old shape", () => {
    window.localStorage.setItem(
      STORAGE_KEYS.taskTabs,
      JSON.stringify({
        "1": {
          primary: {
            tabs: [{ kind: "task", key: "1:task", taskId: 1, title: "Chat", closable: false }],
            activeKey: "1:task",
          },
          side: null,
        },
      }),
    );

    const { result } = renderHook(() => useTaskTabs());
    const layout = result.current.tabsByTask.get(1);
    expect(layout).toBeDefined();
    expect(collectAllPanes(layout!.root)).toHaveLength(1);
    const primary = findPaneById(layout!.root, DEFAULT_PANE_ID)!;
    expect(primary.tabs).toEqual([]);
    expect(primary.activeKey).toBeNull();
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

describe("useTaskTabs panes", () => {
  it('openTab with placement "side" creates a second pane and leaves the default pane\'s active tab alone', () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.openTab(1, fileTab(1, "a.ts"), "side");
    });

    const layout = result.current.tabsByTask.get(1)!;
    const primary = findPaneById(layout.root, DEFAULT_PANE_ID)!;
    expect(primary.activeKey).toBe("1:diff");
    const sidePaneId = otherPaneId(layout.root)!;
    const side = findPaneById(layout.root, sidePaneId)!;
    expect(side.tabs.map((t) => t.key)).toEqual(["1:file:a.ts"]);
    expect(side.activeKey).toBe("1:file:a.ts");
  });

  it('openTab with placement "prefer" uses an existing second pane', () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "side");
      result.current.openTab(1, fileTab(1, "b.ts"), "prefer");
    });

    const layout = result.current.tabsByTask.get(1)!;
    const primary = findPaneById(layout.root, DEFAULT_PANE_ID)!;
    expect(primary.tabs.some((t) => t.key === "1:file:b.ts")).toBe(false);
    const sidePaneId = otherPaneId(layout.root)!;
    const side = findPaneById(layout.root, sidePaneId)!;
    expect(side.tabs.map((t) => t.key)).toEqual(["1:file:a.ts", "1:file:b.ts"]);
  });

  it('openTab with placement "prefer" falls back to the default pane when there is no other pane', () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "prefer");
    });

    const layout = result.current.tabsByTask.get(1)!;
    expect(collectAllPanes(layout.root)).toHaveLength(1);
    const primary = findPaneById(layout.root, DEFAULT_PANE_ID)!;
    expect(primary.tabs.some((t) => t.key === "1:file:a.ts")).toBe(true);
  });

  it('an implicit "prefer" open never yanks a tab already placed in the default pane', () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "primary");
      result.current.openTab(1, fileTab(1, "z.ts"), "side"); // give the task a second pane
      result.current.openTab(1, fileTab(1, "a.ts"), "prefer");
    });

    const layout = result.current.tabsByTask.get(1)!;
    const primary = findPaneById(layout.root, DEFAULT_PANE_ID)!;
    const sidePaneId = otherPaneId(layout.root)!;
    const side = findPaneById(layout.root, sidePaneId)!;
    // a.ts stays in the default pane -- it was already placed there.
    expect(primary.tabs.some((t) => t.key === "1:file:a.ts")).toBe(true);
    expect(side.tabs.some((t) => t.key === "1:file:a.ts")).toBe(false);
    expect(primary.activeKey).toBe("1:file:a.ts");
  });

  it("moveTab relocates an open tab into an existing pane and activates it there", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "files"));
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.openTab(1, fileTab(1, "a.ts"), "side"); // creates the second pane
    });
    const sidePaneId = otherPaneId(result.current.tabsByTask.get(1)!.root)!;

    act(() => {
      result.current.moveTab(1, "1:diff", sidePaneId);
    });

    const layout = result.current.tabsByTask.get(1)!;
    const primary = findPaneById(layout.root, DEFAULT_PANE_ID)!;
    const side = findPaneById(layout.root, sidePaneId)!;
    expect(primary.tabs.map((t) => t.key)).toEqual(["1:task", "1:files"]);
    expect(side.tabs.map((t) => t.key)).toEqual(["1:file:a.ts", "1:diff"]);
    expect(side.activeKey).toBe("1:diff");
  });

  it("moving the default pane's active tab activates its neighbor there", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "files"));
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.openTab(1, baseTabForKind(1, "terminal"));
      result.current.openTab(1, fileTab(1, "a.ts"), "side");
      result.current.activate(1, "1:diff");
    });
    const sidePaneId = otherPaneId(result.current.tabsByTask.get(1)!.root)!;

    act(() => {
      result.current.moveTab(1, "1:diff", sidePaneId);
    });

    const primary = findPaneById(result.current.tabsByTask.get(1)!.root, DEFAULT_PANE_ID)!;
    expect(primary.activeKey).toBe("1:terminal");
  });

  it("splitTab followed by moveTab back to the original pane is the inverse, and removes the now-empty created pane", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.splitTab(1, "1:diff", DEFAULT_PANE_ID, "right");
    });
    expect(collectAllPanes(result.current.tabsByTask.get(1)!.root)).toHaveLength(2);

    act(() => {
      result.current.moveTab(1, "1:diff", DEFAULT_PANE_ID);
    });

    const layout = result.current.tabsByTask.get(1)!;
    expect(collectAllPanes(layout.root)).toHaveLength(1);
    expect(findPaneById(layout.root, DEFAULT_PANE_ID)!.tabs.map((t) => t.key)).toContain("1:diff");
  });

  it("closing the last tab in a created second pane removes the pane", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "side");
    });
    const sidePaneId = otherPaneId(result.current.tabsByTask.get(1)!.root)!;

    act(() => {
      result.current.closeTab(1, "1:file:a.ts");
    });

    const layout = result.current.tabsByTask.get(1)!;
    expect(collectAllPanes(layout.root)).toHaveLength(1);
    expect(findPaneById(layout.root, sidePaneId)).toBeNull();
  });

  it("closing one of several tabs in a second pane keeps the pane, active on a neighbor", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "side");
      result.current.openTab(1, fileTab(1, "b.ts"), "side");
    });
    const sidePaneId = otherPaneId(result.current.tabsByTask.get(1)!.root)!;

    act(() => {
      result.current.closeTab(1, "1:file:b.ts");
    });

    const side = findPaneById(result.current.tabsByTask.get(1)!.root, sidePaneId)!;
    expect(side.tabs.map((t) => t.key)).toEqual(["1:file:a.ts"]);
    expect(side.activeKey).toBe("1:file:a.ts");
  });

  it("moveTab is a no-op for a tab that isn't open anywhere", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
    });
    const before = result.current.tabsByTask.get(1);

    act(() => {
      result.current.moveTab(1, "1:file:ghost.ts", DEFAULT_PANE_ID);
    });
    expect(result.current.tabsByTask.get(1)).toBe(before);
  });

  it("moveTab is a no-op for a target pane that doesn't exist", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
    });
    const before = result.current.tabsByTask.get(1);

    act(() => {
      result.current.moveTab(1, "1:diff", "no-such-pane");
    });
    expect(result.current.tabsByTask.get(1)).toBe(before);
  });

  it("activate finds the key in whichever pane holds it", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.openTab(1, fileTab(1, "a.ts"), "side");
      result.current.openTab(1, baseTabForKind(1, "files")); // switch the default pane away from Chat first
      result.current.activate(1, "1:file:a.ts");
    });

    const layout = result.current.tabsByTask.get(1)!;
    const sidePaneId = otherPaneId(layout.root)!;
    expect(findPaneById(layout.root, sidePaneId)?.activeKey).toBe("1:file:a.ts");
    // Activating a tab in the second pane doesn't touch the default pane's own active tab.
    expect(findPaneById(layout.root, DEFAULT_PANE_ID)?.activeKey).toBe("1:files");
  });

  it("splitTab creates a new pane in the requested direction and lands the tab there", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.splitTab(1, "1:diff", DEFAULT_PANE_ID, "down");
    });

    const layout = result.current.tabsByTask.get(1)!;
    expect(layout.root.kind).toBe("group");
    if (layout.root.kind !== "group") throw new Error("unreachable");
    expect(layout.root.group.direction).toBe("vertical");

    const sidePaneId = otherPaneId(layout.root)!;
    const side = findPaneById(layout.root, sidePaneId)!;
    expect(side.tabs.map((t) => t.key)).toEqual(["1:diff"]);
    expect(layout.focusedPaneId).toBe(sidePaneId);
    // The tab left the default pane.
    expect(findPaneById(layout.root, DEFAULT_PANE_ID)!.tabs.some((t) => t.key === "1:diff")).toBe(false);
  });

  it("splitTab in \"left\" creates a horizontal group with the new pane before the original (Item 7)", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.splitTab(1, "1:diff", DEFAULT_PANE_ID, "left");
    });

    const layout = result.current.tabsByTask.get(1)!;
    expect(layout.root.kind).toBe("group");
    if (layout.root.kind !== "group") throw new Error("unreachable");
    expect(layout.root.group.direction).toBe("horizontal");
    // "left" inserts the new pane before the target -- the default pane is
    // the second child, not the first.
    expect(layout.root.group.children[1]).toMatchObject({ kind: "pane", pane: { id: DEFAULT_PANE_ID } });

    const sidePaneId = otherPaneId(layout.root)!;
    const side = findPaneById(layout.root, sidePaneId)!;
    expect(side.tabs.map((t) => t.key)).toEqual(["1:diff"]);
    expect(layout.focusedPaneId).toBe(sidePaneId);
    expect(findPaneById(layout.root, DEFAULT_PANE_ID)!.tabs.some((t) => t.key === "1:diff")).toBe(false);
  });

  it("splitTab in \"up\" creates a vertical group with the new pane before the original (Item 7)", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.splitTab(1, "1:diff", DEFAULT_PANE_ID, "up");
    });

    const layout = result.current.tabsByTask.get(1)!;
    expect(layout.root.kind).toBe("group");
    if (layout.root.kind !== "group") throw new Error("unreachable");
    expect(layout.root.group.direction).toBe("vertical");
    expect(layout.root.group.children[1]).toMatchObject({ kind: "pane", pane: { id: DEFAULT_PANE_ID } });

    const sidePaneId = otherPaneId(layout.root)!;
    const side = findPaneById(layout.root, sidePaneId)!;
    expect(side.tabs.map((t) => t.key)).toEqual(["1:diff"]);
    expect(layout.focusedPaneId).toBe(sidePaneId);
    expect(findPaneById(layout.root, DEFAULT_PANE_ID)!.tabs.some((t) => t.key === "1:diff")).toBe(false);
  });

  it("splitTab is a silent no-op once it would exceed the tree's max depth", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
    });

    // Alternating directions forces each split to nest a new group (a
    // same-direction split reuses the existing group instead), so depth
    // grows by exactly 1 per split: 1 (pane) -> 2 -> 3 -> 4 -> 5 (== MAX_TREE_DEPTH).
    let paneId = DEFAULT_PANE_ID;
    for (const direction of ["right", "down", "right", "down"] as const) {
      act(() => {
        result.current.splitTab(1, "1:diff", paneId, direction);
      });
      const layout = result.current.tabsByTask.get(1)!;
      paneId = findPaneContainingTab(layout.root, "1:diff")!.id;
    }

    const before = result.current.tabsByTask.get(1);
    act(() => {
      result.current.splitTab(1, "1:diff", paneId, "right");
    });
    expect(result.current.tabsByTask.get(1)).toBe(before);
  });

  it("creates a third pane via two splits, each with its own tabs", () => {
    const { result } = renderHook(() => useTaskTabs());
    act(() => {
      result.current.ensureTask(1);
      result.current.openTab(1, baseTabForKind(1, "diff"));
      result.current.openTab(1, baseTabForKind(1, "terminal"));
      result.current.splitTab(1, "1:diff", DEFAULT_PANE_ID, "right");
    });
    const secondPaneId = otherPaneId(result.current.tabsByTask.get(1)!.root)!;

    act(() => {
      result.current.splitTab(1, "1:terminal", DEFAULT_PANE_ID, "down");
    });

    const layout = result.current.tabsByTask.get(1)!;
    const panes = collectAllPanes(layout.root);
    expect(panes).toHaveLength(3);

    const thirdPaneId = panes.map((p) => p.id).find((id) => id !== DEFAULT_PANE_ID && id !== secondPaneId)!;
    expect(findPaneById(layout.root, DEFAULT_PANE_ID)!.tabs.map((t) => t.key)).toEqual(["1:task"]);
    expect(findPaneById(layout.root, secondPaneId)!.tabs.map((t) => t.key)).toEqual(["1:diff"]);
    expect(findPaneById(layout.root, thirdPaneId)!.tabs.map((t) => t.key)).toEqual(["1:terminal"]);
  });

  it("resizeGroup updates a group's sizes, and they hold across a remount", () => {
    const first = renderHook(() => useTaskTabs());
    act(() => {
      first.result.current.ensureTask(1);
      first.result.current.openTab(1, baseTabForKind(1, "diff"));
      first.result.current.splitTab(1, "1:diff", DEFAULT_PANE_ID, "right");
    });
    const layoutAfterSplit = first.result.current.tabsByTask.get(1)!;
    expect(layoutAfterSplit.root.kind).toBe("group");
    if (layoutAfterSplit.root.kind !== "group") throw new Error("unreachable");
    const groupId = layoutAfterSplit.root.group.id;

    act(() => {
      first.result.current.resizeGroup(1, groupId, [0.3, 0.7]);
    });

    const resized = first.result.current.tabsByTask.get(1)!;
    if (resized.root.kind !== "group") throw new Error("unreachable");
    expect(resized.root.group.sizes[0]).toBeCloseTo(0.3);
    expect(resized.root.group.sizes[1]).toBeCloseTo(0.7);

    const second = renderHook(() => useTaskTabs());
    const reloaded = second.result.current.tabsByTask.get(1)!;
    if (reloaded.root.kind !== "group") throw new Error("unreachable");
    expect(reloaded.root.group.sizes[0]).toBeCloseTo(0.3);
    expect(reloaded.root.group.sizes[1]).toBeCloseTo(0.7);
  });
});

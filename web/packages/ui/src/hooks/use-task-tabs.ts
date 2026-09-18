import { useCallback, useState } from "react";

import { baseTabForKind, type TabEntry, type TabKind } from "@/components/tab-registry";
import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";
import {
  collectAllPanes,
  collectAllTabs,
  DEFAULT_PANE_ID,
  detachTabFromTree,
  findPaneById,
  findPaneContainingTab,
  focusTabInLayout,
  MAX_TREE_DEPTH,
  moveTabToPaneInLayout,
  normalizeLayout,
  resizeSplitInLayout,
  splitPaneEmptyInLayout,
  splitPaneInLayout,
  updatePaneInTree,
  type TaskLayout,
} from "@/lib/split-tree";

/** Any live pane id in a task's split tree -- widened from the old 2-value union now that a task can have an arbitrary number of panes. */
export type PaneId = string;

/**
 * Where an opened-or-moved tab is allowed to land. `prefer` is the
 * implicit-open case (`audit-paseo.md` §1): use an existing non-default
 * pane if one exists, else the default pane -- but never move a tab
 * already placed somewhere. `{ pane }` (Item 9) names an exact, already-
 * existing pane id -- a discriminated object rather than a 4th bare string
 * literal so it can never collide with the 3 sentinel meanings above, even
 * though today's generated pane ids (`pane-N`/`group-N`) couldn't actually
 * produce the string `"side"` or `"primary"` anyway.
 */
export type TabPlacement = "primary" | "side" | "prefer" | { pane: string };

/** The four directions a movable tab's strip entry can be split in (Item 3/7). */
export type SplitDirection = "left" | "right" | "up" | "down";

/** Kinds "Split" and cross-pane moves apply to -- Chat and Files stay pinned to the default pane (the plan's Item 6 explicitly excludes them). */
const MOVABLE_KINDS: readonly TabKind[] = ["file", "diff", "terminal"];

export function isMovableKind(kind: TabKind): boolean {
  return (MOVABLE_KINDS as readonly string[]).includes(kind);
}

/** Maps smind's own up/down `SplitDirection` naming to the tree's top/bottom vocabulary (`splitPaneInLayout`'s `position`). */
const SPLIT_DIRECTION_TO_POSITION: Record<SplitDirection, "left" | "right" | "top" | "bottom"> = {
  left: "left",
  right: "right",
  up: "top",
  down: "bottom",
};

function seedState(taskId: number): TaskLayout {
  // First visit seeds just Chat -- Files/Diff/Terminal are one "+" click
  // or command-palette entry away (both read defaultTabsForTask/
  // BASE_TAB_KINDS directly) rather than pre-opened clutter. Afterwards
  // the strip is the user's -- a persisted empty default pane (every
  // seeded tab closed) rehydrates as empty, not re-seeded, which is why
  // ensureTask keys off the map rather than off pane emptiness.
  const tab = baseTabForKind(taskId, "task");
  return {
    root: { kind: "pane", pane: { id: DEFAULT_PANE_ID, tabs: [tab], activeKey: tab.key } },
    focusedPaneId: DEFAULT_PANE_ID,
  };
}

// Mints ids for newly-created split/group nodes. Doesn't need to survive
// reload -- ids are only minted on new splits, never regenerated for
// existing panes read back from storage.
let nextNodeId = 0;
function createNodeId(prefix: "pane" | "group"): string {
  nextNodeId += 1;
  return `${prefix}-${nextNodeId}`;
}

// --- Persistence (Item 3: "today every open file tab is lost"; the
// pane-split-tree plan extends the same mechanism to an arbitrary tree of
// panes instead of a fixed primary/side pair) --------------------------
//
// The map is JSON-object-keyed by task id (JSON has no numeric object
// keys) and round-tripped through `lib/storage.ts`, so a reload restores
// every task's open tabs, which pane each is in, and which was active in
// each -- not just what the sidebar shows.

/**
 * Tasks kept in storage at once. Eviction is oldest-inserted-first, not a
 * true LRU (that would need every read to reorder, not only every write) --
 * an approximation good enough for "don't grow without bound" without the
 * bookkeeping a precise one would need.
 */
const MAX_PERSISTED_TASKS = 50;

function readPersisted(): Map<number, TaskLayout> {
  const raw = readStored(
    STORAGE_KEYS.taskTabs,
    {},
    (v): v is Record<string, unknown> => typeof v === "object" && v !== null && !Array.isArray(v),
  );
  const map = new Map<number, TaskLayout>();
  for (const [key, value] of Object.entries(raw)) {
    const taskId = Number(key);
    if (!Number.isFinite(taskId)) continue;
    const layout = normalizeLayout(value);
    // Same cross-task-leakage guard as before the tree: a hand-edited or
    // stale-build blob whose tabs claim a different taskId than the map
    // key degrades this one task to reseed-on-next-visit.
    if (collectAllTabs(layout.root).some((t) => t.taskId !== taskId)) continue;
    map.set(taskId, layout);
  }
  return map;
}

function writePersisted(map: Map<number, TaskLayout>): void {
  const obj: Record<string, TaskLayout> = {};
  for (const [taskId, layout] of map) obj[String(taskId)] = layout;
  writeStored(STORAGE_KEYS.taskTabs, obj);
}

/** Drops the oldest-inserted entries once the map exceeds the cap. */
function evictOverflow(map: Map<number, TaskLayout>): Map<number, TaskLayout> {
  if (map.size <= MAX_PERSISTED_TASKS) return map;
  const next = new Map(map);
  const overflow = next.size - MAX_PERSISTED_TASKS;
  const oldest = [...next.keys()].slice(0, overflow);
  for (const taskId of oldest) next.delete(taskId);
  return next;
}

/**
 * Per-task tab state for App.tsx's registry-driven tab strip (ADR 0004)
 * plus an arbitrary split tree of panes (pane-split-tree plan): a Map
 * keyed by task id, persisted to `localStorage` (Item 3). Every setter is
 * an updater over the previous Map and returns `prev` unchanged when the
 * requested change is already in effect, so redundant calls don't cause
 * extra renders or extra writes.
 *
 * Moving a tab between panes is a plain data move -- remove the entry
 * from one pane's array, append it to the other's -- not a remount
 * dance. The pane component underneath (TerminalPane, FileEditorPane,
 * ...) *does* fully unmount from one `<Tabs>` root and mount fresh in
 * the other, since each pane is a separate Radix Tabs tree; that is
 * exactly the same "switching tabs unmounts inactive content" contract
 * every pane already tolerates (ADR 0004), and every pane's own
 * detach-not-stop mount effect (attach-if-exists, else create) already
 * makes that safe -- see `terminal-pane.tsx`'s `terminal.list` ->
 * `attach`-or-`create` logic, which is what actually satisfies "moving a
 * tab must not stop a run or close a terminal session", not anything
 * this hook does.
 */
export function useTaskTabs() {
  const [tabsByTask, setTabsByTaskState] = useState<Map<number, TaskLayout>>(() => readPersisted());

  const setTabsByTask = useCallback(
    (updater: (prev: Map<number, TaskLayout>) => Map<number, TaskLayout>): void => {
      setTabsByTaskState((prev) => {
        const updated = updater(prev);
        if (updated === prev) return prev;
        const next = evictOverflow(updated);
        writePersisted(next);
        return next;
      });
    },
    [],
  );

  /** Seeds taskId's default tab set if it doesn't have one yet (a no-op otherwise). */
  const ensureTask = useCallback(
    (taskId: number): void => {
      setTabsByTask((prev) => {
        if (prev.has(taskId)) return prev;
        const next = new Map(prev);
        next.set(taskId, seedState(taskId));
        return next;
      });
    },
    [setTabsByTask],
  );

  /**
   * Opens entry in taskId's layout. If its key is already open (in any
   * pane), this only focuses/activates it there -- `placement` never
   * yanks a tab the user (or a previous call) already placed. For a
   * genuinely new tab, `placement` decides where: `"primary"` always
   * lands in the default pane; `"side"`/`"prefer"` land in an existing
   * non-default pane if one exists, else `"prefer"` falls back to the
   * default pane (never creates a split implicitly) while `"side"`
   * explicitly creates one.
   */
  const openTab = useCallback(
    (taskId: number, entry: TabEntry, placement: TabPlacement = "primary"): void => {
      setTabsByTask((prev) => {
        // The seed fallback only applies when the task has *no* entry at
        // all (never selected). A task whose tabs were all closed has an
        // entry with an empty default pane, which rehydrates as empty --
        // reopening from the empty state must not resurrect the whole
        // seed set around the reopened tab.
        const layout = prev.get(taskId) ?? seedState(taskId);

        if (findPaneContainingTab(layout.root, entry.key)) {
          const focused = focusTabInLayout({ layout, tabKey: entry.key });
          if (!focused) return prev;
          const next = new Map(prev);
          next.set(taskId, focused);
          return next;
        }

        let targetLayout = layout;
        let targetPaneId = DEFAULT_PANE_ID;

        if (typeof placement === "object") {
          targetPaneId = placement.pane;
        } else if (placement === "side" || placement === "prefer") {
          const existingOtherPane = collectAllPanes(layout.root).find((pane) => pane.id !== DEFAULT_PANE_ID);
          if (existingOtherPane) {
            targetPaneId = existingOtherPane.id;
          } else if (placement === "side") {
            const split = splitPaneEmptyInLayout({
              layout,
              targetPaneId: DEFAULT_PANE_ID,
              position: "right",
              createNodeId,
              maxTreeDepth: MAX_TREE_DEPTH,
            });
            if (split) {
              targetLayout = split.layout;
              targetPaneId = split.paneId;
            }
          }
        }

        const nextRoot = updatePaneInTree(targetLayout.root, {
          paneId: targetPaneId,
          updater: (pane) => ({ ...pane, tabs: [...pane.tabs, entry], activeKey: entry.key }),
        });
        const next = new Map(prev);
        next.set(taskId, { root: nextRoot, focusedPaneId: targetPaneId });
        return next;
      });
    },
    [setTabsByTask],
  );

  /**
   * Removes the tab with key from wherever it is, collapsing its pane
   * into a sibling once empty -- unless it's the tree's sole pane, which
   * is left in place with zero tabs.
   */
  const closeTab = useCallback(
    (taskId: number, key: string): void => {
      setTabsByTask((prev) => {
        const layout = prev.get(taskId);
        if (!layout) return prev;
        const pane = findPaneContainingTab(layout.root, key);
        if (!pane) return prev;
        const isSolePane = collectAllPanes(layout.root).length === 1;
        const { root } = detachTabFromTree(layout.root, {
          tabKey: key,
          preserveEmptyPaneId: isSolePane ? pane.id : null,
        });
        const focusedPaneId = findPaneById(root, layout.focusedPaneId)
          ? layout.focusedPaneId
          : (collectAllPanes(root)[0]?.id ?? null);
        const next = new Map(prev);
        next.set(taskId, { root, focusedPaneId });
        return next;
      });
    },
    [setTabsByTask],
  );

  /** Makes key the active tab of whichever pane it's open in (a no-op if it isn't open anywhere). */
  const activate = useCallback(
    (taskId: number, key: string): void => {
      setTabsByTask((prev) => {
        const layout = prev.get(taskId);
        if (!layout) return prev;
        const focused = focusTabInLayout({ layout, tabKey: key });
        if (!focused) return prev;
        const next = new Map(prev);
        next.set(taskId, focused);
        return next;
      });
    },
    [setTabsByTask],
  );

  /**
   * Explicitly relocates an already-open tab to `target` -- any live pane
   * id, not just the old primary/side pair. A no-op if the tab is already
   * there, or isn't open anywhere (nothing to move).
   */
  const moveTab = useCallback(
    (taskId: number, key: string, target: PaneId): void => {
      setTabsByTask((prev) => {
        const layout = prev.get(taskId);
        if (!layout) return prev;
        const moved = moveTabToPaneInLayout({ layout, tabKey: key, toPaneId: target });
        if (!moved) return prev;
        const next = new Map(prev);
        next.set(taskId, moved);
        return next;
      });
    },
    [setTabsByTask],
  );

  /** Splits targetPaneId in `direction`, carrying `key`'s tab into the freshly created pane. A no-op past `MAX_TREE_DEPTH`, or if the tab/pane can't be found. */
  const splitTab = useCallback(
    (taskId: number, key: string, targetPaneId: string, direction: SplitDirection): void => {
      setTabsByTask((prev) => {
        const layout = prev.get(taskId);
        if (!layout) return prev;
        const result = splitPaneInLayout({
          layout,
          tabKey: key,
          targetPaneId,
          position: SPLIT_DIRECTION_TO_POSITION[direction],
          createNodeId,
          maxTreeDepth: MAX_TREE_DEPTH,
        });
        if (!result) return prev; // depth cap hit, or tab/pane not found -- silent no-op, per the plan's Item 3 Acceptance Criteria
        const next = new Map(prev);
        next.set(taskId, result.layout);
        return next;
      });
    },
    [setTabsByTask],
  );

  /** Persists a resize handle drag's new sizes for the group with `groupId`. */
  const resizeGroup = useCallback(
    (taskId: number, groupId: string, sizes: number[]): void => {
      setTabsByTask((prev) => {
        const layout = prev.get(taskId);
        if (!layout) return prev;
        const next = new Map(prev);
        next.set(taskId, resizeSplitInLayout({ layout, groupId, sizes }));
        return next;
      });
    },
    [setTabsByTask],
  );

  return { tabsByTask, ensureTask, openTab, closeTab, activate, moveTab, splitTab, resizeGroup };
}

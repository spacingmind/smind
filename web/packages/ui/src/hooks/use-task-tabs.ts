import { useCallback, useState } from "react";

import { defaultTabsForTask, type TabEntry, type TabKind } from "@/components/tab-registry";
import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";

/** The main content area's two positions (Item 6's "one split"). */
export type PaneId = "primary" | "side";

/** One pane's tabs, in strip order, plus which one is active (null once every tab in it is closed). */
export interface PaneState {
  tabs: TabEntry[];
  activeKey: string | null;
}

/**
 * One task's whole tab layout: an always-present primary pane and an
 * optional side pane. `side` is `null` exactly when there is no split --
 * its presence *is* the split, rather than a separate boolean the two
 * could disagree about.
 */
export interface TaskTabsState {
  primary: PaneState;
  side: PaneState | null;
}

/** Where an opened-or-moved tab is allowed to land. `prefer` is the implicit-open case (`audit-paseo.md` §1): use the side pane if one exists, else primary -- but never move a tab already placed somewhere. */
export type TabPlacement = "primary" | "side" | "prefer";

/** Kinds "Open to side" and cross-pane moves apply to -- Chat and Files stay pinned to primary (the plan's Item 6 explicitly excludes them). */
const MOVABLE_KINDS: readonly TabKind[] = ["file", "diff", "terminal"];

export function isMovableKind(kind: TabKind): boolean {
  return (MOVABLE_KINDS as readonly string[]).includes(kind);
}

function seedState(taskId: number): TaskTabsState {
  // First visit seeds the default set (web-ui-dogfood-polish Item 3);
  // afterwards the strip is the user's -- a persisted empty primary pane
  // (every seeded tab closed) rehydrates as empty, not re-seeded, which
  // is why ensureTask keys off the map rather than off pane emptiness.
  const tabs = defaultTabsForTask(taskId);
  return { primary: { tabs, activeKey: tabs[0]!.key }, side: null };
}

/** The pane holding `key`, or null if it's open nowhere. */
function paneOf(state: TaskTabsState, key: string): PaneId | null {
  if (state.primary.tabs.some((t) => t.key === key)) return "primary";
  if (state.side?.tabs.some((t) => t.key === key)) return "side";
  return null;
}

function getPane(state: TaskTabsState, pane: PaneId): PaneState {
  return pane === "primary" ? state.primary : (state.side ?? { tabs: [], activeKey: null });
}

/** Returns a new TaskTabsState with `pane` replaced by `next` -- an empty `side` (no tabs left) collapses back to `null`, which is what "closing the last side tab removes the pane" means. */
function withPane(state: TaskTabsState, pane: PaneId, next: PaneState): TaskTabsState {
  if (pane === "primary") return { ...state, primary: next };
  return { ...state, side: next.tabs.length === 0 ? null : next };
}

// --- Persistence (Item 3: "today every open file tab is lost"; Item 6
// extends the same mechanism to which pane each tab is in) -------------
//
// The map is JSON-object-keyed by task id (JSON has no numeric object
// keys) and round-tripped through `lib/storage.ts`, so a reload restores
// every task's open tabs, which pane each is in, and which was active in
// each -- not just what the sidebar shows.

const TAB_KINDS: readonly TabKind[] = ["task", "files", "file", "diff", "terminal"];

/**
 * Tasks kept in storage at once. Eviction is oldest-inserted-first, not a
 * true LRU (that would need every read to reorder, not only every write) --
 * an approximation good enough for "don't grow without bound" without the
 * bookkeeping a precise one would need.
 */
const MAX_PERSISTED_TASKS = 50;

function isTabEntry(value: unknown): value is TabEntry {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.kind === "string" &&
    (TAB_KINDS as readonly string[]).includes(v.kind) &&
    typeof v.key === "string" &&
    typeof v.taskId === "number" &&
    typeof v.title === "string" &&
    (v.closable === undefined || typeof v.closable === "boolean")
  );
}

function isPaneState(value: unknown): value is PaneState {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  if (!Array.isArray(v.tabs) || !v.tabs.every(isTabEntry)) return false;
  if (v.activeKey !== null && typeof v.activeKey !== "string") return false;
  return true;
}

function isTaskTabsState(value: unknown): value is TaskTabsState {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  if (!isPaneState(v.primary)) return false;
  if (v.side !== null && !isPaneState(v.side)) return false;
  return true;
}

function isPersistedMap(value: unknown): value is Record<string, TaskTabsState> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  return Object.values(value).every(isTaskTabsState);
}

/** Every tab across both panes, for the taskId/activeKey consistency checks below. */
function allTabs(state: TaskTabsState): TabEntry[] {
  return [...state.primary.tabs, ...(state.side?.tabs ?? [])];
}

/**
 * The persisted map, dropping any task whose entry doesn't hang together
 * (a tab claiming a different taskId than its own key, an activeKey
 * naming a tab that isn't in its own pane, or the same key open in both
 * panes at once) -- storage a stale build or a hand-edit left
 * inconsistent degrades that one task to "reseed on next visit" rather
 * than rendering a broken layout.
 */
function readPersisted(): Map<number, TaskTabsState> {
  const raw = readStored(STORAGE_KEYS.taskTabs, {}, isPersistedMap);
  const map = new Map<number, TaskTabsState>();
  for (const [key, state] of Object.entries(raw)) {
    const taskId = Number(key);
    if (!Number.isFinite(taskId)) continue;
    if (allTabs(state).some((t) => t.taskId !== taskId)) continue;
    if (state.primary.activeKey !== null && !state.primary.tabs.some((t) => t.key === state.primary.activeKey)) {
      continue;
    }
    if (state.side) {
      if (state.side.activeKey !== null && !state.side.tabs.some((t) => t.key === state.side!.activeKey)) {
        continue;
      }
      const primaryKeys = new Set(state.primary.tabs.map((t) => t.key));
      if (state.side.tabs.some((t) => primaryKeys.has(t.key))) continue;
    }
    map.set(taskId, state);
  }
  return map;
}

function writePersisted(map: Map<number, TaskTabsState>): void {
  const obj: Record<string, TaskTabsState> = {};
  for (const [taskId, state] of map) obj[String(taskId)] = state;
  writeStored(STORAGE_KEYS.taskTabs, obj);
}

/** Drops the oldest-inserted entries once the map exceeds the cap. */
function evictOverflow(map: Map<number, TaskTabsState>): Map<number, TaskTabsState> {
  if (map.size <= MAX_PERSISTED_TASKS) return map;
  const next = new Map(map);
  const overflow = next.size - MAX_PERSISTED_TASKS;
  const oldest = [...next.keys()].slice(0, overflow);
  for (const taskId of oldest) next.delete(taskId);
  return next;
}

/**
 * Per-task tab state for App.tsx's registry-driven tab strip (ADR 0004)
 * plus the side dock (Item 6): a Map keyed by task id, persisted to
 * `localStorage` (Item 3). Every setter is an updater over the previous
 * Map and returns `prev` unchanged when the requested change is already
 * in effect, so redundant calls don't cause extra renders or extra
 * writes.
 *
 * Moving a tab between panes is a plain data move -- remove the entry
 * from one pane's array, append it to the other's -- not a remount
 * dance. The pane component underneath (TerminalPane, FileEditorPane,
 * ...) *does* fully unmount from one `<Tabs>` root and mount fresh in
 * the other, since primary and side are separate Radix Tabs trees; that
 * is exactly the same "switching tabs unmounts inactive content"
 * contract every pane already tolerates (ADR 0004), and every pane's own
 * detach-not-stop mount effect (attach-if-exists, else create) already
 * makes that safe -- see `terminal-pane.tsx`'s `terminal.list` ->
 * `attach`-or-`create` logic, which is what actually satisfies "moving a
 * tab must not stop a run or close a terminal session", not anything
 * this hook does.
 */
export function useTaskTabs() {
  const [tabsByTask, setTabsByTaskState] = useState<Map<number, TaskTabsState>>(() => readPersisted());

  const setTabsByTask = useCallback(
    (updater: (prev: Map<number, TaskTabsState>) => Map<number, TaskTabsState>): void => {
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
   * Opens entry in taskId's layout. If its key is already open (in either
   * pane), this only activates it there -- `placement` never yanks a tab
   * the user (or a previous call) already placed, which is the plan's
   * "never yank a tab the user placed" rule. For a genuinely new tab,
   * `placement` decides where: `"primary"` or `"side"` explicitly, or
   * `"prefer"` for an implicit open (a file-tree click) -- the side pane
   * if one exists, else primary.
   */
  const openTab = useCallback(
    (taskId: number, entry: TabEntry, placement: TabPlacement = "primary"): void => {
      setTabsByTask((prev) => {
        // The seed fallback only applies when the task has *no* entry at
        // all (never selected). A task whose tabs were all closed has an
        // entry with an empty primary pane, which rehydrates as empty --
        // reopening from the empty state must not resurrect the whole
        // seed set around the reopened tab.
        const state = prev.get(taskId) ?? seedState(taskId);
        const existingPane = paneOf(state, entry.key);
        if (existingPane) {
          const pane = getPane(state, existingPane);
          if (pane.activeKey === entry.key) return prev;
          const next = new Map(prev);
          next.set(taskId, withPane(state, existingPane, { ...pane, activeKey: entry.key }));
          return next;
        }

        const target: PaneId =
          placement === "side" ? "side" : placement === "prefer" ? (state.side ? "side" : "primary") : "primary";
        const pane = getPane(state, target);
        const next = new Map(prev);
        next.set(taskId, withPane(state, target, { tabs: [...pane.tabs, entry], activeKey: entry.key }));
        return next;
      });
    },
    [setTabsByTask],
  );

  /**
   * Removes the tab with key from wherever it is. If it was that pane's
   * active tab, the neighbor to its right becomes active (the one that
   * slid into its place), else the one to its left; closing a pane's
   * last tab leaves its activeKey null, and for the side pane, removes
   * the pane entirely (Item 6: "closing the last tab in the side pane
   * removes it").
   */
  const closeTab = useCallback(
    (taskId: number, key: string): void => {
      setTabsByTask((prev) => {
        const state = prev.get(taskId);
        if (!state) return prev;
        const paneId = paneOf(state, key);
        if (!paneId) return prev;

        const pane = getPane(state, paneId);
        const index = pane.tabs.findIndex((t) => t.key === key);
        const tabs = pane.tabs.filter((t) => t.key !== key);
        const activeKey =
          pane.activeKey === key
            ? tabs.length > 0
              ? (tabs[index] ?? tabs[index - 1]!)!.key
              : null
            : pane.activeKey;

        const next = new Map(prev);
        next.set(taskId, withPane(state, paneId, { tabs, activeKey }));
        return next;
      });
    },
    [setTabsByTask],
  );

  /** Makes key the active tab of whichever pane it's open in (a no-op if it isn't open anywhere). */
  const activate = useCallback(
    (taskId: number, key: string): void => {
      setTabsByTask((prev) => {
        const state = prev.get(taskId);
        if (!state) return prev;
        const paneId = paneOf(state, key);
        if (!paneId) return prev;
        const pane = getPane(state, paneId);
        if (pane.activeKey === key) return prev;
        const next = new Map(prev);
        next.set(taskId, withPane(state, paneId, { ...pane, activeKey: key }));
        return next;
      });
    },
    [setTabsByTask],
  );

  /**
   * Explicitly relocates an already-open tab to `target` -- "Open to
   * side" and its inverse. A no-op if the tab is already there, or isn't
   * open anywhere (nothing to move). Moving into `"side"` creates the
   * side pane if it didn't exist; moving `"side"`'s last tab elsewhere
   * removes the pane, same as {@link closeTab}.
   */
  const moveTab = useCallback(
    (taskId: number, key: string, target: PaneId): void => {
      setTabsByTask((prev) => {
        const state = prev.get(taskId);
        if (!state) return prev;
        const sourceId = paneOf(state, key);
        if (!sourceId || sourceId === target) return prev;

        const source = getPane(state, sourceId);
        const entry = source.tabs.find((t) => t.key === key)!;
        const sourceIndex = source.tabs.findIndex((t) => t.key === key);
        const sourceTabs = source.tabs.filter((t) => t.key !== key);
        const sourceActiveKey =
          source.activeKey === key
            ? sourceTabs.length > 0
              ? (sourceTabs[sourceIndex] ?? sourceTabs[sourceIndex - 1]!)!.key
              : null
            : source.activeKey;

        const destination = getPane(state, target);
        const withoutSource = withPane(state, sourceId, { tabs: sourceTabs, activeKey: sourceActiveKey });
        const moved = withPane(withoutSource, target, {
          tabs: [...destination.tabs, entry],
          activeKey: entry.key,
        });

        const next = new Map(prev);
        next.set(taskId, moved);
        return next;
      });
    },
    [setTabsByTask],
  );

  return { tabsByTask, ensureTask, openTab, closeTab, activate, moveTab };
}

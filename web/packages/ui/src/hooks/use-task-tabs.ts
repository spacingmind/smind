import { useCallback, useState } from "react";

import { defaultTabsForTask, type TabEntry, type TabKind } from "@/components/tab-registry";
import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";

/** One task's tab set: its open tabs in strip order plus which one is active (null once every tab is closed). */
export interface TaskTabsState {
  tabs: TabEntry[];
  activeKey: string | null;
}

function seedState(taskId: number): TaskTabsState {
  const tabs = defaultTabsForTask(taskId);
  return { tabs, activeKey: tabs[0]!.key };
}

// --- Persistence (Item 3: "today every open file tab is lost") ---------
//
// The map is JSON-object-keyed by task id (JSON has no numeric object
// keys) and round-tripped through `lib/storage.ts`, so a reload restores
// every task's open tabs and active one, not just the sidebar-visible
// state. A task never revisited after a browser restart still has a
// stale entry sitting in storage; capped below rather than left to grow
// forever.

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

function isTaskTabsState(value: unknown): value is TaskTabsState {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  if (!Array.isArray(v.tabs) || !v.tabs.every(isTabEntry)) return false;
  if (v.activeKey !== null && typeof v.activeKey !== "string") return false;
  return true;
}

function isPersistedMap(value: unknown): value is Record<string, TaskTabsState> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  return Object.values(value).every(isTaskTabsState);
}

/**
 * The persisted map, dropping any task whose entry doesn't hang together
 * (a tab claiming a different taskId than its own key, or an activeKey
 * naming a tab that isn't in the list) -- storage a stale build or a
 * hand-edit left inconsistent degrades that one task to "reseed on next
 * visit" rather than rendering a broken strip.
 */
function readPersisted(): Map<number, TaskTabsState> {
  const raw = readStored(STORAGE_KEYS.taskTabs, {}, isPersistedMap);
  const map = new Map<number, TaskTabsState>();
  for (const [key, state] of Object.entries(raw)) {
    const taskId = Number(key);
    if (!Number.isFinite(taskId)) continue;
    if (state.tabs.some((t) => t.taskId !== taskId)) continue;
    if (state.activeKey !== null && !state.tabs.some((t) => t.key === state.activeKey)) continue;
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
 * Per-task tab state for App.tsx's registry-driven tab strip (ADR 0004):
 * a Map keyed by task id, persisted to `localStorage` (Item 3) so a
 * reload restores every task's open tabs and which one was active, not
 * just what the sidebar shows. Every setter is an updater over the
 * previous Map and returns `prev` unchanged when the requested change is
 * already in effect, so redundant calls don't cause extra renders or
 * extra writes.
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

  /** Adds entry to taskId's strip if its key isn't open yet, and makes it active either way. */
  const openTab = useCallback(
    (taskId: number, entry: TabEntry): void => {
      setTabsByTask((prev) => {
        const state = prev.get(taskId) ?? seedState(taskId);
        const alreadyOpen = state.tabs.some((t) => t.key === entry.key);
        if (alreadyOpen && state.activeKey === entry.key) return prev;
        const next = new Map(prev);
        next.set(taskId, {
          tabs: alreadyOpen ? state.tabs : [...state.tabs, entry],
          activeKey: entry.key,
        });
        return next;
      });
    },
    [setTabsByTask],
  );

  /**
   * Removes the tab with key from taskId's strip. If it was the active
   * tab, the neighbor to its right becomes active (the one that slid into
   * its place), else the one to its left; closing the last tab leaves
   * activeKey null.
   */
  const closeTab = useCallback(
    (taskId: number, key: string): void => {
      setTabsByTask((prev) => {
        const state = prev.get(taskId);
        if (!state) return prev;
        const index = state.tabs.findIndex((t) => t.key === key);
        if (index === -1) return prev;

        const tabs = state.tabs.filter((t) => t.key !== key);
        const activeKey =
          state.activeKey === key
            ? tabs.length > 0
              ? (tabs[index] ?? tabs[index - 1]!)!.key
              : null
            : state.activeKey;

        const next = new Map(prev);
        next.set(taskId, { tabs, activeKey });
        return next;
      });
    },
    [setTabsByTask],
  );

  /** Makes key the active tab of taskId's strip (a no-op if it isn't open). */
  const activate = useCallback(
    (taskId: number, key: string): void => {
      setTabsByTask((prev) => {
        const state = prev.get(taskId);
        if (!state || state.activeKey === key || !state.tabs.some((t) => t.key === key)) return prev;
        const next = new Map(prev);
        next.set(taskId, { ...state, activeKey: key });
        return next;
      });
    },
    [setTabsByTask],
  );

  return { tabsByTask, ensureTask, openTab, closeTab, activate };
}

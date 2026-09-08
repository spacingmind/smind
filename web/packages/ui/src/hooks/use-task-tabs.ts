import { useCallback, useState } from "react";

import { defaultTabsForTask, type TabEntry } from "@/components/tab-registry";

/** One task's tab set: its open tabs in strip order plus which one is active (null once every tab is closed). */
export interface TaskTabsState {
  tabs: TabEntry[];
  activeKey: string | null;
}

function seedState(taskId: number): TaskTabsState {
  const tabs = defaultTabsForTask(taskId);
  return { tabs, activeKey: tabs[0]!.key };
}

/**
 * Per-task tab state for App.tsx's registry-driven tab strip (ADR 0004):
 * a Map keyed by task id, held in component state only -- no persistence
 * this pass, by the plan's explicit scoping. Every setter is an updater
 * over the previous Map and returns `prev` unchanged when the requested
 * change is already in effect, so redundant calls don't cause extra
 * renders.
 */
export function useTaskTabs() {
  const [tabsByTask, setTabsByTask] = useState<Map<number, TaskTabsState>>(new Map());

  /** Seeds taskId's default tab set if it doesn't have one yet (a no-op otherwise). */
  const ensureTask = useCallback((taskId: number): void => {
    setTabsByTask((prev) => {
      if (prev.has(taskId)) return prev;
      const next = new Map(prev);
      next.set(taskId, seedState(taskId));
      return next;
    });
  }, []);

  /** Adds entry to taskId's strip if its key isn't open yet, and makes it active either way. */
  const openTab = useCallback((taskId: number, entry: TabEntry): void => {
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
  }, []);

  /**
   * Removes the tab with key from taskId's strip. If it was the active
   * tab, the neighbor to its right becomes active (the one that slid into
   * its place), else the one to its left; closing the last tab leaves
   * activeKey null.
   */
  const closeTab = useCallback((taskId: number, key: string): void => {
    setTabsByTask((prev) => {
      const state = prev.get(taskId);
      if (!state) return prev;
      const index = state.tabs.findIndex((t) => t.key === key);
      if (index === -1) return prev;

      const tabs = state.tabs.filter((t) => t.key !== key);
      const activeKey =
        state.activeKey === key ? (tabs.length > 0 ? (tabs[index] ?? tabs[index - 1]!)!.key : null) : state.activeKey;

      const next = new Map(prev);
      next.set(taskId, { tabs, activeKey });
      return next;
    });
  }, []);

  /** Makes key the active tab of taskId's strip (a no-op if it isn't open). */
  const activate = useCallback((taskId: number, key: string): void => {
    setTabsByTask((prev) => {
      const state = prev.get(taskId);
      if (!state || state.activeKey === key || !state.tabs.some((t) => t.key === key)) return prev;
      const next = new Map(prev);
      next.set(taskId, { ...state, activeKey: key });
      return next;
    });
  }, []);

  return { tabsByTask, ensureTask, openTab, closeTab, activate };
}

import { useCallback, useEffect } from "react";

import { useCollapsedTasksStore } from "@/stores/collapsed-tasks-store";

export interface CollapsedTasks {
  collapsed: ReadonlySet<number>;
  toggle: (taskId: number) => void;
  expand: (ids: number[]) => void;
}

/**
 * The sidebar tree view's set of collapsed-parent task ids (PR-U1),
 * persisted in localStorage under `smind.sidebar.collapsedTasks`, from the
 * shared preference store so every reader sees collapses change live.
 *
 * Default is empty -- every parent starts expanded, so a freshly loaded
 * workspace shows its full hierarchy rather than an apparently empty one.
 *
 * `liveTaskIds` prunes a collapsed id once the task is no longer live
 * (archived/deleted), with the same `null`-means-not-loaded-yet contract
 * as usePinnedTasks/useUnreadTasks so the tree's initial empty render
 * can't wipe persisted state before the real fetch lands.
 */
export function useCollapsedTasks(liveTaskIds: ReadonlySet<number> | null): CollapsedTasks {
  const collapsed = useCollapsedTasksStore((s) => s.value);

  useEffect(() => {
    if (liveTaskIds === null) return;
    const { value: prev, set } = useCollapsedTasksStore.getState();
    let changed = false;
    const next = new Set<number>();
    for (const taskId of prev) {
      if (liveTaskIds.has(taskId)) next.add(taskId);
      else changed = true;
    }
    if (changed) set(next);
  }, [liveTaskIds]);

  const toggle = useCallback((taskId: number) => {
    const { value: prev, set } = useCollapsedTasksStore.getState();
    const next = new Set(prev);
    if (next.has(taskId)) next.delete(taskId);
    else next.add(taskId);
    set(next);
  }, []);

  // Batched expand (rather than N toggles) so revealing a selected
  // descendant re-renders its ancestors once.
  const expand = useCallback((ids: number[]) => {
    if (ids.length === 0) return;
    const { value: prev, set } = useCollapsedTasksStore.getState();
    const next = new Set(prev);
    let changed = false;
    for (const id of ids) {
      if (next.delete(id)) changed = true;
    }
    if (changed) set(next);
  }, []);

  return { collapsed, toggle, expand };
}

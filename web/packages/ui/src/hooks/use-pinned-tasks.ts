import { useCallback, useEffect } from "react";

import { usePinnedTasksStore } from "@/stores/pinned-tasks-store";

export interface PinnedTasks {
  pinned: ReadonlySet<number>;
  togglePin: (taskId: number) => void;
}

/**
 * The sidebar's pinned-task set (AC4), persisted in localStorage, from the
 * shared preference store so every reader sees pins change live.
 *
 * `liveTaskIds` prunes a pinned id once it's no longer in the live task
 * list (archived/deleted) -- same contract as useUnreadTasks' own
 * pruning: `null` means "the tree hasn't loaded yet" and is never pruned
 * against, so the tree's initial (empty) render can't wipe out a
 * persisted pin before the real fetch even lands.
 */
export function usePinnedTasks(liveTaskIds: ReadonlySet<number> | null): PinnedTasks {
  const pinned = usePinnedTasksStore((s) => s.value);

  useEffect(() => {
    if (liveTaskIds === null) return;
    const { value: prev, set } = usePinnedTasksStore.getState();
    let changed = false;
    const next = new Set<number>();
    for (const taskId of prev) {
      if (liveTaskIds.has(taskId)) next.add(taskId);
      else changed = true;
    }
    if (changed) set(next);
  }, [liveTaskIds]);

  const togglePin = useCallback((taskId: number) => {
    const { value: prev, set } = usePinnedTasksStore.getState();
    const next = new Set(prev);
    if (next.has(taskId)) next.delete(taskId);
    else next.add(taskId);
    set(next);
  }, []);

  return { pinned, togglePin };
}

import { useCallback, useEffect, useState } from "react";

import { readPinnedTasks, writePinnedTasks } from "@/lib/sidebar-preferences";

export interface PinnedTasks {
  pinned: ReadonlySet<number>;
  togglePin: (taskId: number) => void;
}

/**
 * The sidebar's pinned-task set (AC4), persisted in localStorage. A plain
 * per-mount `useState` (unlike the notification-permission/sound
 * preferences, which use a module-level store) is enough here: pins are
 * only ever read and written from within AppSidebar, which has exactly
 * one live instance, so there's no second mount that needs to observe a
 * change made in another one.
 *
 * `liveTaskIds` prunes a pinned id once it's no longer in the live task
 * list (archived/deleted) -- same contract as useUnreadTasks' own
 * pruning: `null` means "the tree hasn't loaded yet" and is never pruned
 * against, so the tree's initial (empty) render can't wipe out a
 * persisted pin before the real fetch even lands.
 */
export function usePinnedTasks(liveTaskIds: ReadonlySet<number> | null): PinnedTasks {
  const [pinned, setPinned] = useState<Set<number>>(() => readPinnedTasks());

  useEffect(() => {
    writePinnedTasks(pinned);
  }, [pinned]);

  useEffect(() => {
    if (liveTaskIds === null) return;
    setPinned((prev) => {
      let changed = false;
      const next = new Set<number>();
      for (const taskId of prev) {
        if (liveTaskIds.has(taskId)) next.add(taskId);
        else changed = true;
      }
      return changed ? next : prev;
    });
  }, [liveTaskIds]);

  const togglePin = useCallback((taskId: number) => {
    setPinned((prev) => {
      const next = new Set(prev);
      if (next.has(taskId)) next.delete(taskId);
      else next.add(taskId);
      return next;
    });
  }, []);

  return { pinned, togglePin };
}

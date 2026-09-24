import { useCallback, useState } from "react";

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
 */
export function usePinnedTasks(): PinnedTasks {
  const [pinned, setPinned] = useState<Set<number>>(() => readPinnedTasks());

  const togglePin = useCallback((taskId: number) => {
    setPinned((prev) => {
      const next = new Set(prev);
      if (next.has(taskId)) next.delete(taskId);
      else next.add(taskId);
      writePinnedTasks(next);
      return next;
    });
  }, []);

  return { pinned, togglePin };
}

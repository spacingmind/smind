import { useCallback, useEffect, useRef, useState } from "react";

import type { TaskAttention } from "@/hooks/use-task-attention";
import { readUnreadTasks, writeUnreadTasks } from "@/lib/sidebar-preferences";

export interface UnreadTasks {
  /** Task ids the user hasn't looked at since their last attention-worthy event (or a manual "Mark unread"). */
  unread: ReadonlySet<number>;
  /** Manual override from the task's context menu -- sets unread even without a live attention reason. */
  markUnread: (taskId: number) => void;
}

/**
 * Tracks which tasks are unread (web-sidebar-attention plan, AC2): a task
 * becomes unread when its attention set (useTaskAttention) gains a *new*
 * reason -- same edge-triggered `${taskId}:${reason}` bookkeeping as
 * useAttentionNotifications, so reloading with pre-existing attention state
 * never floods every such task back into "unread" -- and becomes read again
 * the moment it's opened (`selectedTaskId` naming it). "Mark unread" is a
 * manual override that ignores attention entirely.
 *
 * Persisted in localStorage (plan's Decisions: "all client-side state"), so
 * unread survives a reload; the initial read is the only place a fresh
 * mount's pre-existing attention could otherwise flood the set, and the
 * edge-trigger above already avoids that.
 */
export function useUnreadTasks(attention: TaskAttention, selectedTaskId: number | null): UnreadTasks {
  const [unread, setUnread] = useState<Set<number>>(() => readUnreadTasks());
  const seenKeysRef = useRef<Set<string>>(new Set());
  const initializedRef = useRef(false);
  const selectedTaskIdRef = useRef(selectedTaskId);
  selectedTaskIdRef.current = selectedTaskId;

  useEffect(() => {
    writeUnreadTasks(unread);
  }, [unread]);

  useEffect(() => {
    const currentKeys = new Set<string>();
    const gained = new Set<number>();
    for (const [taskId, reasons] of attention) {
      for (const reason of reasons) {
        const key = `${taskId}:${reason}`;
        currentKeys.add(key);
        if (!seenKeysRef.current.has(key)) gained.add(taskId);
      }
    }

    if (initializedRef.current && gained.size > 0) {
      setUnread((prev) => {
        let changed = false;
        const next = new Set(prev);
        for (const taskId of gained) {
          // A gain for the task currently open is something the user is
          // already looking at -- it never needed the unread marker.
          if (taskId === selectedTaskIdRef.current || next.has(taskId)) continue;
          next.add(taskId);
          changed = true;
        }
        return changed ? next : prev;
      });
    }
    initializedRef.current = true;
    seenKeysRef.current = currentKeys;
  }, [attention]);

  useEffect(() => {
    if (selectedTaskId === null) return;
    setUnread((prev) => {
      if (!prev.has(selectedTaskId)) return prev;
      const next = new Set(prev);
      next.delete(selectedTaskId);
      return next;
    });
  }, [selectedTaskId]);

  const markUnread = useCallback((taskId: number) => {
    setUnread((prev) => {
      if (prev.has(taskId)) return prev;
      return new Set(prev).add(taskId);
    });
  }, []);

  return { unread, markUnread };
}

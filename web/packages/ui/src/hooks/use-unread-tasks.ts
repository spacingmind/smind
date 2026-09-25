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
 * The currently-selected task is normally exempt from a new gain (the user
 * is already looking at it) -- but only while the document is visible.
 * With the tab backgrounded or another window focused, "selected" just
 * means "was open when the user left"; a gain there is exactly as unseen
 * as any other task's, so it still marks unread. Bringing the document
 * back to visible then clears it for whatever task is still selected,
 * the same "looking at it now" signal a fresh selection gives.
 *
 * Persisted in localStorage (plan's Decisions: "all client-side state"), so
 * unread survives a reload; the initial read is the only place a fresh
 * mount's pre-existing attention could otherwise flood the set, and the
 * edge-trigger above already avoids that.
 *
 * `liveTaskIds` prunes stale entries (an archived/deleted task, or ids left
 * over from a different daemon/SMIND_HOME) once the caller actually knows
 * the live set -- `null` means "not loaded yet" and is never pruned
 * against, since an empty initial task list is indistinguishable from "not
 * fetched yet" and must not wipe a persisted set out from under a page
 * that just hasn't finished its first load.
 */
export function useUnreadTasks(
  attention: TaskAttention,
  selectedTaskId: number | null,
  liveTaskIds: ReadonlySet<number> | null,
): UnreadTasks {
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
      const documentHidden = typeof document !== "undefined" && document.hidden;
      setUnread((prev) => {
        let changed = false;
        const next = new Set(prev);
        for (const taskId of gained) {
          // A gain for the task currently open is something the user is
          // already looking at -- but only if they're actually looking at
          // the document right now (see this hook's doc comment).
          const isOpenAndVisible = taskId === selectedTaskIdRef.current && !documentHidden;
          if (isOpenAndVisible || next.has(taskId)) continue;
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

  useEffect(() => {
    if (typeof document === "undefined") return;
    function onVisibilityChange() {
      if (document.hidden || selectedTaskIdRef.current === null) return;
      const taskId = selectedTaskIdRef.current;
      setUnread((prev) => {
        if (!prev.has(taskId)) return prev;
        const next = new Set(prev);
        next.delete(taskId);
        return next;
      });
    }
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => document.removeEventListener("visibilitychange", onVisibilityChange);
  }, []);

  useEffect(() => {
    if (liveTaskIds === null) return;
    setUnread((prev) => {
      let changed = false;
      const next = new Set<number>();
      for (const taskId of prev) {
        if (liveTaskIds.has(taskId)) next.add(taskId);
        else changed = true;
      }
      return changed ? next : prev;
    });
  }, [liveTaskIds]);

  const markUnread = useCallback((taskId: number) => {
    setUnread((prev) => {
      if (prev.has(taskId)) return prev;
      return new Set(prev).add(taskId);
    });
  }, []);

  return { unread, markUnread };
}

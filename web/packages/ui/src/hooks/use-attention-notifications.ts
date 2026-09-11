import { useEffect, useRef } from "react";

import type { AttentionReason, TaskAttention } from "@/hooks/use-task-attention";
import type { NotificationPermissionState } from "@/hooks/use-notification-permission";

/** Just enough of a Task to label a notification -- callers pass whatever task list they already have (e.g. AppSidebar's workspace tree). */
export interface NotifiableTask {
  ID: number;
  Title: string;
}

const REASON_BODY: Record<AttentionReason, string> = {
  error: "A run finished with an error.",
  finished: "A run finished.",
  permission: "A run is waiting on a permission decision.",
};

/**
 * Fires a browser Notification when a task's attention set gains a *new*
 * reason (see useTaskAttention) while the tab is backgrounded
 * (document.hidden) -- the out-of-tab counterpart to the sidebar's own
 * attention dot, which only helps if you're looking at the tab at all.
 *
 * Edge-triggered, not level-triggered: a (task, reason) pair notifies once
 * when it's first added, never again while it stays present, and is
 * eligible to notify again only after it's cleared and later re-added
 * (e.g. a task errors, gets looked at, then errors again later). The very
 * first render establishes a baseline snapshot instead of notifying --
 * otherwise reloading the page with pre-existing attention state (e.g. a
 * task that already needed attention before this tab even opened) would
 * fire a burst of "new" notifications for state that isn't new at all.
 *
 * Never fires unless permission is exactly "granted" (never prompts for
 * it itself -- see useNotificationPermission) and never throws: browsers
 * can refuse to construct a Notification for reasons outside this code's
 * control (permission revoked mid-session, platform quirks), and a
 * best-effort notification is never worth crashing the app over.
 */
export function useAttentionNotifications(
  attention: TaskAttention,
  tasks: NotifiableTask[],
  permission: NotificationPermissionState,
): void {
  const notifiedRef = useRef<Set<string>>(new Set());
  const initializedRef = useRef(false);
  const tasksRef = useRef(tasks);
  tasksRef.current = tasks;

  useEffect(() => {
    const currentKeys = new Set<string>();
    const newlyAdded: { taskId: number; reason: AttentionReason }[] = [];

    for (const [taskId, reasons] of attention) {
      for (const reason of reasons) {
        const key = `${taskId}:${reason}`;
        currentKeys.add(key);
        if (!notifiedRef.current.has(key)) newlyAdded.push({ taskId, reason });
      }
    }

    if (initializedRef.current) {
      for (const { taskId, reason } of newlyAdded) {
        notifyIfEligible(taskId, reason, tasksRef.current, permission);
      }
    }
    initializedRef.current = true;
    notifiedRef.current = currentKeys;
  }, [attention, permission]);
}

function notifyIfEligible(
  taskId: number,
  reason: AttentionReason,
  tasks: NotifiableTask[],
  permission: NotificationPermissionState,
): void {
  if (permission !== "granted") return;
  if (typeof document === "undefined" || !document.hidden) return;
  if (typeof window === "undefined" || !("Notification" in window)) return;

  const title = tasks.find((t) => t.ID === taskId)?.Title ?? `Task ${taskId}`;
  try {
    new Notification(title, { body: REASON_BODY[reason] });
  } catch {
    // Degrade silently -- a best-effort notification is never worth
    // throwing into the render/effect over.
  }
}

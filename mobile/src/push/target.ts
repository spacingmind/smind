import type { RouteGate } from '../navigation/gate';
import { automationHref, taskHref } from '../navigation/routes';

/** A tapped notification opens a task, or (an automation auto-pause with no task) an automation. */
export type NotificationTarget = { taskId: number; chatId?: number } | { automationId: number };

/** The task a target is about, or null for an automation target. */
export function targetTaskId(t: NotificationTarget | null): number | null {
  return t !== null && 'taskId' in t ? t.taskId : null;
}

function toId(v: unknown): number | null {
  let n: number;
  if (typeof v === 'number') n = v;
  else if (typeof v === 'string' && /^\d+$/.test(v)) n = Number(v);
  else return null;
  return Number.isSafeInteger(n) && n > 0 ? n : null;
}

/**
 * The `data` of a tapped notification is untrusted: only a positive safe-integer taskId makes a task target, and
 * only without one does a positive safe-integer automationId make an automation target.
 */
export function resolveNotificationTarget(data: unknown): NotificationTarget | null {
  if (typeof data !== 'object' || data === null || Array.isArray(data)) return null;
  const d = data as Record<string, unknown>;
  const taskId = toId(d.taskId);
  if (taskId === null) {
    const automationId = toId(d.automationId);
    return automationId === null ? null : { automationId };
  }
  const chatId = toId(d.chatId);
  return chatId === null ? { taskId } : { taskId, chatId };
}

/** The header shows "Task <id>" until the screen loads (empty title, see parseTaskParams). */
export function targetHref(t: { taskId: number; chatId?: number }): ReturnType<typeof taskHref>;
export function targetHref(t: { automationId: number }): ReturnType<typeof automationHref>;
export function targetHref(t: NotificationTarget): ReturnType<typeof taskHref> | ReturnType<typeof automationHref>;
export function targetHref(t: NotificationTarget): ReturnType<typeof taskHref> | ReturnType<typeof automationHref> {
  if ('automationId' in t) return automationHref(t.automationId);
  return taskHref({ ID: t.taskId, Title: '' });
}

/** Holds the tapped target until the app can show it (a cold-start tap precedes the connection). */
export class PendingTarget {
  private target: NotificationTarget | null = null;

  set(t: NotificationTarget | null): void {
    this.target = t;
  }

  peek(): NotificationTarget | null {
    return this.target;
  }

  take(): NotificationTarget | null {
    const t = this.target;
    this.target = null;
    return t;
  }
}

/** Returns the target to navigate to exactly once when the gate is 'app'; otherwise it stays pending. */
export function releaseTarget(gate: RouteGate, pending: PendingTarget): NotificationTarget | null {
  return gate === 'app' ? pending.take() : null;
}

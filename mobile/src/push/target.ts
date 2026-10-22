import type { RouteGate } from '../navigation/gate';
import { taskHref } from '../navigation/routes';

export interface NotificationTarget {
  taskId: number;
  chatId?: number;
}

function toId(v: unknown): number | null {
  let n: number;
  if (typeof v === 'number') n = v;
  else if (typeof v === 'string' && /^\d+$/.test(v)) n = Number(v);
  else return null;
  return Number.isSafeInteger(n) && n > 0 ? n : null;
}

/** The `data` of a tapped notification is untrusted: only a positive safe-integer taskId makes a target. */
export function resolveNotificationTarget(data: unknown): NotificationTarget | null {
  if (typeof data !== 'object' || data === null || Array.isArray(data)) return null;
  const d = data as Record<string, unknown>;
  const taskId = toId(d.taskId);
  if (taskId === null) return null;
  const chatId = toId(d.chatId);
  return chatId === null ? { taskId } : { taskId, chatId };
}

/** The header shows "Task <id>" until the screen loads (empty title, see parseTaskParams). */
export function targetHref(t: NotificationTarget): ReturnType<typeof taskHref> {
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

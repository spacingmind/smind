// taskAttention.ts is Item 2's "needs approval" tracking + sort logic,
// kept out of TasksScreen.tsx so both are unit-testable without
// rendering (the same discipline as permissionRequests.ts's
// PermissionBoard). The pending-approval set is deliberately
// session-scoped, not persisted ground truth: internal/wsapi/events.go's
// permission.pending bus topic has no matching "resolved" counterpart a
// list screen can subscribe to (see the mobile-ui-polish plan's
// Decisions), so PendingApprovalSet resets to empty on every reset()
// (TasksScreen calls this at the start of every load()) and only accrues
// taskIds seen live since that reset -- an honest "something new
// happened since you last checked," never an omniscient "these tasks are
// still waiting."

import { RelayConnection, RelayEvent } from './relay/RelayConnection';
import { Task } from './api';

export const PERMISSION_PENDING_TOPIC = 'permission.pending';

/** permission.pending's wire payload (internal/wsapi/events.go). */
export interface PermissionPendingPayload {
  runId: string;
  taskId: number;
  requestId: string;
  summary: string;
  options: unknown[];
}

/** Which taskIds have fired permission.pending live since the last reset(). */
export class PendingApprovalSet {
  private taskIds = new Set<number>();

  /** The current set, read-only -- callers never mutate it directly. */
  get ids(): ReadonlySet<number> {
    return this.taskIds;
  }

  has(taskId: number): boolean {
    return this.taskIds.has(taskId);
  }

  markPending(taskId: number): void {
    this.taskIds.add(taskId);
  }

  /** Called at the start of every load() (pull-to-refresh or first mount): the set is a claim about "since I last checked," not ground truth. */
  reset(): void {
    this.taskIds.clear();
  }
}

/**
 * Subscribes to permission.pending for the connection's lifetime.
 * onTaskPending fires with the event's taskId; the caller (PendingApprovalSet.markPending, then a re-render) decides what "new" means. Returns the unsubscribe function.
 */
export function subscribeToPendingApprovals(conn: RelayConnection, onTaskPending: (taskId: number) => void): () => void {
  return conn.subscribe([PERMISSION_PENDING_TOPIC], (event: RelayEvent) => {
    onTaskPending((event.payload as PermissionPendingPayload).taskId);
  });
}

/**
 * Sorts tasks by attention (Item 2's Acceptance Criteria): a live
 * permission.pending hit sorts first, then Status === "running", then
 * everything else. Ties preserve original order (stable sort) -- no new
 * backend sort needed.
 */
export function sortTasksByAttention(tasks: Task[], pendingTaskIds: ReadonlySet<number>): Task[] {
  function rank(task: Task): number {
    if (pendingTaskIds.has(task.ID)) return 0;
    if (task.Status === 'running') return 1;
    return 2;
  }
  return tasks
    .map((task, index) => ({ task, index, rank: rank(task) }))
    .sort((a, b) => (a.rank !== b.rank ? a.rank - b.rank : a.index - b.index))
    .map((entry) => entry.task);
}

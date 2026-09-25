import type { AttentionReason, TaskAttention, TaskRunStatus } from "@/hooks/use-task-attention";
import type { Task } from "@/lib/types";

/** The group-by-status view's five buckets (AC6, named directly by the plan's Decisions). */
export type StatusGroupKey = "needs-attention" | "error" | "running" | "done" | "idle";

/** Display order: most urgent first, same "what costs the user most to miss" ordering lib/sidebar-signal.ts's REASON_PRECEDENCE already uses for a single row's dot. */
export const STATUS_GROUP_ORDER: StatusGroupKey[] = ["needs-attention", "error", "running", "done", "idle"];

export const STATUS_GROUP_LABELS: Record<StatusGroupKey, string> = {
  "needs-attention": "Needs attention",
  error: "Error",
  running: "Running",
  done: "Done",
  idle: "Idle",
};

function hasReason(attention: TaskAttention, taskId: number, reason: AttentionReason): boolean {
  return attention.get(taskId)?.has(reason) ?? false;
}

/**
 * Which bucket a task falls into, from the same attention/runStatus data
 * the tree view's dots already use (AC6: "derived from existing status and
 * attention data", no new daemon field).
 *
 * `error` keys off the latest run's status, not the attention reason of
 * the same name -- the attention reason clears the moment the task is
 * opened (useTaskAttention's "seen" logic), but a task whose last run
 * failed stays in the Error bucket regardless of whether anyone's looked
 * at it since; that's a fact about the run, not about the viewer.
 * `permission` and `finished`, by contrast, ARE about the viewer (a
 * pending decision or an unseen result), so they route to
 * "needs-attention" ahead of the coarser running/done buckets.
 */
export function statusGroupForTask(taskId: number, attention: TaskAttention, runStatus: TaskRunStatus): StatusGroupKey {
  if (hasReason(attention, taskId, "permission")) return "needs-attention";
  if (runStatus.get(taskId) === "error") return "error";
  if (hasReason(attention, taskId, "finished")) return "needs-attention";
  if (runStatus.get(taskId) === "running") return "running";
  if (runStatus.get(taskId) === "done") return "done";
  return "idle";
}

export interface StatusGroup {
  key: StatusGroupKey;
  label: string;
  tasks: Task[];
}

/** Buckets every task by statusGroupForTask, dropping empty buckets and keeping STATUS_GROUP_ORDER's ordering. */
export function groupTasksByStatus(tasks: Task[], attention: TaskAttention, runStatus: TaskRunStatus): StatusGroup[] {
  const buckets = new Map<StatusGroupKey, Task[]>();
  for (const task of tasks) {
    const key = statusGroupForTask(task.ID, attention, runStatus);
    const list = buckets.get(key);
    if (list) list.push(task);
    else buckets.set(key, [task]);
  }
  return STATUS_GROUP_ORDER.filter((key) => buckets.has(key)).map((key) => ({
    key,
    label: STATUS_GROUP_LABELS[key],
    tasks: buckets.get(key)!,
  }));
}

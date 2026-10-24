import type { TaskAttention, TaskRunStatus } from "@/hooks/use-task-attention";
import type { Task, TaskGit } from "@/lib/types";

/** The stage view's four groups: where a task's work is in its lifecycle (stage-sidebar-view plan). */
export type StageKey = "working" | "ready" | "done" | "idle";

/** Display order: work in flight first, then what waits for review, then finished work, then everything else. */
export const STAGE_ORDER: StageKey[] = ["working", "ready", "done", "idle"];

export const STAGE_LABELS: Record<StageKey, string> = {
  working: "Working",
  ready: "Ready to review",
  done: "Done",
  idle: "Idle",
};

export interface StageSignals {
  attention: TaskAttention;
  runStatus: TaskRunStatus;
  gitByTask: ReadonlyMap<number, TaskGit>;
}

/**
 * Which stage a task is in, from run state plus git/PR state; first match wins:
 * running or waiting on a permission -> working; merged -> done; something to
 * review (dirty worktree, commits ahead of base, open or draft PR) -> ready;
 * otherwise idle. A failed last run follows the same rows. With no `task.git`
 * entry yet only run state counts, so such a task is never Done.
 */
export function stageForTask(taskId: number, { attention, runStatus, gitByTask }: StageSignals): StageKey {
  if (runStatus.get(taskId) === "running" || attention.get(taskId)?.has("permission")) return "working";
  const git = gitByTask.get(taskId);
  if (!git) return "idle";
  if (git.merged?.state === "merged" || git.pr?.state === "merged") return "done";
  if (git.worktree?.dirty || (git.commits?.aheadOfBase ?? 0) > 0 || git.pr?.state === "open") return "ready";
  return "idle";
}

export interface StageGroup {
  key: StageKey;
  label: string;
  tasks: Task[];
}

function activityMs(task: Task): number {
  const ms = Date.parse(task.UpdatedAt);
  return Number.isNaN(ms) ? Number.NEGATIVE_INFINITY : ms;
}

/** Most recent activity first (`UpdatedAt`), ties by id descending, unparsable dates last. */
function compareActivity(a: Task, b: Task): number {
  const ams = activityMs(a);
  const bms = activityMs(b);
  if (ams !== bms) return ams < bms ? 1 : -1;
  return b.ID - a.ID;
}

/** Buckets every task by stageForTask, dropping empty groups, keeping STAGE_ORDER and sorting each group by activity. */
export function groupTasksByStage(tasks: Task[], signals: StageSignals): StageGroup[] {
  const buckets = new Map<StageKey, Task[]>();
  for (const task of tasks) {
    const key = stageForTask(task.ID, signals);
    const list = buckets.get(key);
    if (list) list.push(task);
    else buckets.set(key, [task]);
  }
  return STAGE_ORDER.filter((key) => buckets.has(key)).map((key) => ({
    key,
    label: STAGE_LABELS[key],
    tasks: buckets.get(key)!.sort(compareActivity),
  }));
}

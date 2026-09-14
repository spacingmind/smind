import type { AttentionReason, TaskRunStatus } from "@/hooks/use-task-attention";
import type { StatusDotStatus } from "@/components/ui/status-dot";
import type { Task } from "@/lib/types";
import type { WorkspaceWithTree, SpaceWithTasks } from "@/lib/workspace-tree";

/**
 * Attention reason -> dot variant. The three reasons already existed
 * (`hooks/use-task-attention.ts`) and all rendered as the same warning
 * dot; Item 12's requirement is that they become distinguishable, which
 * means one variant each rather than one shared one.
 *
 * `finished` maps to success rather than neutral because the dot only
 * exists while the result is *unseen* -- it clears the moment the task is
 * selected -- so it is a "there's a result waiting for you" signal, not a
 * resting state.
 */
const REASON_DOT: Record<AttentionReason, StatusDotStatus> = {
  error: "danger",
  permission: "warning",
  finished: "success",
};

/**
 * Which reason wins when a task has several. Ordered by how much the user
 * losing the race costs them: a failed run is the thing they most need to
 * see, a blocked permission is the thing most likely to be silently
 * stalling work, and a finished run can wait for either.
 */
const REASON_PRECEDENCE: AttentionReason[] = ["error", "permission", "finished"];

/** The winning attention reason for a task, or null when it has none. */
export function primaryAttentionReason(reasons: ReadonlySet<AttentionReason> | undefined): AttentionReason | null {
  if (!reasons || reasons.size === 0) return null;
  return REASON_PRECEDENCE.find((r) => reasons.has(r)) ?? null;
}

/** The dot variant for an attention reason. */
export function attentionDotStatus(reason: AttentionReason): StatusDotStatus {
  return REASON_DOT[reason];
}

/**
 * Run status -> dot variant for a task row. `stopped` is a user-initiated
 * end, not a fault, so it reads neutral rather than danger; `done` reads
 * success. A task with no run at all yields null and renders no dot --
 * the row's leading slot stays reserved and empty, so nothing reflows
 * when the first run starts.
 */
export function runDotStatus(status: string | undefined): StatusDotStatus | null {
  switch (status) {
    case "running":
      return "running";
    case "done":
      return "success";
    case "error":
      return "danger";
    case "stopped":
      return "neutral";
    default:
      return null;
  }
}

/**
 * The aggregate marker a workspace or space row carries for the tasks
 * beneath it (`audit-paseo.md` §5's workspace status bucket): the single
 * loudest signal in the bucket, so a collapsed container still tells the
 * user whether anything below it needs them.
 *
 * Precedence runs attention-before-activity -- a failed run somewhere in
 * the workspace outranks another task merely being busy -- and a bucket
 * with nothing to say yields null rather than a neutral dot, so an idle
 * tree is visually quiet.
 */
export function aggregateStatus(
  tasks: readonly Task[],
  attention: ReadonlyMap<number, ReadonlySet<AttentionReason>> | undefined,
  runStatus: TaskRunStatus,
): StatusDotStatus | null {
  let anyPermission = false;
  let anyRunning = false;
  let anyFinished = false;

  for (const task of tasks) {
    const reason = primaryAttentionReason(attention?.get(task.ID));
    // Error is the top of the precedence order, so it can short-circuit.
    if (reason === "error" || runStatus.get(task.ID) === "error") return "danger";
    if (reason === "permission") anyPermission = true;
    if (reason === "finished") anyFinished = true;
    if (runStatus.get(task.ID) === "running") anyRunning = true;
  }

  if (anyPermission) return "warning";
  if (anyRunning) return "running";
  if (anyFinished) return "success";
  return null;
}

/** Every task under a workspace, across its spaces and its ungrouped bucket. */
export function workspaceTasks(workspace: WorkspaceWithTree): Task[] {
  return [...workspace.spaces.flatMap((sp: SpaceWithTasks) => sp.tasks), ...workspace.ungroupedTasks];
}

/**
 * "Archive finished sub-tasks" and "Stop all in tree" (task-hierarchy-tiers
 * PR-H11): the wire types of the two wsapi RPCs and the pure client-side
 * heuristic that decides whether the menu entries are offered and enabled.
 * The heuristic is advisory only -- the dialogs' counts come from a server
 * dry run (one source of truth), so the menu can be wrong about "nothing
 * runs" without ever doing the wrong thing.
 */

import type { TaskTreeNode } from "@/lib/task-lineage";
import type { RunStatusValue, Task } from "@/lib/types";

/** Item = { id, title } on the wire (PR-H11 §5, flat, no null arrays). */
export interface TreeActionItem {
  id: number;
  title: string;
}

export interface TaskArchiveFinishedResult {
  dryRun: boolean;
  eligible: TreeActionItem[];
  archived: TreeActionItem[];
  skipped: Array<TreeActionItem & { reason: string }>;
  failed: Array<TreeActionItem & { error: string }>;
  remaining: number;
}

export interface StopTreeItem extends TreeActionItem {
  runId: string;
}

export interface TaskStopTreeResult {
  dryRun: boolean;
  runs: StopTreeItem[];
  queuedPrompts: number;
  stopped: StopTreeItem[];
  failed: Array<StopTreeItem & { error: string }>;
  queuedCancelled: number;
}

/** The states the availability heuristic understands (task.tree's run states). */
export type TreeRunState = "running" | "waiting_permission" | "done" | "error" | "stopped" | "idle";

const STOP_REASON_NOTHING_RUNS = "No running tasks in this tree";
const ARCHIVE_REASON_NO_DESCENDANTS = "No sub-tasks";
const ARCHIVE_REASON_NOTHING_FINISHED = "No finished sub-tasks";

export interface TreeActionAvailability {
  /** Stop-all covers the task itself plus its subtree. */
  stop: { enabled: boolean; reason: string | null };
  /** Archive-finished covers descendants only, never the task itself. */
  archive: { enabled: boolean; reason: string | null };
}

function isRunningLike(state: TreeRunState): boolean {
  return state === "running" || state === "waiting_permission";
}

function isFinished(state: TreeRunState): boolean {
  return state === "done" || state === "error" || state === "stopped";
}

/**
 * The pure menu heuristic: `self` is the task's own latest run state,
 * `descendantStates` the same for each live descendant. Stop is enabled when
 * anything in the tree INCLUDING self runs (or waits on a human); archive
 * when any DESCENDANT is finished -- the task itself never counts, its own
 * run is what "finished sub-tasks" would otherwise sweep away.
 */
export function treeActionAvailability(
  self: TreeRunState,
  descendantStates: ReadonlyArray<TreeRunState>,
): TreeActionAvailability {
  const anythingRuns = isRunningLike(self) || descendantStates.some(isRunningLike);
  const finishedDescendant = descendantStates.some(isFinished);
  return {
    stop: { enabled: anythingRuns, reason: anythingRuns ? null : STOP_REASON_NOTHING_RUNS },
    archive:
      descendantStates.length === 0
        ? { enabled: false, reason: ARCHIVE_REASON_NO_DESCENDANTS }
        : { enabled: finishedDescendant, reason: finishedDescendant ? null : ARCHIVE_REASON_NOTHING_FINISHED },
  };
}

/**
 * Adapter for task.tree data (the header): nodes[0] is the queried task,
 * the rest its live descendants in flat pre-order.
 */
export function availabilityFromTaskTree(nodes: ReadonlyArray<TaskTreeNode>): TreeActionAvailability | null {
  const [selfNode, ...rest] = nodes;
  // A task without live descendants offers neither entry (the sidebar's
  // sidebarTreeActions encodes the same rule by keying only parents).
  if (!selfNode || rest.length === 0) return null;
  return treeActionAvailability(
    selfNode.run.state,
    rest.map((n) => n.run.state),
  );
}

function sidebarRunState(status: RunStatusValue | undefined): TreeRunState {
  // The sidebar's runStatus map has no waiting_permission state (only the
  // task.tree build does); absent means "no run known" = idle.
  return status ?? "idle";
}

/**
 * Adapter for the sidebar's flat task list + runStatus map: builds the
 * parent->children links (same orphan rule as nestTasks -- a parent not in
 * the list makes the task a root) and answers availability for every task
 * that HAS live descendants. Tasks absent from the returned map have no
 * descendants and are offered neither entry.
 */
export function sidebarTreeActions(
  tasks: ReadonlyArray<Task>,
  runStatus: ReadonlyMap<number, RunStatusValue>,
): ReadonlyMap<number, TreeActionAvailability> {
  const byId = new Map<number, Task>();
  for (const task of tasks) byId.set(task.ID, task);
  const childrenOf = new Map<number, Task[]>();
  for (const task of tasks) {
    const parent = task.ParentTaskID;
    if (parent == null || !byId.has(parent)) continue;
    const list = childrenOf.get(parent);
    if (list) list.push(task);
    else childrenOf.set(parent, [task]);
  }
  const result = new Map<number, TreeActionAvailability>();
  for (const task of tasks) {
    const kids = childrenOf.get(task.ID);
    if (!kids || kids.length === 0) continue;
    const descendantStates: TreeRunState[] = [];
    const stack = [...kids];
    while (stack.length > 0) {
      const t = stack.pop()!;
      descendantStates.push(sidebarRunState(runStatus.get(t.ID)));
      const grandkids = childrenOf.get(t.ID);
      if (grandkids) stack.push(...grandkids);
    }
    result.set(task.ID, treeActionAvailability(sidebarRunState(runStatus.get(task.ID)), descendantStates));
  }
  return result;
}

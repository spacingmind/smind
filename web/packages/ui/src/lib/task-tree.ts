/**
 * Pure helpers for rendering the task hierarchy in the sidebar's tree view
 * (task-hierarchy-tiers plan, PR-U1). Everything here is derived, never
 * stored: the daemon's Task wire shape carries only `ParentTaskID`, so
 * tiers, nesting and depth are all computed from the loaded task list.
 *
 * One deliberate rule (plan Decision 2): the hierarchy trumps the space
 * grouping -- a child nests under its parent wherever the parent renders,
 * even when its own SpaceID differs. Callers therefore pass ALL of a
 * workspace's tasks (every space's plus the ungrouped bucket) and filter
 * roots back into their own buckets afterwards.
 */

import type { AttentionReason, TaskRunStatus } from "@/hooks/use-task-attention";
import { primaryAttentionReason } from "@/lib/sidebar-signal";
import type { Task } from "@/lib/types";

/**
 * A task's tier, derived from its position (plan Decision 1): a parent
 * without a parent is a supervisor, a parent with a parent is a leader, a
 * childless child is a coder, and a childless root is untiered (a plain
 * task -- most tasks -- and renders with no label at all).
 */
export type Tier = "supervisor" | "leader" | "coder";

export function tierOf(hasParent: boolean, hasChildren: boolean): Tier | null {
  if (hasChildren) return hasParent ? "leader" : "supervisor";
  return hasParent ? "coder" : null;
}

export interface TaskNode {
  task: Task;
  depth: number;
  tier: Tier | null;
  children: TaskNode[];
}

/**
 * Nests a flat task list into root TaskNodes, O(n).
 *
 * Roots are emitted in task-id order, each parent's children likewise. A
 * task whose ParentTaskID is unset -- or points at a task not in `tasks`
 * (an orphan: its parent was archived or simply not loaded) -- is a root,
 * so no task is ever dropped from the list.
 *
 * Cycle-safe: the parent links are followed only while the ancestor chain
 * is making progress, so a corrupt cycle (A->B->A) never loops. Tasks
 * whose ancestry is nothing but the cycle get emitted as extra roots,
 * lowest id first, rather than vanishing.
 */
export function nestTasks(tasks: Task[]): TaskNode[] {
  const byId = new Map<number, Task>();
  const childIds = new Map<number, number[]>();
  const isChild = new Set<number>();
  for (const task of tasks) byId.set(task.ID, task);
  // A parent that isn't in the loaded list is an orphan, not a link: the
  // task becomes its own root (Decision 2: never dropped). byId is fully
  // populated first so input order can't decide who counts as an orphan.
  for (const task of tasks) {
    const parent = task.ParentTaskID;
    if (parent != null && byId.has(parent)) {
      const list = childIds.get(parent);
      if (list) list.push(task.ID);
      else childIds.set(parent, [task.ID]);
      isChild.add(task.ID);
    }
  }

  for (const list of childIds.values()) list.sort((a, b) => a - b);

  const hasChildren = new Set(childIds.keys());
  const nodes = new Map<number, TaskNode>();
  const build = (task: Task, depth: number, visiting: Set<number>): TaskNode => {
    const existing = nodes.get(task.ID);
    if (existing) return existing;
    const node: TaskNode = {
      task,
      depth,
      tier: tierOf(task.ParentTaskID != null && byId.has(task.ParentTaskID), hasChildren.has(task.ID)),
      children: [],
    };
    nodes.set(task.ID, node);
    visiting.add(task.ID);
    for (const childId of childIds.get(task.ID) ?? []) {
      // A child pointing back into the visiting chain would loop forever;
      // cutting the edge there emits the cycle's members as roots instead.
      if (visiting.has(childId)) continue;
      node.children.push(build(byId.get(childId)!, depth + 1, visiting));
    }
    visiting.delete(task.ID);
    return node;
  };

  const roots: TaskNode[] = [];
  for (const task of tasks) {
    if (isChild.has(task.ID)) continue;
    roots.push(build(task, 0, new Set()));
  }
  // Cycle members never appeared as roots above (every one of them is
  // somebody's child), so append them by lowest id -- see the doc comment.
  const cycleRoots: TaskNode[] = [];
  for (const task of tasks) {
    if (nodes.has(task.ID)) continue;
    cycleRoots.push(build(task, 0, new Set()));
  }
  cycleRoots.sort((a, b) => a.task.ID - b.task.ID);
  return [...roots, ...cycleRoots];
}

/** Every task strictly below `node` (not the node itself), depth-first. */
export function descendantsOf(node: TaskNode): Task[] {
  const out: Task[] = [];
  const walk = (n: TaskNode) => {
    for (const child of n.children) {
      out.push(child.task);
      walk(child);
    }
  };
  walk(node);
  return out;
}

/**
 * The attention rollup a COLLAPSED parent shows instead of its children's
 * own dots: how many descendants need the user and why, most urgent reason
 * first. Uses the same precedence as a row's own dot (error > permission >
 * finished) so a collapsed subtree never looks calmer than its loudest
 * child. A descendant whose run status is "error" counts as `error` even
 * when its attention set doesn't say so yet -- the run status is the more
 * authoritative, fresher signal.
 */
export function attentionRollup(
  node: TaskNode,
  attention: ReadonlyMap<number, ReadonlySet<AttentionReason>> | undefined,
  runStatus: TaskRunStatus,
): { reason: AttentionReason | null; count: number } {
  const present = new Set<AttentionReason>();
  let count = 0;
  for (const task of descendantsOf(node)) {
    const reasons = new Set(attention?.get(task.ID) ?? []);
    if (runStatus.get(task.ID) === "error") reasons.add("error");
    const reason = primaryAttentionReason(reasons);
    if (reason === null) continue;
    count += 1;
    present.add(reason);
  }
  return { reason: primaryAttentionReason(present), count };
}

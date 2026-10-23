import type { StatusDotStatus } from "@/components/ui/status-dot";
import { describeRateLimitReset, isRateLimitedRun } from "@/lib/rate-limit";
import { formatCost, formatTokens } from "@/lib/usage-format";

// Wire shapes of the task.tree wsapi call (docs/plans/active/
// task-hierarchy-tiers.md §4.1): lowercase json tags, flat depth-first
// pre-order node list, siblings ordered by task id. The daemon side lands
// in a separate PR; the UI codes against this contract.

/** Per-scope usage totals as task.tree reports them: every field nullable, null means "not reported", NEVER 0 (ADR-0020). */
export interface TreeUsageTotals {
  runs: number;
  inputTokens: number | null;
  outputTokens: number | null;
  cacheReadTokens: number | null;
  cacheWriteTokens: number | null;
  reasoningTokens: number | null;
  costUsd: number | null;
}

/** Descendant status counts over LIVE descendants, never the node itself. */
export interface TaskTreeCounts {
  running: number;
  waitingPermission: number;
  failed: number;
  done: number;
  idle: number;
}

/** The task's latest run's state (waiting_permission wins while any run has a pending request). */
export type TaskTreeRunState = "running" | "waiting_permission" | "done" | "error" | "stopped" | "idle";

export interface TaskTreeNode {
  id: number;
  parentId: number | null;
  workspaceId: number;
  title: string;
  depth: number;
  taskStatus: string;
  tier: "supervisor" | "leader" | "coder" | null;
  childCount: number;
  run: {
    state: TaskTreeRunState;
    runningRunIds: string[];
    pendingPermissions: number;
    /** "needs-continue" when the latest run is "done" but was cut off at a turn/token/budget cap (ADR-0027 D13). */
    disposition?: string;
    /** ADR-0041: "rate_limited" when the latest run failed on a provider quota (state stays "error"). */
    failureKind?: string;
    /** The quota window that was hit ("5h", "weekly", ...). */
    window?: string;
    /** When the limit lifts (RFC 3339 UTC). */
    resetAt?: string;
  };
  counts: TaskTreeCounts;
  usage: {
    own: TreeUsageTotals;
    subtree: TreeUsageTotals;
    archivedDescendants: number;
  };
}

/** Result of task.tree: nodes[0] is the queried task. */
export interface TaskTreeResult {
  nodes: TaskTreeNode[];
  truncated: boolean;
}

/** The nodes whose parentId is taskId, in the list's own (sibling id) order. */
export function childrenOf(nodes: ReadonlyArray<TaskTreeNode>, taskId: number): TaskTreeNode[] {
  return nodes.filter((n) => n.parentId === taskId);
}

/**
 * One-line subtree status summary, e.g. "2 running · 1 needs you · 1 failed
 * · 3 done". Parts in that fixed order, zero parts omitted, idle never
 * shown (an idle child carries no signal); "" when nothing is to say.
 */
export function statusSummary(counts: TaskTreeCounts): string {
  const parts: Array<[number, string]> = [
    [counts.running, "running"],
    [counts.waitingPermission, "needs you"],
    [counts.failed, "failed"],
    [counts.done, "done"],
  ];
  return parts
    .filter(([n]) => n > 0)
    .map(([n, label]) => `${n} ${label}`)
    .join(" · ");
}

/** Maps a task.tree run state onto StatusDot's palette (waiting_permission is the one that needs a human). */
export function pillStatus(state: TaskTreeRunState): StatusDotStatus {
  switch (state) {
    case "running":
      return "running";
    case "waiting_permission":
      return "warning";
    case "error":
      return "danger";
    case "done":
      return "success";
    case "stopped":
    case "idle":
      return "neutral";
  }
}

/** True when the node's latest run failed on a provider quota (state "error" + failureKind "rate_limited"). */
export function isRateLimitedNode(node: Pick<TaskTreeNode, "run">): boolean {
  return isRateLimitedRun({ status: node.run.state, failureKind: node.run.failureKind });
}

/** How many live descendants of `node` in `all` have a rate-limited latest run. */
function rateLimitedDescendants(node: TaskTreeNode, all: ReadonlyArray<TaskTreeNode>): number {
  const inSubtree = new Set<number>([node.id]);
  let count = 0;
  // task.tree is depth-first pre-order, so a parent always precedes its children.
  for (const n of all) {
    if (n.parentId === null || !inSubtree.has(n.parentId)) continue;
    inSubtree.add(n.id);
    if (isRateLimitedNode(n)) count++;
  }
  return count;
}

/** "Rate limited · resets 17:35" for a rate-limited node, null otherwise. The window stays out of the hint (it is a one-liner on a pill). */
export function rateLimitHint(node: Pick<TaskTreeNode, "run">, now: Date): string | null {
  if (!isRateLimitedNode(node)) return null;
  return `Rate limited · ${describeRateLimitReset(node.run.resetAt, now)}`;
}

/**
 * The pill dot a child pill should actually show: a child with its own
 * children must surface the most urgent state of itself plus its subtree
 * (its own run being idle says nothing about a descendant waiting on a
 * human), read off the node's descendant `counts`. Urgency order is
 * failed > waiting_permission > running > anything else, so a red dot is
 * never masked by a calmer one beneath it.
 */
export function pillStatusWithSubtree(node: TaskTreeNode, all?: ReadonlyArray<TaskTreeNode>): StatusDotStatus {
  if (node.counts.failed > 0) {
    // Danger unless every failed descendant is known (from `all`) to be a provider quota (ADR-0041).
    if (!all || rateLimitedDescendants(node, all) < node.counts.failed) return "danger";
    // The node's own plain failure is not counted in `counts`; it still reads danger.
    return node.run.state === "error" && !isRateLimitedNode(node) ? "danger" : "warning";
  }
  // A provider quota (ADR-0041) is not a code failure: the pill is warning, not danger.
  if (isRateLimitedNode(node)) return "warning";
  if (node.counts.waitingPermission > 0 || node.run.state === "waiting_permission") return "warning";
  if (node.counts.running > 0 || node.run.state === "running") return "running";
  // "done" at a cap is not finished: a calm green dot would hide it.
  if (node.run.state === "done" && node.run.disposition === "needs-continue") return "warning";
  return pillStatus(node.run.state);
}

/**
 * The usage chip text: "<cost> · <tokens> tok", or null when nothing to
 * show (runs === 0 or no field reported). The token total sums the
 * non-null of input/output -- both null means "not reported" (an em dash
 * inside formatTokens), never 0, per ADR-0020.
 */
export function usageChipText(usage: TreeUsageTotals): string | null {
  if (usage.runs === 0) return null;
  if (usage.costUsd === null && usage.inputTokens === null && usage.outputTokens === null) return null;
  const tokens = usage.inputTokens === null && usage.outputTokens === null ? null : (usage.inputTokens ?? 0) + (usage.outputTokens ?? 0);
  return `${formatCost(usage.costUsd)} · ${formatTokens(tokens, "compact")} tok`;
}

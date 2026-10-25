// taskTree.ts turns a task.tree result into the rows the Subtasks section
// renders -- pure (no React) so every label, countdown and bound is
// unit-tested. Decisions 3-6 of docs/plans/completed/mobile-task-tree.md.

import type { TaskTreeNode, TaskTreeResult } from './api';

/** The most nodes the phone renders; the rest collapses into one 'more' row. */
export const MAX_RENDERED_NODES = 100;
const MAX_DEPTH = 8;

export type Tier = 'supervisor' | 'leader' | 'coder';
export type StateTone = 'default' | 'warning' | 'danger' | 'muted';

export type TreeRow =
  | {
      kind: 'node';
      id: number;
      title: string;
      depth: number;
      tier: Tier | null;
      tierLabel: string | null;
      state: string;
      stateTone: StateTone;
      stateNote?: string;
      permissions: number;
      unread: boolean;
      rateLimited: boolean;
      countdown: string | null;
      /** 'Stalled · no run for 23 min' while the node's tree is stalled, else null. */
      stalledNote: string | null;
      cost: string;
      isRoot: boolean;
    }
  | { kind: 'more'; hiddenCount: number; truncated: boolean; label: string };

type RunLike = Partial<TaskTreeNode['run']> | null | undefined;

export function tierLabel(tier: string | null | undefined): string | null {
  switch (tier) {
    case 'supervisor':
      return 'Supervisor';
    case 'leader':
      return 'Lead';
    case 'coder':
      return 'Coder';
    default:
      return null;
  }
}

function isRateLimited(run: RunLike): boolean {
  return run?.state === 'error' && run.failureKind === 'rate_limited';
}

/** The run state word; a rate-limited failure overrides 'Error'. */
export function runStateLabel(run: RunLike): string {
  if (isRateLimited(run)) return 'Rate limited';
  const state = run?.state;
  switch (state) {
    case undefined:
    case '':
    case 'idle':
      return 'Idle';
    case 'running':
      return 'Running';
    case 'waiting_permission':
      return 'Waiting for permission';
    case 'done':
      return 'Done';
    case 'error':
      return 'Error';
    case 'stopped':
      return 'Stopped';
    default:
      return state.charAt(0).toUpperCase() + state.slice(1);
  }
}

/** A secondary note under the state word ('done' cut off at a cap). */
export function runStateNote(run: RunLike): string | undefined {
  if (run?.state === 'done' && run.disposition === 'needs-continue') return 'Hit a cap, needs continue';
  return undefined;
}

export function runStateTone(run: RunLike): StateTone {
  if (isRateLimited(run)) return 'warning';
  switch (run?.state) {
    case 'running':
    case 'waiting_permission':
      return 'warning';
    case 'error':
      return 'danger';
    case 'stopped':
    case 'idle':
    case undefined:
    case '':
      return 'muted';
    default:
      return 'default';
  }
}

/** Coarse floor of a duration: 'Ns' under a minute, then 'Nm', 'Nh', 'Nd'. */
export function formatCountdown(ms: number): string {
  const s = Math.max(0, Math.floor((Number.isFinite(ms) ? ms : 0) / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

/** 'resets in 12m' for a rate-limited run whose resetAt is still ahead, else null. */
export function rateLimitCountdown(run: RunLike, now: Date): string | null {
  if (!isRateLimited(run) || !run?.resetAt) return null;
  const at = Date.parse(run.resetAt);
  if (!Number.isFinite(at)) return null;
  const ms = at - now.getTime();
  if (ms <= 0) return null;
  return `resets in ${run.resetInferred ? '≈' : ''}${formatCountdown(ms)}`;
}

/** 'Stalled · no run for 23 min' (whole minutes since stalledSince), null when the node is not stalled. */
export function stalledNote(node: Pick<TaskTreeNode, 'stalled' | 'stalledSince'>, now: Date): string | null {
  if (!node.stalled || !node.stalledSince) return null;
  const since = Date.parse(node.stalledSince);
  if (!Number.isFinite(since)) return null;
  return `Stalled · no run for ${Math.max(0, Math.floor((now.getTime() - since) / 60000))} min`;
}

/** Cost text: unknown is 'n/a' and never '$0.00'. */
export function formatCost(costUsd: number | null | undefined): string {
  if (costUsd === null || costUsd === undefined || !Number.isFinite(costUsd)) return 'n/a';
  if (costUsd > 0 && costUsd < 0.01) return '<$0.01';
  return `$${costUsd.toFixed(2)}`;
}

/** Own cost, plus the subtree rollup when the node has children. */
export function costLabel(node: Pick<TaskTreeNode, 'childCount' | 'usage'>): string {
  const own = formatCost(node.usage?.own?.costUsd);
  if (!(node.childCount > 0)) return own;
  const subtree = formatCost(node.usage?.subtree?.costUsd);
  if (own === 'n/a' && subtree === 'n/a') return 'n/a';
  return `${own} (subtree ${subtree})`;
}

export type BudgetLine = { text: string; tone: StateTone };

/**
 * The tree's opt-in budget line under the Subtasks header (web wording from
 * lib/task-budget.ts): 'Budget $4.20 / $5.00', '$4.20+ / $5.00 · partial' when
 * some runs reported no cost, 'Over budget' (danger) once spent reaches the cap.
 * Null when the root has no cap.
 */
export function budgetLine(root: Pick<TaskTreeNode, 'budgetUsd' | 'budgetSpentUsd' | 'budgetPartial' | 'budgetUnknownRuns' | 'overBudget'> | undefined): BudgetLine | null {
  const cap = root?.budgetUsd;
  if (cap == null || !Number.isFinite(cap) || !(cap > 0)) return null;
  const spent = root?.budgetSpentUsd ?? 0;
  const partial = (root?.budgetUnknownRuns ?? 0) > 0 || root?.budgetPartial === true;
  const over = Math.round(spent * 100) >= Math.round(cap * 100) || root?.overBudget === true;
  const amounts = `$${spent.toFixed(2)}${partial ? '+' : ''} / $${cap.toFixed(2)}`;
  if (over) return { text: `Over budget · ${amounts}${partial ? ' · partial' : ''}`, tone: 'danger' };
  return { text: partial ? `${amounts} · partial` : `Budget ${amounts}`, tone: 'default' };
}

/** Waiting-for-permission, failed and rate-limited counts over the given nodes' own run state; a rate-limited run is not a failure. */
export function attentionRollup(nodes: readonly TaskTreeNode[]): { waiting: number; failed: number; rateLimited: number } {
  let waiting = 0;
  let failed = 0;
  let rateLimited = 0;
  for (const n of nodes) {
    if (n.run?.state === 'waiting_permission') waiting++;
    else if (isRateLimited(n.run)) rateLimited++;
    else if (n.run?.state === 'error') failed++;
  }
  return { waiting, failed, rateLimited };
}

function clampDepth(depth: unknown): number {
  if (typeof depth !== 'number' || !Number.isFinite(depth)) return 0;
  return Math.min(MAX_DEPTH, Math.max(0, Math.trunc(depth)));
}

function moreLabel(hidden: number, truncated: boolean): string {
  if (hidden <= 0) return 'More not shown';
  return truncated ? `${hidden} more · more not shown` : `${hidden} more`;
}

/**
 * The rows of the Subtasks section, in the daemon's pre-order. Empty for a
 * leaf (<=1 node): the section is only shown when there is a subtree.
 */
export function buildTreeRows(
  result: TaskTreeResult,
  now: Date,
  pendingTaskIds: ReadonlySet<number>,
): { rows: TreeRow[]; hiddenCount: number; attention: { waiting: number; failed: number; rateLimited: number }; budget: BudgetLine | null } {
  const nodes = Array.isArray(result?.nodes) ? result.nodes : [];
  if (nodes.length <= 1) return { rows: [], hiddenCount: 0, attention: { waiting: 0, failed: 0, rateLimited: 0 }, budget: null };
  const shown = nodes.slice(0, MAX_RENDERED_NODES);
  const hiddenCount = nodes.length - shown.length;
  const truncated = result.truncated === true;
  const rows: TreeRow[] = shown.map((n, i) => {
    const permissions = n.run?.pendingPermissions > 0 ? n.run.pendingPermissions : 0;
    const row: TreeRow = {
      kind: 'node',
      id: n.id,
      title: n.title ?? '',
      depth: clampDepth(n.depth),
      tier: n.tier ?? null,
      tierLabel: tierLabel(n.tier),
      state: runStateLabel(n.run),
      stateTone: runStateTone(n.run),
      permissions,
      unread: pendingTaskIds.has(n.id) || permissions > 0,
      rateLimited: isRateLimited(n.run),
      countdown: rateLimitCountdown(n.run, now),
      stalledNote: stalledNote(n, now),
      cost: costLabel(n),
      isRoot: i === 0,
    };
    const note = runStateNote(n.run);
    if (note) row.stateNote = note;
    return row;
  });
  if (hiddenCount > 0 || truncated) {
    rows.push({ kind: 'more', hiddenCount, truncated, label: moreLabel(hiddenCount, truncated) });
  }
  return { rows, hiddenCount, attention: attentionRollup(shown), budget: budgetLine(nodes[0]) };
}

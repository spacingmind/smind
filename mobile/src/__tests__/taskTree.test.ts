// taskTree.test.ts: the pure rows/labels behind the mobile Subtasks section.

import { describe, expect, it } from 'vitest';
import type { TaskTreeNode, TaskTreeUsage } from '../api';
import {
  MAX_RENDERED_NODES,
  attentionRollup,
  budgetLine,
  buildTreeRows,
  costLabel,
  formatCost,
  formatCountdown,
  rateLimitCountdown,
  runStateLabel,
  stalledNote,
  tierLabel,
} from '../taskTree';

const NOW = new Date('2026-10-23T12:00:00Z');
const none = new Set<number>();

function usage(costUsd: number | null): TaskTreeUsage {
  return { runs: 1, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd };
}

function node(id: number, over: Partial<TaskTreeNode> = {}): TaskTreeNode {
  return {
    id,
    parentId: id === 1 ? null : 1,
    workspaceId: 1,
    title: `task ${id}`,
    depth: id === 1 ? 0 : 1,
    taskStatus: 'running',
    tier: null,
    childCount: 0,
    run: { state: 'idle', runningRunIds: [], pendingPermissions: 0 },
    counts: { running: 0, waitingPermission: 0, failed: 0, done: 0, idle: 0 },
    usage: { own: usage(0.5), subtree: usage(1.5), archivedDescendants: 0 },
    ...over,
  };
}

function run(over: Partial<TaskTreeNode['run']>): TaskTreeNode['run'] {
  return { state: 'idle', runningRunIds: [], pendingPermissions: 0, ...over };
}

function nodes(n: number): TaskTreeNode[] {
  return Array.from({ length: n }, (_, i) => node(i + 1));
}

describe('taskTree', () => {
  it('buildTreeRows keeps pre-order and indents by depth', () => {
    const list = [node(1, { tier: 'supervisor', childCount: 2 }), node(2, { depth: 1, tier: 'leader', childCount: 1 }), node(3, { depth: 2, tier: 'coder' }), node(4, { depth: 1, tier: 'coder' })];
    const { rows } = buildTreeRows({ nodes: list, truncated: false }, NOW, none);
    expect(rows.map((r) => (r.kind === 'node' ? [r.id, r.depth, r.isRoot] : null))).toEqual([
      [1, 0, true],
      [2, 1, false],
      [3, 2, false],
      [4, 1, false],
    ]);
  });

  it('buildTreeRows is empty for a single-node (leaf) tree', () => {
    expect(buildTreeRows({ nodes: [node(1)], truncated: false }, NOW, none).rows).toEqual([]);
    expect(buildTreeRows({ nodes: [], truncated: false }, NOW, none).rows).toEqual([]);
  });

  it('buildTreeRows caps at MAX_RENDERED_NODES and reports hiddenCount', () => {
    expect(MAX_RENDERED_NODES).toBe(100); // the plan's bound (decision 3); a change must be deliberate
    const out = buildTreeRows({ nodes: nodes(MAX_RENDERED_NODES + 12), truncated: false }, NOW, none);
    expect(out.hiddenCount).toBe(12);
    expect(out.rows).toHaveLength(MAX_RENDERED_NODES + 1);
    expect(out.rows.at(-1)).toEqual({ kind: 'more', hiddenCount: 12, truncated: false, label: '12 more' });
    expect(out.rows.filter((r) => r.kind === 'more')).toHaveLength(1);
    const exact = buildTreeRows({ nodes: nodes(MAX_RENDERED_NODES), truncated: false }, NOW, none);
    expect(exact.rows).toHaveLength(MAX_RENDERED_NODES);
  });

  it('buildTreeRows adds the daemon-truncated flag to the more row', () => {
    const over = buildTreeRows({ nodes: nodes(MAX_RENDERED_NODES + 3), truncated: true }, NOW, none);
    expect(over.rows.at(-1)).toEqual({ kind: 'more', hiddenCount: 3, truncated: true, label: '3 more · more not shown' });
    const small = buildTreeRows({ nodes: nodes(3), truncated: true }, NOW, none);
    expect(small.hiddenCount).toBe(0);
    expect(small.rows.at(-1)).toEqual({ kind: 'more', hiddenCount: 0, truncated: true, label: 'More not shown' });
  });

  it('buildTreeRows tolerates a negative or missing depth', () => {
    const list = [node(1), node(2, { depth: -3 }), node(3, { depth: undefined as unknown as number }), node(4, { depth: 99 })];
    const { rows } = buildTreeRows({ nodes: list, truncated: false }, NOW, none);
    expect(rows.map((r) => (r.kind === 'node' ? r.depth : -1))).toEqual([0, 0, 0, 8]);
  });

  it('runStateLabel maps every daemon state and needs-continue', () => {
    expect(runStateLabel(run({ state: 'running' }))).toBe('Running');
    expect(runStateLabel(run({ state: 'waiting_permission' }))).toBe('Waiting for permission');
    expect(runStateLabel(run({ state: 'done' }))).toBe('Done');
    expect(runStateLabel(run({ state: 'error' }))).toBe('Error');
    expect(runStateLabel(run({ state: 'stopped' }))).toBe('Stopped');
    expect(runStateLabel(run({ state: 'idle' }))).toBe('Idle');
    expect(runStateLabel(run({ state: 'weird' }))).toBe('Weird');
    expect(runStateLabel(undefined)).toBe('Idle');
    expect(runStateLabel({})).toBe('Idle');
    expect(runStateLabel(run({ state: 'done', disposition: 'needs-continue' }))).toBe('Done');
    const { rows } = buildTreeRows({ nodes: [node(1), node(2, { run: run({ state: 'done', disposition: 'needs-continue' }) })], truncated: false }, NOW, none);
    expect(rows[1]).toMatchObject({ state: 'Done', stateNote: 'Hit a cap, needs continue' });
    expect(rows[0]).not.toHaveProperty('stateNote');
  });

  it('rate_limited failure overrides error and shows the countdown', () => {
    const limited = node(2, { run: run({ state: 'error', failureKind: 'rate_limited', window: '5h', resetAt: '2026-10-23T12:12:30Z' }) });
    const { rows } = buildTreeRows({ nodes: [node(1), limited], truncated: false }, NOW, none);
    expect(rows[1]).toMatchObject({ state: 'Rate limited', stateTone: 'warning', rateLimited: true, countdown: 'resets in 12m' });
    const plain = buildTreeRows({ nodes: [node(1), node(3, { run: run({ state: 'error', failureKind: 'crash' }) })], truncated: false }, NOW, none);
    expect(plain.rows[1]).toMatchObject({ state: 'Error', stateTone: 'danger', rateLimited: false, countdown: null });
  });

  it('countdown has ≈ when inferred', () => {
    const mk = (resetInferred?: boolean) => node(2, { run: run({ state: 'error', failureKind: 'rate_limited', resetAt: '2026-10-23T12:12:30Z', resetInferred }) });
    expect(rateLimitCountdown(mk(true).run, NOW)).toBe('resets in ≈12m');
    expect(rateLimitCountdown(mk(false).run, NOW)).toBe('resets in 12m');
  });

  it('rate limit without resetAt or with a past resetAt shows no countdown', () => {
    const mk = (resetAt?: string) => node(2, { run: run({ state: 'error', failureKind: 'rate_limited', resetAt }) });
    for (const resetAt of [undefined, '2026-10-23T11:00:00Z', 'not a date', '2026-10-23T12:00:00Z']) {
      const { rows } = buildTreeRows({ nodes: [node(1), mk(resetAt)], truncated: false }, NOW, none);
      expect(rows[1]).toMatchObject({ state: 'Rate limited', rateLimited: true, countdown: null });
    }
  });

  it('formatCountdown rounds to s/m/h/d', () => {
    expect(formatCountdown(0)).toBe('0s');
    expect(formatCountdown(59_999)).toBe('59s');
    expect(formatCountdown(60_000)).toBe('1m');
    expect(formatCountdown(12 * 60_000 + 30_000)).toBe('12m');
    expect(formatCountdown(3_600_000 - 1)).toBe('59m');
    expect(formatCountdown(3_600_000)).toBe('1h');
    expect(formatCountdown(23 * 3_600_000 + 59 * 60_000)).toBe('23h');
    expect(formatCountdown(24 * 3_600_000)).toBe('1d');
    expect(formatCountdown(49 * 3_600_000)).toBe('2d');
    expect(formatCountdown(-5)).toBe('0s');
  });

  it('formatCost shows n/a for null, <$0.01 for tiny, never $0 for unknown', () => {
    expect(formatCost(null)).toBe('n/a');
    expect(formatCost(undefined)).toBe('n/a');
    expect(formatCost(NaN)).toBe('n/a');
    expect(formatCost(0.004)).toBe('<$0.01');
    expect(formatCost(0)).toBe('$0.00');
    expect(formatCost(0.01)).toBe('$0.01');
    expect(formatCost(1.374)).toBe('$1.37');
    expect(formatCost(null)).not.toMatch(/\$0/);
  });

  it('costLabel shows own and subtree, omits subtree for a childless node', () => {
    const parent = node(1, { childCount: 2, usage: { own: usage(0.42), subtree: usage(1.37), archivedDescendants: 0 } });
    expect(costLabel(parent)).toBe('$0.42 (subtree $1.37)');
    expect(costLabel(node(2, { usage: { own: usage(0.42), subtree: usage(0.42), archivedDescendants: 0 } }))).toBe('$0.42');
    expect(costLabel(node(3, { childCount: 1, usage: { own: usage(null), subtree: usage(null), archivedDescendants: 0 } }))).toBe('n/a');
    expect(costLabel(node(4, { usage: { own: usage(null), subtree: usage(null), archivedDescendants: 0 } }))).toBe('n/a');
  });

  it('attentionRollup counts waiting and failed over the subtree', () => {
    const list = [
      node(1, { run: run({ state: 'running' }) }),
      node(2, { run: run({ state: 'waiting_permission' }) }),
      node(3, { run: run({ state: 'waiting_permission' }) }),
      node(4, { run: run({ state: 'error' }) }),
      node(5, { run: run({ state: 'done' }) }),
    ];
    expect(attentionRollup(list)).toEqual({ waiting: 2, failed: 1, rateLimited: 0 });
    expect(attentionRollup([])).toEqual({ waiting: 0, failed: 0, rateLimited: 0 });
    expect(buildTreeRows({ nodes: list, truncated: false }, NOW, none).attention).toEqual({ waiting: 2, failed: 1, rateLimited: 0 });
  });

  it('attentionRollup counts a rate-limited run as rate limited, not failed', () => {
    const list = [
      node(1, { run: run({ state: 'running' }) }),
      node(2, { run: run({ state: 'error', failureKind: 'rate_limited' }) }),
      node(3, { run: run({ state: 'error' }) }),
    ];
    expect(attentionRollup(list)).toEqual({ waiting: 0, failed: 1, rateLimited: 1 });
    const only = [node(1), node(2, { run: run({ state: 'error', failureKind: 'rate_limited' }) })];
    expect(buildTreeRows({ nodes: only, truncated: false }, NOW, none).attention).toEqual({ waiting: 0, failed: 0, rateLimited: 1 });
  });

  it('unread comes from pendingTaskIds or pendingPermissions', () => {
    const list = [node(1), node(2), node(3, { run: run({ pendingPermissions: 2 }) }), node(4)];
    const { rows } = buildTreeRows({ nodes: list, truncated: false }, NOW, new Set([2]));
    expect(rows.map((r) => (r.kind === 'node' ? r.unread : null))).toEqual([false, true, true, false]);
    expect(rows[2]).toMatchObject({ permissions: 2 });
  });

  it('tierLabel maps supervisor/leader/coder and null', () => {
    expect(tierLabel('supervisor')).toBe('Supervisor');
    expect(tierLabel('leader')).toBe('Lead');
    expect(tierLabel('coder')).toBe('Coder');
    expect(tierLabel(null)).toBeNull();
    expect(tierLabel('other')).toBeNull();
  });
});

describe('stalledNote', () => {
  it('shows whole minutes since stalledSince on the row and nothing when not stalled', () => {
    const since = '2026-10-23T11:36:30Z';
    const { rows } = buildTreeRows({ nodes: [node(1), node(2, { stalled: true, stalledSince: since })], truncated: false }, NOW, none);
    expect(rows[1]).toMatchObject({ stalledNote: 'Stalled · no run for 23 min' });
    expect(rows[0]).toMatchObject({ stalledNote: null });
    expect(stalledNote({ stalled: true, stalledSince: 'nope' }, NOW)).toBeNull();
  });
});

describe('tree budget line', () => {
  const tree = (root: Partial<TaskTreeNode>) => ({ nodes: [node(1, { childCount: 1, ...root }), node(2)], truncated: false });

  it('shows nothing without a budget', () => {
    expect(budgetLine(node(1))).toBeNull();
    expect(budgetLine(node(1, { budgetUsd: null }))).toBeNull();
    expect(buildTreeRows(tree({}), NOW, none).budget).toBeNull();
  });

  it('shows spent / cap under the cap', () => {
    expect(budgetLine(node(1, { budgetUsd: 5, budgetSpentUsd: 4.2 }))).toEqual({ text: 'Budget $4.20 / $5.00', tone: 'default' });
    expect(buildTreeRows(tree({ budgetUsd: 5, budgetSpentUsd: 4.2 }), NOW, none).budget?.text).toBe('Budget $4.20 / $5.00');
  });

  it('treats a missing spent as $0.00', () => {
    expect(budgetLine(node(1, { budgetUsd: 5 }))?.text).toBe('Budget $0.00 / $5.00');
  });

  it('marks a partial total with + and "partial"', () => {
    expect(budgetLine(node(1, { budgetUsd: 5, budgetSpentUsd: 4.2, budgetPartial: true }))).toEqual({ text: '$4.20+ / $5.00 · partial', tone: 'default' });
    expect(budgetLine(node(1, { budgetUsd: 5, budgetSpentUsd: 1, budgetUnknownRuns: 2 }))?.text).toBe('$1.00+ / $5.00 · partial');
  });

  it('reads as over budget in the danger tone', () => {
    expect(budgetLine(node(1, { budgetUsd: 5, budgetSpentUsd: 5.5, overBudget: true }))).toEqual({ text: 'Over budget · $5.50 / $5.00', tone: 'danger' });
    expect(budgetLine(node(1, { budgetUsd: 5, budgetSpentUsd: 5 }))?.tone).toBe('danger');
    expect(budgetLine(node(1, { budgetUsd: 5, budgetSpentUsd: 6, overBudget: true, budgetPartial: true }))?.text).toBe('Over budget · $6.00+ / $5.00 · partial');
  });
});

describe('buildTreeRows remote nodes (F4)', () => {
  it('carries the host on a remote node and none on a local one', () => {
    const { rows } = buildTreeRows(
      { nodes: [node(1), node(2), node(-7, { remote: true, host: 'box', hostId: 'h1', globalId: 'box:7' })], truncated: false },
      NOW,
      none,
    );
    const [, local, remote] = rows;
    expect(local).toMatchObject({ remote: false, host: null });
    expect(remote).toMatchObject({ remote: true, host: 'box', id: -7 });
  });
});

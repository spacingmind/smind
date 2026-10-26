// automationHistory.test.ts: run-history rows, the decision chip text and paging with before=<oldest id shown>.

import { describe, expect, it } from 'vitest';
import { AUDIT_PAGE_SIZE, fetchAutomationAudit } from '../automations/api';
import { appendPage, auditRowView, decisionChip, nextBefore } from '../automations/historyView';
import { auditRow, fakeConnection } from './automationFixtures';

describe('decision chip', () => {
  it('known decisions get short labels and tones', () => {
    expect(decisionChip('fire')).toEqual({ label: 'Fired', tone: 'success' });
    expect(decisionChip('deduped').label).toBe('Deduped');
    expect(decisionChip('capped')).toEqual({ label: 'Capped', tone: 'warning' });
    expect(decisionChip('paused').label).toBe('Paused');
    expect(decisionChip('missed').label).toBe('Missed');
    expect(decisionChip('disabled_kill_switch').label).toBe('Kill switch');
    expect(decisionChip('failed')).toEqual({ label: 'Failed', tone: 'destructive' });
  });

  it('an unknown decision is shown as the daemon sent it; empty becomes Unknown', () => {
    expect(decisionChip('quarantined')).toEqual({ label: 'quarantined', tone: 'neutral' });
    expect(decisionChip('').label).toBe('Unknown');
  });
});

describe('audit row view', () => {
  it('when, chip, reason, outcome, task id and origin markers', () => {
    const v = auditRowView(auditRow({ id: 9, decision: 'capped', reason: 'rate limit: 2 per hour', outcome: '', taskId: null, manual: true, catchup: true, createdAt: '2026-10-21T19:17:05Z' }));
    expect(v).toMatchObject({ id: 9, when: '2026-10-21 19:17 UTC', decision: 'Capped', reason: 'rate limit: 2 per hour', outcome: null, task: null, origin: 'Manual · Catch-up' });
    const fired = auditRowView(auditRow({ decision: 'fire', taskId: 12, outcome: 'done' }));
    expect(fired).toMatchObject({ decision: 'Fired', task: 'Task 12', taskId: 12, outcome: 'done', reason: null, origin: null });
  });
});

describe('paging', () => {
  const rows = (from: number, to: number) => Array.from({ length: from - to + 1 }, (_, i) => auditRow({ id: from - i }));

  it('the cursor is the oldest id shown; the first page uses before=0', async () => {
    const { conn, calls } = fakeConnection((_, p) => ({ rows: (p as { before: number }).before === 0 ? rows(50, 26) : rows(25, 11) }));
    const first = await fetchAutomationAudit(conn, 5, 0);
    expect(first).toHaveLength(AUDIT_PAGE_SIZE);
    expect(nextBefore(first)).toBe(26);
    const second = await fetchAutomationAudit(conn, 5, nextBefore(first));
    expect(calls.map(([, p]) => p)).toEqual([
      { id: 5, limit: 25, before: 0 },
      { id: 5, limit: 25, before: 26 },
    ]);
    expect(nextBefore([])).toBe(0);
    const shown = appendPage(appendPage({ rows: [], more: false }, first, AUDIT_PAGE_SIZE), second, AUDIT_PAGE_SIZE);
    expect(shown.rows.map((r) => r.id)).toEqual([...first.map((r) => r.id), ...second.map((r) => r.id)]);
    expect(shown.more).toBe(false); // 15 < page size: that was the last page
  });

  it('a full page offers more; a repeated id is not duplicated', () => {
    const page = rows(25, 1);
    const a = appendPage({ rows: [], more: false }, page, 25);
    expect(a.more).toBe(true);
    const b = appendPage(a, [page[24], auditRow({ id: 0 })], 25);
    expect(b.rows.map((r) => r.id).slice(-2)).toEqual([1, 0]);
    expect(b.rows).toHaveLength(26);
  });
});

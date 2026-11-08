// stewardView.test.ts: the verdict chip never throws, gates sort failing-first, findings group per
// file, the copyable findings text, and the card header / frame source.

import { describe, expect, it } from 'vitest';
import { StewardFinding, StewardReview } from '../steward/types';
import {
  axisLabel,
  axisStatusLabel,
  blockedReasonLabel,
  combinedSentence,
  criteriaCounts,
  findingsAsPrompt,
  frameSource,
  gateRow,
  groupFindingsByAxisThenFile,
  groupFindingsByFile,
  isPartialAxes,
  reviewHeader,
  severityLabel,
  sortGatesFailingFirst,
  verdictChip,
} from '../steward/view';

function review(over: Partial<StewardReview> = {}): StewardReview {
  return {
    id: 5,
    taskId: 7,
    triggerKind: 'human',
    headSha: 'abcdef1234567890',
    validationId: 3,
    ciState: 'pass',
    status: 'done',
    findings: [],
    gates: [],
    costUsd: 0.42,
    createdAt: '2026-10-27T10:00:00Z',
    ...over,
  };
}

describe('steward chip and gates', () => {
  it('steward verdict chip never throws', () => {
    expect(verdictChip('ACCEPTED')).toEqual({ text: 'Accepted', tone: 'success', icon: 'check' });
    expect(verdictChip('CHANGES')).toEqual({ text: 'Changes requested', tone: 'warning', icon: 'warning' });
    expect(verdictChip('BLOCKED')).toEqual({ text: 'Blocked', tone: 'destructive', icon: 'stop' });
    expect(verdictChip(undefined)).toEqual({ text: 'Pending', tone: 'neutral', icon: 'info' });
    expect(verdictChip('')).toEqual({ text: 'Pending', tone: 'neutral', icon: 'info' });
    expect(verdictChip('WEIRD')).toEqual({ text: 'WEIRD', tone: 'neutral', icon: 'info' });
    expect(() => gateRow({ id: 'x', state: '' })).not.toThrow();
    expect(gateRow({ id: 'x', state: '' })).toMatchObject({ stateText: 'unknown', icon: 'info', failing: false });
  });

  it('steward gates failing first', () => {
    const gates = [
      { id: 'a', state: 'pass' },
      { id: 'b', state: 'fail' },
      { id: 'c', state: 'unknown' },
      { id: 'd', state: 'fail' },
    ];
    expect(sortGatesFailingFirst(gates).map((g) => g.id)).toEqual(['b', 'd', 'a', 'c']);
    expect(gates.map((g) => g.id)).toEqual(['a', 'b', 'c', 'd']);
  });
});

describe('steward findings', () => {
  it('steward findings grouped by file with unrated', () => {
    const findings: StewardFinding[] = [
      { file: 'b.ts', line: 2, why: 'off', check: 'lint' },
      { file: 'a.ts', line: 9, why: 'w9', check: 'c9', severity: 'high' },
      { file: 'a.ts', line: 3, why: 'w3', check: 'c3' },
    ];
    const groups = groupFindingsByFile(findings);
    expect(groups.map((g) => g.file)).toEqual(['b.ts', 'a.ts']);
    expect(groups[1].findings.map((f) => f.line)).toEqual([3, 9]);
    expect(severityLabel(findings[0])).toBe('unrated');
    expect(severityLabel(findings[1])).toBe('high');
  });

  it('steward findings prompt', () => {
    const text = findingsAsPrompt(
      review({
        verdict: 'CHANGES',
        findings: [
          { file: 'a.ts', line: 3, why: 'broken', check: 'it passes' },
          { file: 'b.ts', line: 4, why: 'off', check: 'lint', severity: 'high' },
        ],
      }),
    );
    const lines = text.split('\n');
    expect(lines[0]).toBe('Steward review 5 of task 7 at abcdef1: Changes requested');
    expect(lines).toContain('- a.ts:3 broken (check: it passes)');
    expect(lines).toContain('- b.ts:4 [high] off (check: lint)');
    expect(findingsAsPrompt(review())).toContain('No findings.');
  });
});

describe('steward header and frames', () => {
  it('reviewHeader and frameSource', () => {
    const h = reviewHeader(
      review({ prNumber: 812, headSha: '3fa9c1efff', stale: 'older head seen', verdict: 'CHANGES', summary: 'ok', blockedReason: 'no budget' }),
    );
    expect(h.chip.text).toBe('Changes requested');
    expect(h.pr).toBe('PR #812');
    expect(h.head7).toBe('3fa9c1e');
    expect(h.staleNote).toBe('Stale: older head seen');
    expect(h.summary).toBe('ok');
    expect(h.blockedReason).toBeNull();
    expect(reviewHeader(review({ verdict: 'BLOCKED', blockedReason: 'quota spent' })).blockedReason).toBe('Reason: quota spent');
    expect(reviewHeader(review()).pr).toBeNull();
    expect(reviewHeader(review()).staleNote).toBeNull();

    expect(frameSource({ contentType: 'image/png', data: 'aGk=' })).toBe('data:image/png;base64,aGk=');
    expect(frameSource({ contentType: 'image/jpeg', data: 'aGk=' })).toBeNull();
    expect(frameSource({ contentType: 'image/png', data: '' })).toBeNull();
    expect(frameSource(null)).toBeNull();
    expect(frameSource(undefined)).toBeNull();
  });

  it('steward review tolerates a missing head sha', () => {
    const r = review({ headSha: undefined as never });
    expect(reviewHeader(r).head7).toBe('');
    expect(() => findingsAsPrompt(r)).not.toThrow();
    expect(findingsAsPrompt(r).startsWith('Steward review ')).toBe(true);
  });
});

describe('axis helpers', () => {
  const ax = (status: string, seatVerdict?: string, blockedReason?: string) => ({ axis: 'spec', status, seatVerdict, blockedReason, costUsd: 0 });
  const t = (a: ReturnType<typeof ax>) => {
    const { text, tone } = axisStatusLabel(a);
    return { text, tone };
  };

  it('axisStatusLabel maps every status', () => {
    expect(t(ax('done', 'ACCEPTED'))).toEqual({ text: 'Accepted', tone: 'success' });
    expect(t(ax('done', 'CHANGES'))).toEqual({ text: 'Changes requested', tone: 'warning' });
    expect(t(ax('done', 'BLOCKED'))).toEqual({ text: 'Blocked', tone: 'destructive' });
    expect(t(ax('done'))).toEqual({ text: 'Done', tone: 'neutral' });
    expect(t(ax('running'))).toEqual({ text: 'Running', tone: 'neutral' });
    expect(t(ax('pending'))).toEqual({ text: 'Pending', tone: 'neutral' });
    expect(t(ax('skipped', undefined, 'no_spec'))).toEqual({ text: 'Skipped · no spec available', tone: 'neutral' });
    expect(t(ax('skipped'))).toEqual({ text: 'Skipped', tone: 'neutral' });
    expect(t(ax('blocked', undefined, 'timeout'))).toEqual({ text: 'Did not complete', tone: 'destructive' });
    expect(t(ax('blocked', undefined, 'not_run'))).toEqual({ text: 'Not run', tone: 'neutral' });
    expect(axisStatusLabel(ax('done', 'ACCEPTED')).icon).toBe('check');
    expect(axisStatusLabel(ax('done', 'CHANGES')).icon).toBe('warning');
    expect(axisStatusLabel(ax('blocked')).icon).toBe('stop');
    expect(axisStatusLabel(ax('running')).icon).toBe('info');
  });

  it('blockedReasonLabel falls back to raw text', () => {
    expect(blockedReasonLabel('timeout')).toBe('Timed out');
    expect(blockedReasonLabel('budget')).toBe('Over its budget');
    expect(blockedReasonLabel('crashed')).toBe('Crashed');
    expect(blockedReasonLabel('cancelled')).toBe('Cancelled');
    expect(blockedReasonLabel('scope_violation')).toBe('Scope violation');
    expect(blockedReasonLabel('not_run')).toBe('Not run');
    expect(blockedReasonLabel('no_spec')).toBe('No spec available');
    expect(blockedReasonLabel('exotic')).toBe('exotic');
  });

  const f = (file: string, line: number, axis?: string): StewardFinding => ({ file, line, why: 'w', check: 'c', axis });

  it('groupFindingsByAxisThenFile orders standards before spec', () => {
    const groups = groupFindingsByAxisThenFile([f('z.go', 1), f('b.go', 9, 'spec'), f('a.go', 5, 'standards'), f('a.go', 2, 'standards')]);
    expect(groups.map((g) => g.axis)).toEqual(['standards', 'spec', undefined]);
    expect(groups[0].files[0].findings.map((x) => x.line)).toEqual([2, 5]);
  });

  it('findingsAsPrompt is unchanged without an axis', () => {
    const r = review({ verdict: 'CHANGES', findings: [{ file: 'a.go', line: 3, why: 'bad', check: 'task test', severity: 'high', rule: 'ignored' }] });
    expect(findingsAsPrompt(r)).toBe('Steward review 5 of task 7 at abcdef1: Changes requested\n- a.go:3 [high] bad (check: task test)');
  });

  it('findingsAsPrompt tags a finding with its axis and rule', () => {
    const r = review({ findings: [{ file: 'a.go', line: 3, why: 'bad', check: 'task test', axis: 'standards', rule: 'no-raw-sql' }] });
    expect(findingsAsPrompt(r)).toContain('- a.go:3 [standards] bad (check: task test) rule: no-raw-sql');
  });

  it('unknown axis and status never throw', () => {
    expect(axisLabel('novel')).toBe('novel');
    expect(axisLabel('')).toBe('Axis');
    expect(axisLabel('spec')).toBe('Spec');
    expect(axisStatusLabel({ status: 'weird' })).toMatchObject({ text: 'weird', tone: 'neutral' });
    expect(axisStatusLabel({ status: 'done', seatVerdict: 'ODD' }).text).toBe('ODD');
    expect(() => groupFindingsByAxisThenFile([f('a', 1, 'novel')])).not.toThrow();
    expect(criteriaCounts([{ id: 'a', status: 'met', evidence: '' }, { id: 'b', status: 'odd', evidence: '' }, { id: 'c', status: 'unmet', evidence: '' }])).toEqual({
      met: 1,
      unmet: 1,
      unverifiable: 1,
    });
  });

  const done = (axis: string, v: string) => ({ axis, status: 'done', seatVerdict: v, costUsd: 0 });

  it('combinedSentence says in progress while a pass is still open', () => {
    expect(combinedSentence({ verdict: 'ACCEPTED', axes: [done('standards', 'ACCEPTED'), { axis: 'spec', status: 'running', costUsd: 0 }] })).toBe('Review in progress.');
  });

  it('combinedSentence never carries raw tags or skipped copy', () => {
    const skipped = { axis: 'spec', status: 'skipped', blockedReason: 'no_spec', costUsd: 0 };
    const sentences = [
      combinedSentence({ verdict: 'ACCEPTED', axes: [done('standards', 'ACCEPTED'), done('spec', 'ACCEPTED')] }),
      combinedSentence({ verdict: 'ACCEPTED', axes: [done('standards', 'ACCEPTED'), skipped] }),
      combinedSentence({ verdict: 'CHANGES', axes: [done('standards', 'CHANGES'), done('spec', 'ACCEPTED')] }),
      combinedSentence({ verdict: 'CHANGES', axes: [{ axis: 'standards', status: 'blocked', blockedReason: 'timeout', costUsd: 0 }, done('spec', 'ACCEPTED')] }),
      combinedSentence({ verdict: 'BLOCKED', axes: [] }),
      combinedSentence({ verdict: undefined, axes: [] }),
    ];
    expect(sentences).toEqual([
      'Both axes accepted the change.',
      'Standards accepted the change; spec had nothing to judge.',
      'Changes requested by Standards.',
      'Standards did not complete.',
      'The review was blocked.',
      'Review in progress.',
    ]);
    for (const s of sentences) {
      expect(s).not.toContain('[');
      expect(s).not.toContain('axis spec skipped');
    }
  });

  it('isPartialAxes only for not_run', () => {
    const axes = [done('standards', 'ACCEPTED'), { axis: 'spec', status: 'blocked', blockedReason: 'not_run', costUsd: 0 }];
    expect(combinedSentence({ verdict: 'CHANGES', partial: true, axes })).toBe('Partial review: only the standards axis ran, so it cannot be accepted.');
    expect(isPartialAxes(axes)).toBe(true);
    expect(isPartialAxes([{ status: 'blocked', reason: 'not_run' }])).toBe(true);
    const timedOut = [done('standards', 'ACCEPTED'), { axis: 'spec', status: 'blocked', blockedReason: 'timeout', costUsd: 0 }];
    expect(isPartialAxes(timedOut)).toBe(false);
    expect(isPartialAxes(undefined)).toBe(false);
  });

  it('reviewHeader carries partial and combined only with axes', () => {
    expect(reviewHeader(review({ verdict: 'CHANGES', summary: 's' }))).toMatchObject({ partial: false, combined: null, summary: 's' });
    const h = reviewHeader(review({ verdict: 'ACCEPTED', axes: [done('standards', 'ACCEPTED'), done('spec', 'ACCEPTED')] }));
    expect(h).toMatchObject({ partial: false, combined: 'Both axes accepted the change.' });
  });
});

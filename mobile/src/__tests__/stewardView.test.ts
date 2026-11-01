// stewardView.test.ts: the verdict chip never throws, gates sort failing-first, findings group per
// file, the copyable findings text, and the card header / frame source.

import { describe, expect, it } from 'vitest';
import { StewardFinding, StewardReview } from '../steward/types';
import {
  findingsAsPrompt,
  frameSource,
  gateRow,
  groupFindingsByFile,
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

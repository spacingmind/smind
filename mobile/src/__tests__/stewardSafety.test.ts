// stewardSafety.test.ts: the steward client is read-only -- it sends only steward.get and
// steward.frame, and a malformed answer degrades to empty lists.

import { describe, expect, it } from 'vitest';
import { fetchFrame, fetchReview, STEWARD_READ_METHODS } from '../steward/api';
import { fakeConnection } from './pluginFixtures';

describe('steward client is read-only', () => {
  it('steward client sends only read methods', async () => {
    const { conn, calls } = fakeConnection((method) => {
      if (method === 'steward.get') return { id: 5, taskId: 7, headSha: 'abcdef1234' };
      return { contentType: 'image/png', data: 'aGk=' };
    });
    const review = await fetchReview(conn, 5);
    const frame = await fetchFrame(conn, 5, 'a.png');
    expect(calls).toEqual([
      ['steward.get', { reviewId: 5 }],
      ['steward.frame', { reviewId: 5, name: 'a.png' }],
    ]);
    expect(calls.every(([m]) => (STEWARD_READ_METHODS as readonly string[]).includes(m))).toBe(true);
    expect(review.findings).toEqual([]);
    expect(review.gates).toEqual([]);
    expect(frame).toEqual({ contentType: 'image/png', data: 'aGk=' });
  });
});

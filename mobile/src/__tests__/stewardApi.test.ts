// stewardApi.test.ts: fetchReview keeps a single-axis review exactly as the daemon sent it (no `axes` key at all) and
// drops a malformed `axes` rather than keeping it.

import { describe, expect, it } from 'vitest';
import { RelayConnection } from '../relay/RelayConnection';
import { fetchReview } from '../steward/api';

const conn = (result: unknown) => ({ call: async () => result }) as unknown as RelayConnection;

describe('fetchReview axes', () => {
  it('a single-axis review has no axes key', async () => {
    const r = await fetchReview(conn({ id: 1, taskId: 2 }), 1);
    expect('axes' in r).toBe(false);
    expect(r.findings).toEqual([]);
    expect(r.gates).toEqual([]);
  });

  it('a malformed axes is dropped and a real array is kept', async () => {
    expect('axes' in (await fetchReview(conn({ id: 1, axes: 'nope' }), 1))).toBe(false);
    expect((await fetchReview(conn({ id: 1, axes: [{ axis: 'spec', status: 'done' }] }), 1)).axes).toEqual([{ axis: 'spec', status: 'done' }]);
  });

  it('unknown fields pass through untouched', async () => {
    const r = (await fetchReview(conn({ id: 1, brandNew: 7, partial: true }), 1)) as unknown as Record<string, unknown>;
    expect(r.brandNew).toBe(7);
    expect(r.partial).toBe(true);
  });
});

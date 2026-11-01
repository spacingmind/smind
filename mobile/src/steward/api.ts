// api.ts is the steward client's read surface: two reads and nothing that sends any other RPC
// (stewardSafety.test.ts). A malformed answer degrades to empty lists rather than throwing.

import { RelayConnection } from '../relay/RelayConnection';
import { StewardReview } from './types';

/** Every RPC this module may send. */
export const STEWARD_READ_METHODS = ['steward.get', 'steward.frame'] as const;

export async function fetchReview(conn: RelayConnection, reviewId: number): Promise<StewardReview> {
  const res = (await conn.call('steward.get', { reviewId })) as Partial<StewardReview> | null;
  const review: Partial<StewardReview> = res ?? {};
  return {
    ...review,
    findings: Array.isArray(review.findings) ? review.findings : [],
    gates: Array.isArray(review.gates) ? review.gates : [],
  } as StewardReview;
}

export async function fetchFrame(conn: RelayConnection, reviewId: number, name: string): Promise<{ contentType: string; data: string }> {
  return (await conn.call('steward.frame', { reviewId, name })) as { contentType: string; data: string };
}

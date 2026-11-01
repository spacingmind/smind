// ReviewRowChip is the chip an inbox review row carries: the verdict once the review is loaded, a
// neutral Review badge while loading or when the fetch fails (the row itself still opens the review).

import { RelayConnection } from '../../relay/RelayConnection';
import { useReview } from '../../steward/useReview';
import { Badge } from '../../ui/Badge';
import { VerdictChip } from '../../ui/VerdictChip';

export function ReviewRowChip({ conn, reviewId }: { conn: RelayConnection; reviewId: number }) {
  const { state } = useReview(conn, reviewId);
  if (state.kind === 'ready') return <VerdictChip verdict={state.review.verdict} />;
  return <Badge>Review</Badge>;
}

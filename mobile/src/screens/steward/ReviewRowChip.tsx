// ReviewRowChip is the chip an inbox review row carries: the verdict once the review is loaded, a
// neutral Review badge while loading or when the fetch fails (the row itself still opens the review).
// A partial review (a one-axis re-run, ADR-0054) also wears a Partial chip, from the row's own axes
// (`partial`) or from the loaded review, so the verdict beside it is not read as a full review's.

import { useMemo } from 'react';
import { StyleSheet, View } from 'react-native';
import { RelayConnection } from '../../relay/RelayConnection';
import { useReview } from '../../steward/useReview';
import { isPartialAxes } from '../../steward/view';
import { useAppTheme } from '../../theme/ThemeProvider';
import { PartialChip } from '../../ui/AxisChip';
import { Badge } from '../../ui/Badge';
import { VerdictChip } from '../../ui/VerdictChip';

export function ReviewRowChip({ conn, reviewId, partial = false }: { conn: RelayConnection; reviewId: number; partial?: boolean }) {
  const theme = useAppTheme();
  const styles = useMemo(() => StyleSheet.create({ row: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] } }), [theme]);
  const { state } = useReview(conn, reviewId);
  const ready = state.kind === 'ready';
  const isPartial = partial || (ready && (state.review.partial === true || isPartialAxes(state.review.axes)));
  const chip = ready ? <VerdictChip verdict={state.review.verdict} /> : <Badge>Review</Badge>;
  if (!isPartial) return chip;
  return (
    <View style={styles.row}>
      {chip}
      <PartialChip />
    </View>
  );
}

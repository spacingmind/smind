// QuestionMarker: the one-line trace a closed question leaves in the transcript (ADR-0055), the question twin of the
// resolved permission row. Plain <Text> only: what was asked and the human's own answer are agent- or human-authored
// strings and are never interpreted. The wording per outcome is questionView.ts's resolutionWords, so no outcome is
// told by colour alone.

import { memo, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import type { QuestionState } from '../../questionRequests';
import { answerSummary, questionSummary, resolutionNeedsAttention, resolutionWords } from '../../questionView';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { Card } from '../../ui/Card';

export const QuestionMarker = memo(function QuestionMarker({ state }: { state: QuestionState }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const reason = state.resolution?.reason;
  const answered = answerSummary(state.request, state.resolution?.answer);
  return (
    <Card tone="subtle" style={styles.card}>
      <Text style={styles.asked} selectable numberOfLines={3}>
        {questionSummary(state.request)}
      </Text>
      <Text style={[styles.outcome, resolutionNeedsAttention(reason) && styles.attention]} selectable>
        {resolutionWords(reason)}
        {answered !== '' ? `: ${answered}` : ''}
      </Text>
    </Card>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: { gap: theme.spacing[1], padding: theme.spacing[3] },
    asked: { ...theme.type.small, color: theme.foregroundSubtle },
    outcome: { ...theme.type.label, color: theme.foreground },
    attention: { color: theme.status.warning },
  });
}

// One run-history row: when, the decision chip, the reason, the outcome and the task it started. All daemon text is
// drawn through <Text>, never interpreted.

import { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { AuditRowView } from '../../automations/historyView';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { Badge } from '../../ui/Badge';

export function HistoryRow({ row }: { row: AuditRowView }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.card}>
      <View style={styles.top}>
        <Text style={styles.when}>{row.when}</Text>
        <Badge tone={row.tone}>{row.decision}</Badge>
      </View>
      {row.origin !== null && <Text style={styles.meta}>{row.origin}</Text>}
      {row.reason !== null && <Text style={styles.reason}>{row.reason}</Text>}
      {row.outcome !== null && <Text style={styles.meta}>{`Outcome: ${row.outcome}`}</Text>}
      {row.task !== null && <Text style={styles.meta}>{row.task}</Text>}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: {
      backgroundColor: theme.card,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      padding: theme.spacing[3],
      gap: theme.spacing[1],
    },
    top: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: theme.spacing[2] },
    when: { ...theme.type.label, color: theme.foreground, flexShrink: 1 },
    reason: { ...theme.type.small, color: theme.foreground },
    meta: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}

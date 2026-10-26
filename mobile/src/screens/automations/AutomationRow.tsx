// One automation in the list: name, status chip, the signal summary and last-fired lines, and the paused reason as a
// plain text line. Every string is daemon-provided and drawn through <Text>; nothing is interpreted. Tapping opens
// the read-only detail (run history and the Pause brake).

import { useMemo } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { AutomationRowView } from '../../automations/listView';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';
import { Badge } from '../../ui/Badge';

export function AutomationRow({ row, onPress }: { row: AutomationRowView; onPress: (id: number) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={`${row.name}, ${row.status}`} onPress={() => onPress(row.id)} style={styles.card}>
      <View style={styles.top}>
        <Text style={styles.name} numberOfLines={1}>
          {row.name}
        </Text>
        <Badge tone={row.tone}>{row.status}</Badge>
        <AppIcon name="chevronRight" size={16} color={theme.foregroundSubtlest} />
      </View>
      <Text style={styles.signal} numberOfLines={2}>
        {row.signal}
      </Text>
      <Text style={styles.fired}>{row.lastFired}</Text>
      {row.pausedReason !== null && <Text style={styles.warning}>{row.pausedReason}</Text>}
    </Pressable>
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
      gap: theme.spacing[2],
    },
    top: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2] },
    name: { ...theme.type.label, color: theme.foreground, flex: 1 },
    signal: { ...theme.type.small, color: theme.foregroundSubtle },
    fired: { ...theme.type.small, color: theme.foregroundSubtle },
    warning: { ...theme.type.small, color: theme.status.warning },
  });
}

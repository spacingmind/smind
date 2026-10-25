// One plugin in the list: name and version, the source and status chips, then the last error and the link warning as
// plain text lines. Every string is plugin-authored and drawn through <Text>; nothing is interpreted. Tapping opens
// the read-only detail.

import { useMemo } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { PluginRowView } from '../../plugins/listView';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';
import { Badge } from '../../ui/Badge';

export function PluginRow({ row, onPress }: { row: PluginRowView; onPress: (id: string) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={`${row.name}, ${row.status}`} onPress={() => onPress(row.id)} style={styles.card}>
      <View style={styles.top}>
        <View style={styles.titles}>
          <Text style={styles.name} numberOfLines={1}>
            {row.name}
          </Text>
          <Text style={styles.version} numberOfLines={1}>
            {row.version}
          </Text>
        </View>
        <AppIcon name="chevronRight" size={16} color={theme.foregroundSubtlest} />
      </View>
      <View style={styles.chips}>
        <Badge>{row.source}</Badge>
        <Badge tone={row.tone}>{row.status}</Badge>
      </View>
      {row.lastError !== null && <Text style={styles.error}>{row.lastError}</Text>}
      {row.linkWarning !== null && <Text style={styles.warning}>{row.linkWarning}</Text>}
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
    titles: { flex: 1, flexDirection: 'row', alignItems: 'baseline', gap: theme.spacing[2] },
    name: { ...theme.type.label, color: theme.foreground, flexShrink: 1 },
    version: { ...theme.type.small, color: theme.foregroundSubtle },
    chips: { flexDirection: 'row', gap: theme.spacing[2] },
    error: { ...theme.type.small, color: theme.status.danger },
    warning: { ...theme.type.small, color: theme.status.warning },
  });
}

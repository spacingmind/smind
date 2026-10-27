// One inbox item: a >= 44 pt single-column row. Tapping the row opens the task (for a permission, the task screen's
// own permission card with the full request); the "more" button opens the action sheet. No hover-only controls.
// Every string is daemon-provided and drawn through <Text>.

import { useMemo } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { InboxRowView } from '../../inbox/view';
import { AppTheme, withAlpha } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';
import { Badge } from '../../ui/Badge';

export function InboxRow({ row, onOpen, onMore }: { row: InboxRowView; onOpen: (row: InboxRowView) => void; onMore: (row: InboxRowView) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const waiting = row.tone === 'waiting';
  const warning = row.tone === 'warning';
  return (
    <View style={[styles.card, waiting && styles.waiting, warning && styles.warning]}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${row.label}${row.unseen ? ', new' : ''}: ${row.title}. ${row.summary}. ${row.time}`}
        onPress={() => onOpen(row)}
        style={styles.main}
      >
        <View style={styles.icon}>
          <AppIcon name={row.icon} size={18} color={waiting ? theme.waiting.foreground : warning ? theme.status.warning : theme.foregroundSubtle} />
        </View>
        <View style={styles.body}>
          <View style={styles.top}>
            <Text style={styles.title} numberOfLines={2}>
              {row.title}
            </Text>
            {row.unseen && <Badge tone={waiting ? 'waiting' : warning ? 'warning' : 'neutral'}>New</Badge>}
          </View>
          <Text style={styles.summary} numberOfLines={2}>
            {row.summary}
          </Text>
          <Text style={styles.time}>{`${row.label} · ${row.time}`}</Text>
        </View>
      </Pressable>
      <Pressable accessibilityRole="button" accessibilityLabel={`Actions for ${row.title}`} onPress={() => onMore(row)} style={styles.more} hitSlop={4}>
        <AppIcon name="more" size={20} color={theme.foregroundSubtle} />
      </Pressable>
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    card: {
      flexDirection: 'row',
      alignItems: 'stretch',
      minHeight: 44,
      backgroundColor: theme.card,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
    },
    waiting: { backgroundColor: theme.waiting.surface },
    warning: { backgroundColor: withAlpha(theme.statusDot.warning, 0.1), borderColor: withAlpha(theme.statusDot.warning, 0.35) },
    main: { flex: 1, flexDirection: 'row', gap: theme.spacing[3], minHeight: 44, padding: theme.spacing[3] },
    body: { flex: 1, gap: theme.spacing[1] },
    icon: { width: 24, alignItems: 'center' },
    top: { flexDirection: 'row', alignItems: 'flex-start', gap: theme.spacing[2] },
    title: { ...theme.type.label, color: theme.foreground, flexShrink: 1 },
    summary: { ...theme.type.small, color: theme.foregroundSubtle },
    time: { ...theme.type.small, color: theme.foregroundSubtle },
    more: { width: 44, minHeight: 44, alignItems: 'center', justifyContent: 'center' },
  });
}

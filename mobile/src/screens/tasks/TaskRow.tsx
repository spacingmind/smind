// TaskRow is one memoised task row of a grouped card list. Primitive props
// only (vercel-react-native-skills: memoised rows take primitives, the
// callback receives the id) so a re-render of the screen does not re-render
// unchanged rows. The grouped-card look (one card per section) is emulated
// per row via `position`: outer corners rounded, border only on the outer
// sides, and an inset hairline towards the next row (never on last/only).

import { memo, useMemo } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { StatusDot } from '../../ui/StatusDot';
import { Badge } from '../../ui/Badge';
import { TaskRowPosition } from '../../taskListItems';
import { taskRowView } from '../../taskRowView';

interface Props {
  id: number;
  title: string;
  status: string;
  attention: boolean;
  position: TaskRowPosition;
  onPress: (id: number) => void;
}

export const TaskRow = memo(function TaskRow({ id, title, status, attention, position, onPress }: Props) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme, position), [theme, position]);
  const view = taskRowView(status, attention);
  const wordColor = view.wordTone === 'danger' ? theme.status.danger : view.wordTone === 'warning' ? theme.status.warning : theme.foregroundSubtle;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${title}, ${view.word}${attention ? ', needs approval' : ''}`}
      accessibilityHint={attention ? 'Needs your approval (since you last checked)' : undefined}
      onPress={() => onPress(id)}
      style={({ pressed }) => [styles.row, pressed && styles.pressed]}
    >
      <View style={styles.slot}>
        {view.slot === 'spinner' ? <ActivityIndicator size="small" color={theme.status.warning} /> : <StatusDot tone={view.slot.dot} />}
      </View>
      <View style={styles.text}>
        <Text style={styles.title} numberOfLines={1}>
          {title}
        </Text>
        <Text style={[styles.meta, { color: wordColor }]}>{view.word}</Text>
      </View>
      {view.badge === 'waiting' ? <Badge tone="waiting">Needs approval</Badge> : null}
      {position !== 'last' && position !== 'only' ? <View style={styles.hairline} /> : null}
    </Pressable>
  );
});

function makeStyles(theme: AppTheme, position: TaskRowPosition) {
  const radius = theme.radius.xl;
  return StyleSheet.create({
    row: {
      minHeight: 56,
      flexDirection: 'row',
      alignItems: 'center',
      gap: theme.spacing[2],
      paddingHorizontal: theme.spacing[3],
      paddingVertical: theme.spacing[2.5],
      backgroundColor: theme.card,
      borderWidth: 1,
      borderColor: theme.border,
      borderCurve: 'continuous',
      borderTopLeftRadius: position === 'first' || position === 'only' ? radius : 0,
      borderTopRightRadius: position === 'first' || position === 'only' ? radius : 0,
      borderBottomLeftRadius: position === 'last' || position === 'only' ? radius : 0,
      borderBottomRightRadius: position === 'last' || position === 'only' ? radius : 0,
      borderTopWidth: position === 'first' || position === 'only' ? 1 : 0,
      borderBottomWidth: position === 'last' || position === 'only' ? 1 : 0,
    },
    pressed: { backgroundColor: theme.surfaceSubtle },
    // A fixed 16x16 slot so the spinner and dot occupy the same space (the slot never collapses).
    slot: { width: 16, height: 16, alignItems: 'center', justifyContent: 'center' },
    text: { flex: 1 },
    title: { ...theme.type.label, color: theme.foreground },
    meta: { ...theme.type.small, color: theme.foregroundSubtle },
    // Inset hairline towards the row below: starts after the 16px status slot + spacing[2] gap + padding.
    hairline: {
      position: 'absolute',
      left: theme.spacing[3] + 16 + theme.spacing[2],
      right: 0,
      bottom: 0,
      height: 1,
      backgroundColor: theme.border,
    },
  });
}

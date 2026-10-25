// TaskTreeRow is one memoised row of the read-only Subtasks tree. Primitive
// props only (the callback receives the id) so a 30 s refresh re-renders
// only rows whose text changed. Depth 0 is the open task itself: shown as
// "this task", not pressable. Look copied from tasks/TaskRow.tsx.

import { memo, useMemo } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { Badge } from '../../ui/Badge';
import { StatusDot, StatusDotTone } from '../../ui/StatusDot';
import type { StateTone } from '../../taskTree';

const INDENT = 12;

interface Props {
  id: number;
  title: string;
  depth: number;
  tierLabel: string | null;
  state: string;
  stateTone: StateTone;
  stateNote?: string;
  countdown: string | null;
  stalledNote: string | null;
  permissions: number;
  unread: boolean;
  cost: string;
  isRoot: boolean;
  onPress: (id: number, title: string) => void;
}

function dotTone(state: string, tone: StateTone): StatusDotTone {
  if (tone === 'danger') return 'danger';
  if (tone === 'warning') return 'warning';
  if (state === 'Done') return 'success';
  return 'idle';
}

export const TaskTreeRow = memo(function TaskTreeRow({
  id,
  title,
  depth,
  tierLabel,
  state,
  stateTone,
  stateNote,
  countdown,
  stalledNote,
  permissions,
  unread,
  cost,
  isRoot,
  onPress,
}: Props) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme, depth, isRoot), [theme, depth, isRoot]);
  const toneColor = stateTone === 'danger' ? theme.status.danger : stateTone === 'warning' || stalledNote ? theme.status.warning : theme.foregroundSubtle;
  const running = state === 'Running';
  const permLabel = `${permissions} ${permissions === 1 ? 'permission' : 'permissions'}`;
  const detail = [isRoot ? 'This task ·' : null, state, stateNote, countdown ? `· ${countdown}` : null, stalledNote ? `· ${stalledNote}` : null].filter(Boolean).join(' ');
  const shownTitle = title || `Task ${id}`;
  const body = (
    <>
      <View style={styles.slot}>{running ? <ActivityIndicator size="small" color={theme.status.warning} /> : <StatusDot tone={dotTone(state, stateTone)} />}</View>
      <View style={styles.text}>
        <View style={styles.titleLine}>
          <Text style={styles.title} numberOfLines={1}>
            {shownTitle}
          </Text>
          {tierLabel ? <Badge>{tierLabel}</Badge> : null}
        </View>
        <Text style={[styles.meta, { color: toneColor }]} numberOfLines={1}>
          {detail}
        </Text>
      </View>
      {permissions > 0 ? <Badge tone="waiting">{permLabel}</Badge> : null}
      {unread ? <StatusDot tone="unread" /> : null}
      <Text style={styles.cost} numberOfLines={1}>
        {cost}
      </Text>
    </>
  );
  if (isRoot) {
    return (
      <View accessible accessibilityLabel={`${shownTitle}, this task, ${state}`} style={styles.row}>
        {body}
      </View>
    );
  }
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${shownTitle}, ${state}${permissions > 0 ? `, ${permLabel}` : ''}${unread ? ', unread' : ''}`}
      accessibilityHint="Opens this subtask"
      onPress={() => onPress(id, title)}
      style={({ pressed }) => [styles.row, pressed && styles.pressed]}
    >
      {body}
    </Pressable>
  );
});

function makeStyles(theme: AppTheme, depth: number, isRoot: boolean) {
  return StyleSheet.create({
    row: {
      minHeight: 48,
      flexDirection: 'row',
      alignItems: 'center',
      gap: theme.spacing[2],
      paddingRight: theme.spacing[3],
      paddingLeft: theme.spacing[3] + depth * INDENT,
      paddingVertical: theme.spacing[2],
      backgroundColor: isRoot ? theme.surfaceSubtle : theme.card,
    },
    pressed: { backgroundColor: theme.surfaceSubtle },
    slot: { width: 16, height: 16, alignItems: 'center', justifyContent: 'center' },
    text: { flex: 1 },
    titleLine: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2] },
    title: { ...theme.type.label, color: theme.foreground, flexShrink: 1 },
    meta: { ...theme.type.small, color: theme.foregroundSubtle },
    cost: { ...theme.type.mono, color: theme.foregroundSubtle },
  });
}

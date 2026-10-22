// ToolCallRow: one collapsed-by-default tool-call row -- status slot (spinner
// while running, a StatusDot otherwise), tool name and title in mono; tapping
// expands a long title. The expanded set lives in the screen; the row takes
// the id and a stable onToggle(id) (vercel-react-native-skills: pass ids, not closures).

import { memo, useCallback, useMemo } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { StatusDot, StatusDotTone } from '../../ui/StatusDot';

function toneFor(status: string): StatusDotTone {
  if (status === 'success') return 'success';
  if (status === 'failure') return 'danger';
  return 'idle';
}

export const ToolCallRow = memo(function ToolCallRow({
  id,
  toolName,
  title,
  status,
  expanded,
  onToggle,
}: {
  id: string;
  toolName: string;
  title: string;
  status: string;
  expanded: boolean;
  onToggle: (id: string) => void;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const toggle = useCallback(() => onToggle(id), [id, onToggle]);
  return (
    <Pressable style={styles.row} onPress={toggle} accessibilityRole="button" accessibilityState={{ expanded }}>
      <View style={styles.slot}>
        {status === 'running' ? <ActivityIndicator size="small" color={theme.foregroundSubtle} /> : <StatusDot tone={toneFor(status)} />}
      </View>
      <Text style={styles.name}>{toolName}</Text>
      {title.length > 0 ? (
        <Text style={styles.title} numberOfLines={expanded ? undefined : 1} selectable>
          {title}
        </Text>
      ) : null}
    </Pressable>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: theme.spacing[2],
      minHeight: 44,
      backgroundColor: theme.surfaceSubtle,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.md,
      borderCurve: 'continuous',
      paddingVertical: theme.spacing[2],
      paddingHorizontal: theme.spacing[2.5],
    },
    slot: { width: 16, alignItems: 'center', justifyContent: 'center' },
    name: { ...theme.type.mono, color: theme.foreground },
    title: { ...theme.type.mono, color: theme.foregroundSubtle, flex: 1 },
  });
}

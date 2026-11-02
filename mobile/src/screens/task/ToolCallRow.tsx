// ToolCallRow: one collapsed-by-default tool-call row -- status slot (spinner
// while running, a StatusDot otherwise), tool name and title in mono; tapping
// expands a long title. The expanded set lives in the screen; the row takes
// the id and a stable onToggle(id) (vercel-react-native-skills: pass ids, not closures).
// Heredoc reveals (when the daemon elided bodies) render UNDER the row,
// outside its Pressable: the row's tap stays expand/collapse, the reveal
// toggles are their own buttons.

import { memo, useCallback, useMemo } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';
import { HeredocReveal } from '../../api';
import type { HeredocRef } from '../../runTimeline';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { StatusDot, StatusDotTone } from '../../ui/StatusDot';
import { HeredocReveal as HeredocRevealToggles } from './HeredocReveal';

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
  heredocs,
  runId,
  toolCallId,
  onRevealTool,
}: {
  id: string;
  toolName: string;
  title: string;
  status: string;
  expanded: boolean;
  onToggle: (id: string) => void;
  heredocs?: HeredocRef[];
  runId?: string;
  toolCallId?: string;
  onRevealTool?: (runId: string, toolCallId: string, index: number) => Promise<HeredocReveal>;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const toggle = useCallback(() => onToggle(id), [id, onToggle]);
  return (
    <View style={styles.outer}>
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
      {expanded && heredocs && heredocs.length > 0 && runId && toolCallId && onRevealTool ? (
        <View style={styles.reveal}>
          <HeredocRevealToggles refs={heredocs} reveal={(index) => onRevealTool(runId, toolCallId, index)} />
        </View>
      ) : null}
    </View>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    outer: { gap: theme.spacing[1.5] },
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
    reveal: { paddingLeft: theme.spacing[2.5] },
  });
}

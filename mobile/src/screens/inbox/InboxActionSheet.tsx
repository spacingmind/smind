// The action sheet of one inbox row (the ConnectionSheet pattern: @expo/ui BottomSheet, plain RN rows >= 44 pt).
// Actions come from the row model: a permission row never lists Snooze or Resolve (it can only be answered, on the
// task screen). Snooze opens the 1 h / 4 h / tomorrow choice, every one capped under 24 h.

import { useEffect, useMemo, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { BottomSheet } from '@expo/ui';
import { InboxRowView, snoozeOptions } from '../../inbox/view';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { UiHost } from '../../ui/UiHost';

export interface InboxActionHandlers {
  onSeen: (row: InboxRowView) => void;
  onSnooze: (row: InboxRowView, until: string) => void;
  onResolve: (row: InboxRowView) => void;
  onOpen: (row: InboxRowView) => void;
  onOpenHosts: (row: InboxRowView) => void;
}

export function InboxActionSheet({ row, onClose, handlers }: { row: InboxRowView | null; onClose: () => void; handlers: InboxActionHandlers }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [choosingSnooze, setChoosingSnooze] = useState(false);
  useEffect(() => setChoosingSnooze(false), [row?.key]);

  const item = (label: string, onPress: () => void, danger = false) => (
    <Pressable key={label} accessibilityRole="button" accessibilityLabel={label} onPress={onPress} style={styles.row}>
      <Text style={[styles.label, danger && styles.danger]}>{label}</Text>
    </Pressable>
  );
  const done = (fn: () => void) => () => {
    onClose();
    fn();
  };

  return (
    <UiHost>
      <BottomSheet isPresented={row !== null} onDismiss={onClose} snapPoints={['half']} containerColor={theme.overlay}>
        <View style={styles.content}>
          {row && (
            <>
              <Text style={styles.heading} numberOfLines={2}>
                {row.title}
              </Text>
              {!choosingSnooze && row.actions.includes('open') && item('Open task', done(() => handlers.onOpen(row)))}
              {!choosingSnooze && row.actions.includes('hosts') && item('Review hosts', done(() => handlers.onOpenHosts(row)))}
              {!choosingSnooze && row.actions.includes('seen') && item('Mark seen', done(() => handlers.onSeen(row)))}
              {!choosingSnooze && row.actions.includes('snooze') && item('Snooze…', () => setChoosingSnooze(true))}
              {!choosingSnooze && row.actions.includes('resolve') && item('Resolve', done(() => handlers.onResolve(row)))}
              {choosingSnooze && row.actions.includes('snooze') && snoozeOptions(new Date()).map((o) => item(o.label, done(() => handlers.onSnooze(row, o.until))))}
              {choosingSnooze && item('Back', () => setChoosingSnooze(false))}
            </>
          )}
        </View>
      </BottomSheet>
    </UiHost>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    content: { paddingBottom: theme.spacing[4] },
    heading: { ...theme.type.label, color: theme.foregroundSubtle, paddingHorizontal: theme.spacing[4], paddingBottom: theme.spacing[2] },
    row: { minHeight: 44, justifyContent: 'center', paddingHorizontal: theme.spacing[4], borderTopWidth: 1, borderTopColor: theme.border },
    label: { ...theme.type.body, color: theme.foreground },
    danger: { color: theme.status.danger },
  });
}

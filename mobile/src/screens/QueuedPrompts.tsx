// QueuedPrompts renders the daemon-side prompt queue's rows (ADR-0021 §9),
// the mobile counterpart of web's composer queued-prompts.tsx: a status word
// (queueRows.ts), the prompt itself, and per-row Cancel -- or the auto-cancel
// reason when the daemon gave up on the prompt. Colors come from the token
// system only.

import { useMemo } from 'react';
import { StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { ChatQueueItem } from '../api';
import { cancelAccessibilityLabel, queueRowLabel } from '../queueRows';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';

export function QueuedPrompts({ items, onCancel }: { items: ChatQueueItem[]; onCancel: (itemId: number) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  if (items.length === 0) return null;
  return (
    <View accessibilityLabel="Queued prompts">
      {items.map((item) => {
        const label = queueRowLabel(item);
        const cancelled = label === 'NOT SENT';
        return (
          <View key={item.ID} style={styles.row}>
            <Text style={[styles.label, cancelled && styles.labelDanger]}>{label}</Text>
            <Text style={styles.prompt} numberOfLines={1}>
              {item.Prompt}
            </Text>
            {cancelled ? (
              <Text style={styles.reason}>{item.CancelReason}</Text>
            ) : (
              <TouchableOpacity
                accessibilityRole="button"
                accessibilityLabel={cancelAccessibilityLabel(item)}
                hitSlop={{ top: 14, bottom: 14, left: 10, right: 10 }}
                onPress={() => onCancel(item.ID)}
              >
                <Text style={styles.cancel}>Cancel</Text>
              </TouchableOpacity>
            )}
          </View>
        );
      })}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: theme.spacing[2],
      backgroundColor: theme.surface[2],
      borderRadius: theme.radius.sm,
      paddingVertical: theme.spacing[1],
      paddingHorizontal: theme.spacing[2],
      marginBottom: theme.spacing[1],
    },
    label: {
      fontSize: theme.type.metadataLabel.fontSize,
      lineHeight: theme.type.metadataLabel.lineHeight,
      fontWeight: theme.type.metadataLabel.fontWeight,
      color: theme.foregroundMuted,
    },
    labelDanger: { color: theme.status.danger },
    prompt: {
      flex: 1,
      fontSize: theme.type.interface.fontSize,
      lineHeight: theme.type.interface.lineHeight,
      color: theme.foregroundMuted,
    },
    reason: {
      flexShrink: 1, // a long reason must not crush the prompt column
      fontSize: theme.type.metadataLabel.fontSize,
      lineHeight: theme.type.metadataLabel.lineHeight,
      fontWeight: theme.type.metadataLabel.fontWeight,
      color: theme.status.danger,
    },
    cancel: {
      fontSize: theme.type.metadataLabel.fontSize,
      lineHeight: theme.type.metadataLabel.lineHeight,
      fontWeight: theme.type.metadataLabel.fontWeight,
      color: theme.primary,
    },
  });
}

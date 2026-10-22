// QueuedPrompts renders the daemon-side prompt queue's rows (ADR-0021 §9),
// the mobile counterpart of web's composer queued-prompts.tsx: a floating
// card above the composer with one chip per prompt -- a status word
// (queueRows.ts), the prompt itself, and per-row Cancel -- or the auto-cancel
// reason when the daemon gave up on the prompt. Chips fade in/out with
// Reanimated (expo-animation: entering/exiting on Animated.View, <= 8px
// travel, none under reduced motion). Colors come from the token system only.

import { useMemo } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import Animated, { FadeIn, FadeOut, useReducedMotion } from 'react-native-reanimated';
import { ChatQueueItem } from '../api';
import { cancelAccessibilityLabel, queueRowLabel } from '../queueRows';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';

export function QueuedPrompts({ items, onCancel }: { items: ChatQueueItem[]; onCancel: (itemId: number) => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const reduceMotion = useReducedMotion();
  if (items.length === 0) return null;
  const entering = reduceMotion ? undefined : FadeIn.duration(theme.duration.hover);
  const exiting = reduceMotion ? undefined : FadeOut.duration(theme.duration.hover);
  return (
    <View accessibilityLabel="Queued prompts" style={styles.strip}>
      {items.map((item) => {
        const label = queueRowLabel(item);
        const cancelled = label === 'NOT SENT';
        return (
          <Animated.View key={item.ID} entering={entering} exiting={exiting} style={styles.row}>
            <Text style={[styles.label, cancelled && styles.labelDanger]}>{label}</Text>
            <Text style={styles.prompt} numberOfLines={1}>
              {item.Prompt}
            </Text>
            {cancelled ? (
              <Text style={styles.reason}>{item.CancelReason}</Text>
            ) : (
              <Pressable
                accessibilityRole="button"
                accessibilityLabel={cancelAccessibilityLabel(item)}
                hitSlop={{ top: 12, bottom: 12, left: 10, right: 10 }}
                onPress={() => onCancel(item.ID)}
                style={styles.cancelHit}
              >
                <Text style={styles.cancel}>Cancel</Text>
              </Pressable>
            )}
          </Animated.View>
        );
      })}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    strip: {
      backgroundColor: theme.overlay,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius['2xl'],
      borderCurve: 'continuous',
      boxShadow: theme.boxShadow.overlay,
      padding: theme.spacing[2],
      marginBottom: theme.spacing[2],
    },
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: theme.spacing[2],
      minHeight: 44,
      paddingHorizontal: theme.spacing[1],
    },
    cancelHit: { minHeight: 44, justifyContent: 'center' },
    label: {
      fontSize: theme.type.micro.fontSize,
      lineHeight: theme.type.micro.lineHeight,
      fontWeight: theme.type.micro.fontWeight,
      color: theme.foregroundSubtle,
    },
    labelDanger: { color: theme.status.danger },
    prompt: {
      flex: 1,
      fontSize: theme.type.small.fontSize,
      lineHeight: theme.type.small.lineHeight,
      color: theme.foreground,
    },
    reason: {
      flexShrink: 1, // a long reason must not crush the prompt column
      fontSize: theme.type.small.fontSize,
      lineHeight: theme.type.small.lineHeight,
      color: theme.status.danger,
    },
    cancel: {
      fontSize: theme.type.small.fontSize,
      lineHeight: theme.type.small.lineHeight,
      color: theme.foreground,
    },
  });
}

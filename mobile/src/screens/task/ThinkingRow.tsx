// ThinkingRow: a collapsed-by-default "Thinking…" row (Pressable, min height
// 44) that expands to the thinking text. The expanded state is local: it is
// UI-only and survives nothing but the row's own mount.

import { memo, useCallback, useMemo, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';

export const ThinkingRow = memo(function ThinkingRow({ text }: { text: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [expanded, setExpanded] = useState(false);
  const toggle = useCallback(() => setExpanded((e) => !e), []);
  return (
    <View>
      <Pressable style={styles.row} onPress={toggle} accessibilityRole="button" accessibilityState={{ expanded }} accessibilityLabel="Thinking">
        <Text style={styles.label}>Thinking…</Text>
        <View style={expanded ? styles.chevronOpen : null}>
          <AppIcon name="chevronRight" size={14} color={theme.foregroundSubtle} />
        </View>
      </Pressable>
      {expanded ? (
        <Text style={styles.body} selectable>
          {text}
        </Text>
      ) : null}
    </View>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    row: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[1.5], minHeight: 44 },
    label: { ...theme.type.small, color: theme.foregroundSubtle },
    chevronOpen: { transform: [{ rotate: '90deg' }] },
    body: { ...theme.type.small, color: theme.foregroundSubtle, paddingBottom: theme.spacing[2] },
  });
}

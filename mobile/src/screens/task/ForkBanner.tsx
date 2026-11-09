// ForkBanner: the strip above a forked chat's transcript (ADR-0057 PR 1b
// AC20) naming the chat it came from, with the "View original" action
// when the parent is viewable (an archived or missing parent has none --
// the screen omits the handler). HistorySourceNote's recipe
// (surfaceSubtle, 6px status dot, type.small); every wording decision is
// chatFork.ts's forkBannerText, this only lays it out.

import { memo, useMemo } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';

export const ForkBanner = memo(function ForkBanner({ text, onViewOriginal }: { text: string; onViewOriginal?: () => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.strip}>
      <View style={[styles.dot, { backgroundColor: theme.foregroundSubtlest }]} />
      <View style={styles.body}>
        <Text style={styles.text} accessibilityRole="text" selectable>
          {text}
        </Text>
        {onViewOriginal !== undefined ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="View original"
            onPress={onViewOriginal}
            style={({ pressed }) => [styles.open, pressed && styles.pressed]}
          >
            <Text style={styles.openLabel}>View original</Text>
          </Pressable>
        ) : null}
      </View>
    </View>
  );
});

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    strip: {
      flexDirection: 'row',
      alignItems: 'flex-start',
      gap: theme.spacing[2],
      backgroundColor: theme.surfaceSubtle,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.lg,
      borderCurve: 'continuous',
      padding: theme.spacing[3],
    },
    // Aligns with the first line of type.small text (lineHeight 16 vs the 6px dot).
    dot: { width: 6, height: 6, borderRadius: theme.radius.full, marginTop: 5 },
    body: { flexShrink: 1, gap: theme.spacing[1] },
    text: { ...theme.type.small, color: theme.foregroundSubtle, flexShrink: 1 },
    open: { alignSelf: 'flex-start', minHeight: 44, flexDirection: 'row', alignItems: 'center' },
    pressed: { opacity: 0.6 },
    openLabel: { ...theme.type.label, color: theme.foreground },
  });
}

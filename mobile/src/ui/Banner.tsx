// Banner.tsx is the full-width status strip primitive (expo-design-system:
// a nameable role, an API smaller than its implementation): ReconnectingBanner
// moved here from ConnectionStatusCard.tsx. Mount/unmount animate via
// Reanimated entering/exiting -- FadeIn/FadeOut at duration.hover, skipped
// entirely when the user prefers reduced motion (expo-animation: respect
// useReducedMotion). Tokens only, never a hex literal.

import { ReactNode, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { FadeIn, FadeOut, useReducedMotion } from 'react-native-reanimated';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';

export type BannerTone = 'neutral' | 'warning';

export function Banner({ children, tone = 'neutral' }: { children: ReactNode; tone?: BannerTone }) {
  const theme = useAppTheme();
  const reduceMotion = useReducedMotion();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const dot = tone === 'warning' ? theme.statusDot.warning : theme.foregroundSubtlest;
  const motion = reduceMotion
    ? {}
    : { entering: FadeIn.duration(theme.duration.hover), exiting: FadeOut.duration(theme.duration.hover) };
  return (
    <View style={styles.container} accessibilityRole="alert" accessibilityLiveRegion="polite" {...motion}>
      <View style={[styles.dot, { backgroundColor: dot }]} />
      <Text style={styles.text}>{children}</Text>
    </View>
  );
}

/** A thin warning strip over a live session while the connection recovers. */
export function ReconnectingBanner({ children }: { children: ReactNode }) {
  return <Banner tone="warning">{children}</Banner>;
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    container: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: theme.spacing[2],
      backgroundColor: theme.surfaceSubtle,
      borderBottomWidth: 1,
      borderBottomColor: theme.border,
      paddingVertical: theme.spacing[2],
      paddingHorizontal: theme.spacing[4],
    },
    dot: { width: 6, height: 6, borderRadius: theme.radius.full },
    text: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}

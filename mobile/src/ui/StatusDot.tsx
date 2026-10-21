// StatusDot.tsx is the small filled-circle status indicator. Color-only by
// design: it always sits next to text (a Badge or a label) that carries the
// same status in words, so the signal never rests on color alone. Uses the
// fill-tier statusDot tokens (higher chroma than the text tier).

import { useMemo } from 'react';
import { StyleSheet, View } from 'react-native';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';

export type StatusDotTone = 'success' | 'danger' | 'warning' | 'running' | 'idle' | 'unread';

export function StatusDot({ tone = 'success', size = 8 }: { tone?: StatusDotTone; size?: number }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const backgroundColor =
    tone === 'unread' ? theme.unread : tone === 'idle' ? theme.border : theme.statusDot[tone];
  return <View style={[styles.dot, { width: size, height: size, backgroundColor }]} />;
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    dot: { borderRadius: theme.radius.full },
  });
}

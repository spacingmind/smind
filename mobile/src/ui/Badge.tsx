// Badge.tsx is the pill status-label primitive. The one component that owns
// its text (like a @expo/ui Button label): children is a string rendered in
// an inner Text, so callers can't restyle the label per site. Tinted
// backgrounds for status tones come from withAlpha on the fill-tier
// statusDot tokens; text uses the AA text-tier status tokens.

import { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { AppTheme, withAlpha } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';

export type BadgeTone = 'neutral' | 'waiting' | 'success' | 'destructive' | 'warning';

export function Badge({ children, tone = 'neutral' }: { children: string; tone?: BadgeTone }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const colors = useMemo(() => toneColors(theme, tone), [theme, tone]);
  return (
    <View style={[styles.pill, { backgroundColor: colors.background }]}>
      <Text style={[styles.label, { color: colors.foreground }]} numberOfLines={1}>
        {children}
      </Text>
    </View>
  );
}

function toneColors(theme: AppTheme, tone: BadgeTone): { background: string; foreground: string } {
  switch (tone) {
    case 'waiting':
      return { background: theme.waiting.surface, foreground: theme.waiting.foreground };
    case 'success':
    case 'destructive':
    case 'warning': {
      const dot = tone === 'success' ? theme.statusDot.success : tone === 'destructive' ? theme.statusDot.danger : theme.statusDot.warning;
      const text = tone === 'success' ? theme.status.success : tone === 'destructive' ? theme.status.danger : theme.status.warning;
      return { background: withAlpha(dot, 0.14), foreground: text };
    }
    default:
      return { background: theme.secondary, foreground: theme.foreground };
  }
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    pill: {
      borderRadius: theme.radius.full,
      height: 20,
      paddingHorizontal: theme.spacing[2],
      alignItems: 'center',
      justifyContent: 'center',
    },
    label: { ...theme.type.micro },
  });
}

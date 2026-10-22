// EmptySection renders a section's "No tasks" line in the same grouped-card
// shape the section's rows would occupy, so an empty section reads as a card.

import { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useAppTheme } from '../../theme/ThemeProvider';

export function EmptySection({ text }: { text: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.card}>
      <Text style={styles.text}>{text}</Text>
    </View>
  );
}

function makeStyles(theme: ReturnType<typeof useAppTheme>) {
  return StyleSheet.create({
    card: {
      backgroundColor: theme.card,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      padding: theme.spacing[3],
      minHeight: 56,
      justifyContent: 'center',
    },
    text: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}

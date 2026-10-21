// EmptyState.tsx is the centered "nothing here yet" primitive: a label-tier
// title, a small selectable body (selectable so a user can copy an ID or
// command the empty state explains), and optional children below for the
// action that fills the emptiness (expo-design-system: the action is a
// caller-supplied child, not a prop).

import { ReactNode, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';

export function EmptyState({ title, body, children }: { title: string; body: string; children?: ReactNode }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.container}>
      <Text style={styles.title}>{title}</Text>
      <Text style={styles.body} selectable>
        {body}
      </Text>
      {children ? <View style={styles.actions}>{children}</View> : null}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    container: {
      alignItems: 'center',
      padding: theme.spacing[8],
      gap: theme.spacing[4],
    },
    title: { ...theme.type.label, color: theme.foreground },
    body: { ...theme.type.small, color: theme.foregroundSubtle, textAlign: 'center' },
    actions: { alignItems: 'center' },
  });
}

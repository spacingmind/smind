// TaskListSkeleton is the loading placeholder: three rows in the grouped-card
// shape (one card, two inset separators), so loading never shows an empty page.

import { useMemo } from 'react';
import { StyleSheet, View } from 'react-native';
import { useAppTheme } from '../../theme/ThemeProvider';
import { Skeleton } from '../../ui/Skeleton';

export function TaskListSkeleton() {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.card}>
      {[0, 1, 2].map((i) => (
        <View key={i} style={[styles.row, i < 2 && styles.separated]}>
          <Skeleton width={16} height={16} radius="full" />
          <View style={styles.lines}>
            <Skeleton width="60%" height={14} radius="sm" />
            <Skeleton width="30%" height={10} radius="sm" />
          </View>
        </View>
      ))}
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
    },
    row: {
      minHeight: 56,
      flexDirection: 'row',
      alignItems: 'center',
      gap: theme.spacing[2],
      paddingHorizontal: theme.spacing[3],
      paddingVertical: theme.spacing[2.5],
    },
    separated: {
      borderBottomWidth: 1,
      borderBottomColor: theme.border,
    },
    lines: { flex: 1, gap: theme.spacing[1] },
  });
}

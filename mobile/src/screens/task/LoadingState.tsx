// LoadingState: what the task screen shows while its runs load -- the RunStrip
// skeleton plus three transcript-shaped blocks (a short right-aligned bubble,
// then two assistant text lines), so no frame of the push transition is an
// empty page.

import { useMemo } from 'react';
import { StyleSheet, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { Skeleton } from '../../ui/Skeleton';
import { RunStripSkeleton } from './RunStrip';

export function LoadingState() {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View accessibilityLabel="Loading task" style={styles.screen}>
      <RunStripSkeleton />
      <View style={styles.body}>
        <Skeleton width="45%" height={40} radius="lg" style={styles.bubble} />
        <Skeleton height={14} />
        <Skeleton width="70%" height={14} />
      </View>
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    screen: { flex: 1 },
    body: { padding: theme.spacing[4], gap: theme.spacing[3] },
    bubble: { alignSelf: 'flex-end' },
  });
}

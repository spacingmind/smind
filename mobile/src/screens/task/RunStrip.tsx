// RunStrip: one row under the header with the latest run's identity --
// `run <first 8 of ID> · <provider> · <live|status>` (the smoke matches this
// text) -- led by a StatusDot. While the run is loading a Skeleton takes the
// text's place.

import { memo, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { Skeleton } from '../../ui/Skeleton';
import { StatusDot, StatusDotTone } from '../../ui/StatusDot';

function toneFor(status: string): StatusDotTone {
  if (status === 'running') return 'running';
  if (status === 'done') return 'success';
  if (status === 'error' || status === 'stopped' || status === 'interrupted') return 'danger';
  return 'idle';
}

export const RunStrip = memo(function RunStrip({ runId, provider, status }: { runId: string; provider: string; status: string }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.strip}>
      <StatusDot tone={toneFor(status)} />
      <Text style={styles.text} selectable>
        run {runId.slice(0, 8)} · {provider} · {status === 'running' ? 'live' : status}
      </Text>
    </View>
  );
});

export function RunStripSkeleton() {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  return (
    <View style={styles.strip}>
      <Skeleton width="55%" height={12} />
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    strip: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2], paddingHorizontal: theme.spacing[4], paddingVertical: theme.spacing[2] },
    text: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}

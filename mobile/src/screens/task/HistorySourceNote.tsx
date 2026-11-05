// HistorySourceNote: the strip above a finished run's transcript saying
// where the shown history came from (ADR-0053's skeleton/rebuilt
// contract) and, when the daemon says the full history can be had, the
// Load full history action. Banner.tsx's recipe (surfaceSubtle, 6px
// status dot, type.small) as a compact in-list strip; every decision --
// wording, tone, which of the three states renders -- is historySource's
// pure logic, this only lays it out.

import { memo, useMemo } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { historyNoteView, HistorySourceInfo } from '../../historySource';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { AppIcon } from '../../ui/AppIcon';

export const HistorySourceNote = memo(function HistorySourceNote({ info, loading, onLoad }: { info: HistorySourceInfo; loading: boolean; onLoad: () => void }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const view = historyNoteView(info, loading);
  if (view === null) return null;
  const dot = view.tone === 'warning' ? theme.statusDot.warning : theme.foregroundSubtlest;
  return (
    <View style={styles.strip} accessibilityLiveRegion="polite">
      <View style={[styles.dot, { backgroundColor: dot }]} />
      <View style={styles.body}>
        <Text style={styles.message} accessibilityRole="text" selectable>
          {view.message}
        </Text>
        {view.showButton ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Load full history"
            onPress={onLoad}
            style={({ pressed }) => [styles.load, pressed && styles.pressed]}
          >
            <AppIcon name="history" size={theme.type.label.fontSize} color={theme.foreground} />
            <Text style={styles.loadLabel}>Load full history</Text>
          </Pressable>
        ) : null}
        {view.showLoading ? (
          <Text style={styles.loading} accessibilityRole="text">
            Loading full history…
          </Text>
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
    message: { ...theme.type.small, color: theme.foregroundSubtle, flexShrink: 1 },
    load: { alignSelf: 'flex-start', minHeight: 44, flexDirection: 'row', alignItems: 'center', gap: theme.spacing[1] },
    pressed: { opacity: 0.6 },
    loadLabel: { ...theme.type.label, color: theme.foreground },
    loading: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}

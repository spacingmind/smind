// HeredocReveal: the "Show heredoc (N bytes)" toggles under a permission
// card's policy line and an expanded tool-call row (permission-heredoc-
// reveal plan design 7). One row per elided marker; the body arrives
// already redacted from the daemon through the caller's reveal()
// (permission.revealHeredoc or run.revealToolHeredoc) and is fetched at
// most once -- heredocReveal.ts's pure state machine owns that, this file
// only renders it. The body wraps (no fixed width) and is selectable; the
// daemon's redaction/truncation notes travel with it.

import { useCallback, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';
import type { HeredocReveal as RevealBody } from '../../api';
import type { HeredocRef } from '../../runTimeline';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { initialRevealState, heredocToggleLabel, nextOnToggle, onFailed, onLoaded, onRetry, RevealState } from '../../transcript/heredocReveal';

export function HeredocReveal({ refs, reveal }: { refs: readonly HeredocRef[]; reveal: (index: number) => Promise<RevealBody> }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  if (refs.length === 0) return null;
  return (
    <View style={styles.rows}>
      {refs.map((ref) => (
        <HeredocRow key={ref.index} heredoc={ref} reveal={reveal} />
      ))}
    </View>
  );
}

function HeredocRow({ heredoc, reveal }: { heredoc: HeredocRef; reveal: (index: number) => Promise<RevealBody> }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [state, setState] = useState<RevealState>(initialRevealState);
  // The toggle and the fetch callbacks both read the latest state without
  // re-subscribing: a ref mirror, always written through `apply`.
  const stateRef = useRef(state);
  const apply = useCallback((next: RevealState) => {
    stateRef.current = next;
    setState(next);
  }, []);
  const fetch = useCallback(async () => {
    try {
      const body = await reveal(heredoc.index);
      apply(onLoaded(stateRef.current, body));
    } catch (e) {
      apply(onFailed(stateRef.current, e instanceof Error ? e.message : String(e)));
    }
  }, [apply, heredoc.index, reveal]);
  const onToggle = useCallback(() => {
    const next = nextOnToggle(stateRef.current);
    apply(next.state);
    if (next.fetch) void fetch();
  }, [apply, fetch]);
  const onRetryPress = useCallback(() => {
    apply(onRetry(stateRef.current));
    void fetch();
  }, [apply, fetch]);
  return (
    <View style={styles.column}>
      <Pressable style={styles.toggle} onPress={onToggle} accessibilityRole="button" accessibilityState={{ expanded: state.open }}>
        <Text style={styles.toggleLabel}>{heredocToggleLabel(heredoc.bytes, state.open)}</Text>
        {state.status === 'loading' ? <ActivityIndicator size="small" color={theme.foregroundSubtle} /> : null}
      </Pressable>
      {state.open && state.status === 'error' ? (
        <View style={styles.errorRow}>
          <Text style={styles.errorText} selectable>
            {state.error}
          </Text>
          <Pressable style={styles.retry} onPress={onRetryPress} accessibilityRole="button" hitSlop={8}>
            <Text style={styles.retryLabel}>Retry</Text>
          </Pressable>
        </View>
      ) : null}
      {state.open && state.body ? (
        <View style={styles.bodyBox}>
          <Text style={styles.bodyText} selectable>
            {state.body.text}
          </Text>
          {state.body.redacted ? <Text style={styles.note}>secrets redacted</Text> : null}
          {state.body.truncated ? <Text style={styles.note}>truncated ({state.body.bytes} bytes in total)</Text> : null}
        </View>
      ) : null}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    rows: { gap: theme.spacing[1.5] },
    column: { gap: theme.spacing[1.5] },
    toggle: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[1.5], minHeight: 44, justifyContent: 'flex-start' },
    toggleLabel: { ...theme.type.small, color: theme.foregroundSubtle, textDecorationLine: 'underline' },
    errorRow: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
    errorText: { ...theme.type.small, color: theme.status.danger, flexShrink: 1 },
    retry: { minHeight: 44, justifyContent: 'center' },
    retryLabel: { ...theme.type.small, color: theme.foregroundSubtle, textDecorationLine: 'underline' },
    bodyBox: {
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      backgroundColor: theme.surfaceSubtle,
      padding: theme.spacing[2],
      gap: theme.spacing[1],
    },
    bodyText: { ...theme.type.mono, color: theme.foreground, flexShrink: 1 },
    note: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}

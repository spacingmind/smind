// AutomationDetailScreen (ADR-0044 section 10, AC21): one automation's header and READ-ONLY run history (automation.get
// and automation.audit, newest first, paged with before=<oldest id shown>), plus the Pause brake for an active
// automation: a confirm first, then automation.disable once, the daemon's error text shown on failure. No other
// control: create/edit/enable/resume/fire are for the computer. Daemon text is plain <Text>.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { AUDIT_PAGE_SIZE, disableAutomation, fetchAutomation, fetchAutomationAudit } from '../automations/api';
import { appendPage, auditRowView, HISTORY_EMPTY_TEXT, HistoryPages, nextBefore } from '../automations/historyView';
import { automationRow } from '../automations/listView';
import { pauseWithConfirm } from '../automations/pause';
import { useAutomationResource } from '../automations/useAutomations';
import { RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { Badge } from '../ui/Badge';
import { confirmNative } from '../ui/confirm';
import { EmptyState } from '../ui/EmptyState';
import { TextButton } from '../ui/TextButton';
import { HistoryRow } from './automations/HistoryRow';
import { SectionHeader } from './tasks/SectionHeader';

export function AutomationDetailScreen({ conn, id }: { conn: RelayConnection; id: number }) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const load = useCallback(
    async (c: RelayConnection) => {
      const [info, first] = await Promise.all([fetchAutomation(c, id), fetchAutomationAudit(c, id, 0, AUDIT_PAGE_SIZE)]);
      return { info, first };
    },
    [id],
  );
  const { state, reload } = useAutomationResource(conn, load);
  // The first page comes with every (re)load; "Load more" pages are kept on top of it until the next reload.
  const [pages, setPages] = useState<HistoryPages>({ rows: [], more: false });
  const [loadingMore, setLoadingMore] = useState(false);
  const [moreError, setMoreError] = useState<string | null>(null);
  const [pause, setPause] = useState<{ busy: boolean; error: string | null }>({ busy: false, error: null });

  useEffect(() => {
    if (state.kind === 'ready') {
      setPages({ rows: state.value.first, more: state.value.first.length >= AUDIT_PAGE_SIZE });
      setMoreError(null);
    }
  }, [state]);

  const loadMore = useCallback(async () => {
    setLoadingMore(true);
    setMoreError(null);
    try {
      const page = await fetchAutomationAudit(conn, id, nextBefore(pages.rows), AUDIT_PAGE_SIZE);
      setPages((p) => appendPage(p, page, AUDIT_PAGE_SIZE));
    } catch (e) {
      setMoreError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoadingMore(false);
    }
  }, [conn, id, pages.rows]);

  const onPause = useCallback(
    async (name: string) => {
      setPause({ busy: true, error: null });
      const res = await pauseWithConfirm({ name, confirm: (t, m) => confirmNative(t, m), disable: () => disableAutomation(conn, id) });
      setPause({ busy: false, error: res.kind === 'error' ? res.message : null });
      if (res.kind === 'paused') reload();
    },
    [conn, id, reload],
  );

  if (state.kind === 'loading') {
    return (
      <View style={styles.screen}>
        <Text style={styles.muted}>Loading…</Text>
      </View>
    );
  }
  if (state.kind === 'error') {
    return (
      <View style={styles.center}>
        <EmptyState title="Couldn't load this automation" body={state.message} />
      </View>
    );
  }
  const row = automationRow(state.value.info);
  const history = pages.rows.map(auditRowView);

  return (
    <ScrollView style={styles.screen} contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
      <View style={styles.header}>
        <Text style={styles.name}>{row.name}</Text>
        <Badge tone={row.tone}>{row.status}</Badge>
      </View>
      <Text style={styles.line}>{row.signal}</Text>
      <Text style={styles.line}>{row.lastFired}</Text>
      {row.pausedReason !== null && <Text style={styles.warning}>{row.pausedReason}</Text>}
      {row.canPause && <TextButton label={pause.busy ? 'Pausing…' : 'Pause'} tone="danger" disabled={pause.busy} onPress={() => void onPause(row.name)} style={styles.pause} />}
      {pause.error !== null && (
        <Text style={styles.error} accessibilityRole="alert">
          {pause.error}
        </Text>
      )}
      <SectionHeader title="Run history" />
      {history.length === 0 ? (
        <Text style={styles.muted}>{HISTORY_EMPTY_TEXT}</Text>
      ) : (
        <View style={styles.list}>
          {history.map((r) => (
            <HistoryRow key={r.id} row={r} />
          ))}
        </View>
      )}
      {moreError !== null && <Text style={styles.error}>{moreError}</Text>}
      {pages.more && <TextButton label={loadingMore ? 'Loading…' : 'Load more'} disabled={loadingMore} onPress={() => void loadMore()} style={styles.more} />}
    </ScrollView>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    screen: { flex: 1, backgroundColor: theme.background },
    content: { padding: theme.spacing[4], paddingBottom: theme.spacing[8], gap: theme.spacing[2] },
    center: { alignItems: 'center', justifyContent: 'center', padding: theme.spacing[4] },
    header: { flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2] },
    name: { ...theme.type.heading2, color: theme.foreground, flex: 1 },
    line: { ...theme.type.small, color: theme.foregroundSubtle },
    muted: { ...theme.type.small, color: theme.foregroundSubtle, padding: theme.spacing[1] },
    warning: { ...theme.type.small, color: theme.status.warning },
    error: { ...theme.type.small, color: theme.status.danger },
    list: { gap: theme.spacing[2] },
    pause: { alignSelf: 'flex-start' },
    more: { alignSelf: 'center' },
  });
}

// InboxScreen (ADR-0045 section 5, AC20): the Human inbox, one column. Rows are grouped Needs you now / Ready for a
// decision / Stalled. A permission is only opened (the task screen's own permission card shows the full request) and
// answered there; the actions sheet never offers Snooze or Resolve for it. Data: inbox.list, refetched on
// inbox.changed and after every setter.

import { useCallback, useMemo, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { markInboxSeen, resolveInbox, snoozeInbox } from '../inbox/api';
import { useInbox } from '../inbox/useInbox';
import { InboxRowView, inboxView } from '../inbox/view';
import { RelayConnection } from '../relay/RelayConnection';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { AppIcon } from '../ui/AppIcon';
import { EmptyState } from '../ui/EmptyState';
import { TextButton } from '../ui/TextButton';
import { InboxActionSheet } from './inbox/InboxActionSheet';
import { InboxRow } from './inbox/InboxRow';

export function InboxScreen({
  conn,
  onOpenTask,
  onOpenHosts,
  onOpenReview,
  onOpenPolicy,
}: {
  conn: RelayConnection;
  onOpenTask: (taskId: number, title: string) => void;
  /** Opens the Hosts screen; hostId names the host the row is about, if any. */
  onOpenHosts: (hostId: string | null) => void;
  /** Opens the read-only review screen; taskId is the review's task, when it has one. */
  onOpenReview: (reviewId: number, taskId: number | null) => void;
  /** Opens the read-only permission policy screen. */
  onOpenPolicy: () => void;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const { list, reload } = useInbox(conn, true);
  const [sheetRow, setSheetRow] = useState<InboxRowView | null>(null);
  const [error, setError] = useState<string | null>(null);

  const run = useCallback(
    (op: () => Promise<void>) => {
      setError(null);
      op()
        .then(reload)
        .catch((e) => setError(e instanceof Error ? e.message : String(e)));
    },
    [reload],
  );

  const open = useCallback(
    (row: InboxRowView) => {
      // A review row opens the read-only review screen (it has its own Open task button), not the task.
      const review = row.kind === 'review' && row.reviewId !== null;
      // A host row (pairing, link down or refused) opens the Hosts screen, not a task.
      const host = row.actions.includes('hosts');
      if (!review && row.taskId === null && !host) return;
      if (row.unseen) markInboxSeen(conn, [row.key]).then(reload, () => {});
      if (review && row.reviewId !== null) onOpenReview(row.reviewId, row.taskId);
      else if (host) onOpenHosts(row.hostId);
      else if (row.taskId !== null) onOpenTask(row.taskId, row.title);
    },
    [conn, onOpenTask, onOpenHosts, onOpenReview, reload],
  );

  const handlers = useMemo(
    () => ({
      onOpen: open,
      onOpenHosts: (row: InboxRowView) => onOpenHosts(row.hostId),
      onSeen: (row: InboxRowView) => run(() => markInboxSeen(conn, [row.key])),
      onSnooze: (row: InboxRowView, until: string) => run(() => snoozeInbox(conn, [row.key], until)),
      onResolve: (row: InboxRowView) => run(() => resolveInbox(conn, [row.key])),
    }),
    [conn, open, onOpenHosts, run],
  );

  if (list.kind === 'loading') {
    return (
      <View style={styles.screen}>
        <Text style={styles.loading}>Loading inbox…</Text>
      </View>
    );
  }
  if (list.kind === 'error') {
    return (
      <View style={styles.center}>
        <EmptyState title="Couldn't load the inbox" body={list.message}>
          <TextButton label="Retry" onPress={reload} />
        </EmptyState>
      </View>
    );
  }
  const view = inboxView(list.items, Date.now());

  return (
    <>
      <ScrollView
        style={styles.screen}
        contentInsetAdjustmentBehavior="automatic"
        contentContainerStyle={styles.content}
        refreshControl={<RefreshControl refreshing={false} onRefresh={reload} />}
      >
        {error !== null && (
          <View style={styles.notice} accessibilityRole="alert">
            <Text style={styles.noticeText}>Couldn't update: {error}</Text>
          </View>
        )}
        {view.kind === 'empty' && (
          <View style={styles.center}>
            <EmptyState title={view.title} body={view.text} />
          </View>
        )}
        {view.kind === 'list' &&
          view.sections.map((section) => (
            <View key={section.kind} style={styles.section}>
              <Text style={styles.sectionTitle} accessibilityRole="header">
                {section.title}
              </Text>
              {section.rows.map((row) => (
                <InboxRow key={row.key} row={row} conn={conn} onOpen={open} onMore={setSheetRow} />
              ))}
            </View>
          ))}
        {/* The policy link stays the last child of the list, empty or not, so it is always reachable. */}
        <Pressable accessibilityRole="button" accessibilityLabel="Permission policy" onPress={onOpenPolicy} style={styles.policyLink}>
          <AppIcon name="lock" size={18} color={theme.foregroundSubtle} />
          <View style={styles.policyTexts}>
            <Text style={styles.policyTitle}>Permission policy</Text>
            <Text style={styles.policyNote}>Rules that allow or deny agent requests. Read-only.</Text>
          </View>
          <AppIcon name="chevronRight" size={18} color={theme.foregroundSubtle} />
        </Pressable>
      </ScrollView>
      <InboxActionSheet row={sheetRow} onClose={() => setSheetRow(null)} handlers={handlers} />
    </>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    screen: { flex: 1, backgroundColor: theme.background },
    content: { padding: theme.spacing[4], paddingBottom: theme.spacing[8], gap: theme.spacing[4] },
    center: { alignItems: 'center', justifyContent: 'center', padding: theme.spacing[4] },
    loading: { ...theme.type.small, color: theme.foregroundSubtle, padding: theme.spacing[4] },
    section: { gap: theme.spacing[2] },
    sectionTitle: { ...theme.type.label, color: theme.foregroundSubtle },
    notice: {
      backgroundColor: theme.surfaceSubtle,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      padding: theme.spacing[3],
    },
    noticeText: { ...theme.type.body, color: theme.foreground },
    policyLink: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: theme.spacing[3],
      minHeight: 44,
      padding: theme.spacing[3],
      backgroundColor: theme.card,
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
    },
    policyTexts: { flex: 1, gap: theme.spacing[1] },
    policyTitle: { ...theme.type.label, color: theme.foreground },
    policyNote: { ...theme.type.small, color: theme.foregroundSubtle },
  });
}

// StewardReviewScreen is the read-only steward review card (S3b): the verdict
// chip, the gate checklist, the findings per file and the evidence frames,
// exactly as the daemon reported them -- every daemon string is drawn as plain
// <Text> and never interpreted. The only actions are Share findings (the
// plain-text findings to paste to the Lead), Open task and Resolve; there is
// NO button of any other kind here -- nothing that answers a permission or
// lands anything on the PR. Resolve only hides this review from the inbox.

import { useCallback, useMemo, useState } from 'react';
import { ScrollView, Share, StyleSheet, Text, View } from 'react-native';
import { Button } from '@expo/ui';
import { resolveInbox } from '../inbox/api';
import { RelayConnection } from '../relay/RelayConnection';
import { findingsAsPrompt, gateRow, groupFindingsByFile, reviewHeader, severityLabel, sortGatesFailingFirst } from '../steward/view';
import { useReview } from '../steward/useReview';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { AppIcon } from '../ui/AppIcon';
import { Badge } from '../ui/Badge';
import { Card } from '../ui/Card';
import { EmptyState } from '../ui/EmptyState';
import { TextButton } from '../ui/TextButton';
import { UiHost } from '../ui/UiHost';
import { VerdictChip } from '../ui/VerdictChip';
import { FrameThumb } from './steward/FrameThumb';
import { SectionHeader } from './tasks/SectionHeader';

export function StewardReviewScreen({
  conn,
  reviewId,
  onOpenTask,
  onResolved,
}: {
  conn: RelayConnection;
  reviewId: number;
  onOpenTask: (taskId: number) => void;
  onResolved: () => void;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const { state, reload } = useReview(conn, reviewId);
  const [shared, setShared] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const resolve = useCallback(() => {
    setBusy(true);
    setError('');
    resolveInbox(conn, [`review:${reviewId}`])
      .then(() => onResolved())
      .catch((e: unknown) => {
        setBusy(false);
        setError(e instanceof Error ? e.message : String(e));
      });
  }, [conn, onResolved, reviewId]);

  if (state.kind === 'loading') {
    return (
      <View style={styles.screen}>
        <Text style={styles.muted}>Loading review…</Text>
      </View>
    );
  }
  if (state.kind === 'error') {
    return (
      <View style={styles.center}>
        <EmptyState title="Couldn't load this review" body={state.message}>
          <TextButton label="Retry" onPress={reload} />
        </EmptyState>
      </View>
    );
  }

  const review = state.review;
  const h = reviewHeader(review);
  const gateRows = sortGatesFailingFirst(review.gates).map(gateRow);
  const groups = groupFindingsByFile(review.findings);
  const frames = review.frames ?? [];

  const share = async () => {
    try {
      // Web has no Share module of its own unless the bundler provides one; a missing sheet is not an error.
      if (typeof Share === 'undefined' || typeof Share.share !== 'function') return;
      const result = await Share.share({ message: findingsAsPrompt(review) });
      if (result.action === Share.sharedAction) setShared(true);
    } catch {
      // dismissed or unsupported: nothing to report
    }
  };

  return (
    <ScrollView style={styles.screen} contentInsetAdjustmentBehavior="automatic" contentContainerStyle={styles.content}>
      <Card style={styles.headerCard}>
        <View style={styles.headerRow}>
          <VerdictChip verdict={review.verdict} />
          {h.pr !== null && <Text style={styles.meta}>{h.pr}</Text>}
          <Text style={styles.mono}>{h.head7}</Text>
        </View>
        {h.staleNote !== null && <Text style={styles.warning}>{h.staleNote}</Text>}
        {h.summary !== null && (
          <Text style={styles.body} selectable>
            {h.summary}
          </Text>
        )}
        {h.blockedReason !== null && <Text style={styles.danger}>{h.blockedReason}</Text>}
      </Card>

      <SectionHeader title="Gates" />
      <Card style={styles.card}>
        {gateRows.length === 0 ? (
          <Text style={styles.muted}>–</Text>
        ) : (
          gateRows.map((row) => (
            <View key={row.id} style={styles.gateRow}>
              <AppIcon name={row.icon} size={16} color={row.failing ? theme.status.danger : theme.foregroundSubtle} />
              <View style={styles.gateColumn}>
                <View style={styles.gateFirstLine}>
                  <Text style={styles.gateId} numberOfLines={1}>
                    {row.id}
                  </Text>
                  <Text style={row.failing ? styles.danger : styles.small}>{row.stateText}</Text>
                </View>
                {row.detail !== null && <Text style={styles.small}>{row.detail}</Text>}
              </View>
            </View>
          ))
        )}
      </Card>

      {groups.length > 0 && (
        <View style={styles.section}>
          <SectionHeader title="Findings" />
          {groups.map((g) => (
            <Card key={g.file} style={styles.card}>
              <Text style={styles.mono} selectable>
                {g.file}
              </Text>
              {g.findings.map((f, i) => (
                <View key={`${g.file}:${f.line}:${i}`} style={styles.finding}>
                  <View style={styles.findingRow}>
                    <Badge>{severityLabel(f)}</Badge>
                    <Text style={styles.small} selectable>{`line ${f.line}: ${f.why}`}</Text>
                  </View>
                  <Text style={styles.small} selectable>{`check: ${f.check}`}</Text>
                </View>
              ))}
            </Card>
          ))}
        </View>
      )}

      {frames.length > 0 && (
        <View style={styles.section}>
          <SectionHeader title="Evidence frames" />
          <View style={styles.frames}>
            {frames.map((frame) => (
              <FrameThumb key={frame.name} conn={conn} reviewId={reviewId} frame={frame} />
            ))}
          </View>
        </View>
      )}

      <View style={styles.actions}>
        {review.findings.length > 0 && (
          <UiHost>
            <Button variant="outlined" label={shared ? 'Shared' : 'Share findings'} onPress={share} style={{ height: 44 }} />
          </UiHost>
        )}
        {typeof review.taskId === 'number' && (
          <UiHost>
            <Button variant="outlined" label="Open task" onPress={() => onOpenTask(review.taskId)} style={{ height: 44 }} />
          </UiHost>
        )}
        <UiHost>
          <Button variant="outlined" label={busy ? 'Resolving…' : 'Resolve'} onPress={resolve} disabled={busy} style={{ height: 44 }} />
        </UiHost>
        <Text style={styles.small}>Resolving hides this review from your inbox. It does not change the PR.</Text>
        {error.length > 0 && (
          <Text style={styles.danger} accessibilityRole="alert" selectable>
            {error}
          </Text>
        )}
      </View>
    </ScrollView>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    screen: { flex: 1, backgroundColor: theme.background },
    content: { padding: theme.spacing[4], paddingBottom: theme.spacing[8], gap: theme.spacing[4] },
    center: { alignItems: 'center', justifyContent: 'center', padding: theme.spacing[4] },
    card: { gap: theme.spacing[3] },
    section: { gap: theme.spacing[3] },
    headerCard: { gap: theme.spacing[2] },
    headerRow: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
    meta: { ...theme.type.small, color: theme.foregroundSubtle },
    mono: { ...theme.type.mono, color: theme.foregroundSubtle },
    muted: { ...theme.type.small, color: theme.foregroundSubtle },
    body: { ...theme.type.body, color: theme.foreground },
    small: { ...theme.type.small, color: theme.foregroundSubtle },
    warning: { ...theme.type.small, color: theme.status.warning },
    danger: { ...theme.type.small, color: theme.status.danger },
    gateRow: { flexDirection: 'row', gap: theme.spacing[2] },
    gateColumn: { flexShrink: 1, gap: 2 },
    gateFirstLine: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
    gateId: { ...theme.type.mono, color: theme.foreground, flexShrink: 0 },
    finding: { gap: 2 },
    findingRow: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: theme.spacing[2] },
    frames: { gap: theme.spacing[3] },
    actions: { gap: theme.spacing[2], alignItems: 'stretch' },
  });
}

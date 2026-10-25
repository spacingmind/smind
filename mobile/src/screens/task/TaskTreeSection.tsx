// TaskTreeSection is the read-only "Subtasks" section under the run strip
// (ADR-0041 M1, docs/plans/completed/mobile-task-tree.md). It fetches task.tree,
// refreshes on a 30 s timer that also drives the countdown re-render, and
// renders nothing for a leaf task or on any error -- it never blocks the
// transcript. Expand/collapse swaps content in one frame (no animation).

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { fetchTaskTree, TaskTreeResult } from '../../api';
import { RelayConnection } from '../../relay/RelayConnection';
import { AppTheme } from '../../theme';
import { useAppTheme } from '../../theme/ThemeProvider';
import { buildTreeRows } from '../../taskTree';
import { AppIcon } from '../../ui/AppIcon';
import { TaskTreeRow } from './TaskTreeRow';

const REFRESH_MS = 30_000;
const MAX_BODY_HEIGHT = 360;

interface Props {
  conn: RelayConnection;
  taskId: number;
  pendingTaskIds: ReadonlySet<number>;
  onOpenTask: (id: number, title: string) => void;
}

export function TaskTreeSection({ conn, taskId, pendingTaskIds, onOpenTask }: Props) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [result, setResult] = useState<TaskTreeResult | null>(null);
  const [now, setNow] = useState(() => new Date());
  const [expanded, setExpanded] = useState(true);

  useEffect(() => {
    let alive = true;
    setResult(null);
    const load = () => {
      fetchTaskTree(conn, taskId)
        .then((r) => {
          if (!alive) return;
          setResult(r);
          setNow(new Date());
        })
        .catch(() => {
          if (alive) setResult(null); // an error is "no section"
        });
    };
    load();
    const timer = setInterval(load, REFRESH_MS);
    return () => {
      alive = false;
      clearInterval(timer);
    };
  }, [conn, taskId]);

  const built = useMemo(() => (result ? buildTreeRows(result, now, pendingTaskIds) : null), [result, now, pendingTaskIds]);
  const toggle = useCallback(() => setExpanded((e) => !e), []);

  if (!built || built.rows.length === 0) return null;
  const { rows, attention } = built;
  const count = (result?.nodes.length ?? 1) - 1;
  const summary = `Subtasks · ${count}`;
  const attentionLabel = [attention.waiting > 0 ? `${attention.waiting} waiting` : null, attention.rateLimited > 0 ? `${attention.rateLimited} rate limited` : null, attention.failed > 0 ? `${attention.failed} failed` : null]
    .filter(Boolean)
    .join(' · ');

  return (
    <View style={styles.wrap}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${summary}${attentionLabel ? `, ${attentionLabel}` : ''}`}
        accessibilityState={{ expanded }}
        accessibilityHint={expanded ? 'Collapses the subtasks list' : 'Expands the subtasks list'}
        onPress={toggle}
        style={({ pressed }) => [styles.header, pressed && styles.pressed]}
      >
        <Text style={styles.headerTitle}>{summary}</Text>
        <View style={styles.attention}>
          {attention.waiting > 0 ? <Text style={[styles.attentionText, { color: theme.status.warning }]}>{`${attention.waiting} waiting`}</Text> : null}
          {attention.waiting > 0 && attention.rateLimited > 0 ? <Text style={styles.attentionText}>·</Text> : null}
          {attention.rateLimited > 0 ? <Text style={[styles.attentionText, { color: theme.status.warning }]}>{`${attention.rateLimited} rate limited`}</Text> : null}
          {(attention.waiting > 0 || attention.rateLimited > 0) && attention.failed > 0 ? <Text style={styles.attentionText}>·</Text> : null}
          {attention.failed > 0 ? <Text style={[styles.attentionText, { color: theme.status.danger }]}>{`${attention.failed} failed`}</Text> : null}
        </View>
        {/* The registry has one chevron: rotated (not animated) to show the state. */}
        <View style={expanded ? styles.chevronOpen : undefined}>
          <AppIcon name="chevronRight" size={14} color={theme.foregroundSubtle} />
        </View>
      </Pressable>
      {expanded ? (
        <View style={styles.card}>
          <ScrollView style={styles.scroll} nestedScrollEnabled>
            {rows.map((row, i) =>
              row.kind === 'more' ? (
                <Text key={`more-${i}`} style={styles.more}>
                  {row.label}
                </Text>
              ) : (
                <TaskTreeRow
                  key={row.id}
                  id={row.id}
                  title={row.title}
                  depth={row.depth}
                  tierLabel={row.tierLabel}
                  state={row.state}
                  stateTone={row.stateTone}
                  stateNote={row.stateNote}
                  countdown={row.countdown}
                  stalledNote={row.stalledNote}
                  permissions={row.permissions}
                  unread={row.unread}
                  cost={row.cost}
                  isRoot={row.isRoot}
                  onPress={onOpenTask}
                />
              ),
            )}
          </ScrollView>
        </View>
      ) : null}
    </View>
  );
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    wrap: { paddingHorizontal: theme.spacing[4], paddingTop: theme.spacing[2], gap: theme.spacing[1] },
    header: { minHeight: 44, flexDirection: 'row', alignItems: 'center', gap: theme.spacing[2] },
    pressed: { opacity: 0.6 },
    headerTitle: { ...theme.type.label, color: theme.foreground },
    attention: { flex: 1, flexDirection: 'row', gap: theme.spacing[1] },
    attentionText: { ...theme.type.small, color: theme.foregroundSubtle },
    chevronOpen: { transform: [{ rotate: '90deg' }] },
    card: {
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.xl,
      borderCurve: 'continuous',
      backgroundColor: theme.card,
      overflow: 'hidden',
    },
    scroll: { maxHeight: MAX_BODY_HEIGHT },
    more: { ...theme.type.small, color: theme.foregroundSubtle, padding: theme.spacing[3] },
  });
}

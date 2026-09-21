// TasksScreen.tsx is Milestone 2's Item 2 list screen: resolve the paired
// daemon's workspaces (workspace.list), then list the first one's spaces
// and tasks (space.list/task.list) over the existing RelayConnection --
// never a new admit/handshake. Pull-to-refresh re-fetches over the same
// connection; an empty daemon renders an honest empty state; a call
// failure shows the error with a retry. Tapping a task hands off to the
// task detail screen (Item 3).
//
// Rebuilt on the token system as a run-card list (mobile-ui-polish plan
// Item 2): each task is a card with a status badge (icon + text, never
// color alone), sorted attention-first via a live permission.pending
// subscription (taskAttention.ts -- see its header for why the pending
// set is session-scoped, not persisted ground truth).

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ActivityIndicator, FlatList, RefreshControl, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { fetchOverview, Space, Task, Workspace } from '../api';
import { RelayConnection } from '../relay/RelayConnection';
import { PendingApprovalSet, sortTasksByAttention, subscribeToPendingApprovals } from '../taskAttention';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';

type LoadState =
  | { kind: 'loading' }
  | { kind: 'ready'; workspace: Workspace; spaces: Space[]; tasks: Task[] }
  | { kind: 'error'; message: string };

interface Props {
  conn: RelayConnection;
  onOpenTask: (task: Task) => void;
  onDisconnect: () => void;
}

/** {icon, label, color} for a task's status badge -- an icon so the signal never rests on color alone. */
function statusBadge(theme: AppTheme, status: string): { icon: string; color: string } {
  switch (status) {
    case 'running':
      return { icon: '●', color: theme.status.running };
    case 'error':
    case 'stopped':
      return { icon: '✕', color: theme.status.danger };
    case 'done':
      return { icon: '✓', color: theme.status.success };
    default:
      return { icon: '○', color: theme.foregroundMuted };
  }
}

export function TasksScreen({ conn, onOpenTask, onDisconnect }: Props) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const [state, setState] = useState<LoadState>({ kind: 'loading' });
  const [refreshing, setRefreshing] = useState(false);
  const [pendingTaskIds, setPendingTaskIds] = useState<ReadonlySet<number>>(new Set());
  const pendingApprovals = useRef(new PendingApprovalSet()).current;

  const load = useCallback(async () => {
    pendingApprovals.reset();
    setPendingTaskIds(new Set());
    try {
      const result = await loadTasks(conn);
      setState({ kind: 'ready', ...result });
    } catch (e) {
      setState({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
    } finally {
      setRefreshing(false);
    }
  }, [conn, pendingApprovals]);

  useEffect(() => {
    load();
  }, [load]);

  useEffect(() => {
    return subscribeToPendingApprovals(conn, (taskId) => {
      if (pendingApprovals.has(taskId)) return;
      pendingApprovals.markPending(taskId);
      setPendingTaskIds(new Set(pendingApprovals.ids));
    });
  }, [conn, pendingApprovals]);

  if (state.kind === 'loading') {
    return (
      <View style={styles.centered}>
        <ActivityIndicator size="large" color={theme.primary} />
      </View>
    );
  }

  if (state.kind === 'error') {
    return (
      <View style={styles.centered}>
        <Text style={styles.errorTitle}>Couldn't load the workspace</Text>
        <Text style={styles.errorDetail}>{state.message}</Text>
        <TouchableOpacity style={styles.retryButton} onPress={load}>
          <Text style={styles.retryButtonText}>Retry</Text>
        </TouchableOpacity>
      </View>
    );
  }

  const { workspace, spaces, tasks } = state;
  const ungrouped = tasks.filter((t) => t.SpaceID === null);

  return (
    <View style={styles.container}>
      <View style={styles.headerRow}>
        <Text style={styles.header}>{workspace.Title}</Text>
        <TouchableOpacity onPress={onDisconnect}>
          <Text style={styles.disconnect}>Disconnect</Text>
        </TouchableOpacity>
      </View>
      <FlatList
        data={[
          { kind: 'ungrouped' as const, tasks: sortTasksByAttention(ungrouped, pendingTaskIds) },
          ...spaces.map((s) => ({
            kind: 'grouped' as const,
            space: s,
            tasks: sortTasksByAttention(tasks.filter((t) => t.SpaceID === s.ID), pendingTaskIds),
          })),
        ]}
        keyExtractor={(item) => (item.kind === 'ungrouped' ? 'ungrouped' : `space-${item.space.ID}`)}
        refreshControl={
          <RefreshControl
            refreshing={refreshing}
            onRefresh={() => {
              setRefreshing(true);
              load();
            }}
          />
        }
        ListEmptyComponent={
          <View style={styles.empty}>
            <Text style={styles.emptyTitle}>No workspaces yet</Text>
            <Text style={styles.emptyDetail}>
              The paired daemon tracks no workspaces. Create one from the daemon or web UI, then pull to refresh.
            </Text>
          </View>
        }
        renderItem={({ item }) => (
          <View style={styles.section}>
            <Text style={styles.sectionTitle}>{item.kind === 'ungrouped' ? 'Ungrouped' : item.space.Title}</Text>
            {item.tasks.length === 0 ? (
              <Text style={styles.sectionEmpty}>No tasks</Text>
            ) : (
              item.tasks.map((task) => {
                const badge = statusBadge(theme, task.Status);
                const attention = pendingTaskIds.has(task.ID);
                return (
                  <TouchableOpacity key={task.ID} style={styles.taskCard} onPress={() => onOpenTask(task)}>
                    <View style={styles.taskCardTop}>
                      <Text style={styles.taskTitle}>{task.Title}</Text>
                      <View style={styles.statusBadge}>
                        <Text style={[styles.statusIcon, { color: badge.color }]}>{badge.icon}</Text>
                        <Text style={[styles.statusText, { color: badge.color }]}>{task.Status}</Text>
                      </View>
                    </View>
                    {attention && <Text style={styles.attentionLine}>Needs your approval (since you last checked)</Text>}
                  </TouchableOpacity>
                );
              })
            )}
          </View>
        )}
      />
    </View>
  );
}

/**
 * The screen's whole load: pick the daemon's first workspace (Milestone
 * 2's single-workscope scope -- the bridge pairs one daemon, and the
 * harness daemon has exactly one), then its spaces+tasks. Exported for
 * unit tests against a fake connection.
 */
export async function loadTasks(conn: RelayConnection): Promise<{ workspace: Workspace; spaces: Space[]; tasks: Task[] }> {
  const workspaces = (await conn.call('workspace.list')) as Workspace[];
  if (workspaces.length === 0) {
    return { workspace: { ID: 0, Path: '', Title: '(no workspace)', CreatedAt: '', UpdatedAt: '' }, spaces: [], tasks: [] };
  }
  const workspace = workspaces[0];
  const { spaces, tasks } = await fetchOverview(conn, workspace.ID);
  return { workspace, spaces, tasks };
}

function makeStyles(theme: AppTheme) {
  return StyleSheet.create({
    container: {
      flex: 1,
      backgroundColor: theme.surface[0],
      paddingTop: theme.spacing[16],
      paddingHorizontal: theme.spacing[4],
    },
    centered: {
      flex: 1,
      backgroundColor: theme.surface[0],
      alignItems: 'center',
      justifyContent: 'center',
      padding: theme.spacing[8],
    },
    headerRow: {
      flexDirection: 'row',
      justifyContent: 'space-between',
      alignItems: 'center',
      marginBottom: theme.spacing[4],
    },
    header: {
      fontSize: theme.type.sectionTitle.fontSize,
      lineHeight: theme.type.sectionTitle.lineHeight,
      fontWeight: theme.type.sectionTitle.fontWeight,
      color: theme.foreground,
      flex: 1,
    },
    disconnect: {
      color: theme.primary,
      fontSize: theme.type.interface.fontSize,
    },
    section: {
      marginBottom: theme.spacing[6],
    },
    sectionTitle: {
      fontSize: theme.type.metadataLabel.fontSize,
      lineHeight: theme.type.metadataLabel.lineHeight,
      fontWeight: theme.type.metadataLabel.fontWeight,
      color: theme.foregroundMuted,
      textTransform: 'uppercase',
      marginBottom: theme.spacing[2],
      letterSpacing: 0.5,
    },
    sectionEmpty: {
      color: theme.foregroundMuted,
      fontSize: theme.type.interface.fontSize,
    },
    taskCard: {
      backgroundColor: theme.surface[1],
      borderWidth: 1,
      borderColor: theme.border,
      borderRadius: theme.radius.md,
      padding: theme.spacing[3],
      marginBottom: theme.spacing[2],
      ...theme.elevation.sm,
    },
    taskCardTop: {
      flexDirection: 'row',
      justifyContent: 'space-between',
      alignItems: 'center',
    },
    taskTitle: {
      fontSize: theme.type.interface.fontSize,
      lineHeight: theme.type.interface.lineHeight,
      color: theme.foreground,
      flex: 1,
      marginRight: theme.spacing[2],
    },
    statusBadge: {
      flexDirection: 'row',
      alignItems: 'center',
    },
    statusIcon: {
      fontSize: theme.type.metadataLabel.fontSize,
      marginRight: theme.spacing[1],
    },
    statusText: {
      fontSize: theme.type.metadataLabel.fontSize,
      fontWeight: theme.type.interface.fontWeight,
      textTransform: 'uppercase',
      letterSpacing: 0.5,
    },
    attentionLine: {
      fontSize: theme.type.metadataLabel.fontSize,
      color: theme.status.warning,
      marginTop: theme.spacing[1],
    },
    empty: {
      alignItems: 'center',
      marginTop: theme.spacing[16],
      padding: theme.spacing[4],
    },
    emptyTitle: {
      fontSize: theme.type.panelTitle.fontSize,
      lineHeight: theme.type.panelTitle.lineHeight,
      fontWeight: theme.type.panelTitle.fontWeight,
      color: theme.foreground,
      marginBottom: theme.spacing[2],
    },
    emptyDetail: {
      fontSize: theme.type.interface.fontSize,
      color: theme.foregroundMuted,
      textAlign: 'center',
    },
    errorTitle: {
      fontSize: theme.type.panelTitle.fontSize,
      lineHeight: theme.type.panelTitle.lineHeight,
      fontWeight: theme.type.panelTitle.fontWeight,
      color: theme.status.danger,
      marginBottom: theme.spacing[2],
    },
    errorDetail: {
      fontSize: theme.type.codeAnnotation.fontSize,
      color: theme.foregroundMuted,
      textAlign: 'center',
      fontFamily: theme.type.codeAnnotation.fontFamily,
      marginBottom: theme.spacing[6],
    },
    retryButton: {
      backgroundColor: theme.primary,
      borderRadius: theme.radius.md,
      paddingVertical: theme.spacing[3],
      paddingHorizontal: theme.spacing[8],
    },
    retryButtonText: {
      color: theme.surface[0],
      fontSize: theme.type.interface.fontSize,
      fontWeight: theme.type.interface.fontWeight,
    },
  });
}

// TasksScreen.tsx is Milestone 2's Item 2 list screen: resolve the paired
// daemon's workspaces (workspace.list), then list the first one's spaces
// and tasks (space.list/task.list) over the existing RelayConnection --
// never a new admit/handshake. Pull-to-refresh re-fetches over the same
// connection; an empty daemon renders an honest empty state; a call
// failure shows the error with a retry. Tapping a task hands off to the
// task detail screen (Item 3).

import { useCallback, useEffect, useState } from 'react';
import { ActivityIndicator, FlatList, RefreshControl, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { fetchOverview, Space, Task, Workspace } from '../api';
import { RelayConnection } from '../relay/RelayConnection';

type LoadState =
  | { kind: 'loading' }
  | { kind: 'ready'; workspace: Workspace; spaces: Space[]; tasks: Task[] }
  | { kind: 'error'; message: string };

interface Props {
  conn: RelayConnection;
  onOpenTask: (task: Task) => void;
  onDisconnect: () => void;
}

export function TasksScreen({ conn, onOpenTask, onDisconnect }: Props) {
  const [state, setState] = useState<LoadState>({ kind: 'loading' });
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(async () => {
    try {
      const result = await loadTasks(conn);
      setState({ kind: 'ready', ...result });
    } catch (e) {
      setState({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
    } finally {
      setRefreshing(false);
    }
  }, [conn]);

  useEffect(() => {
    load();
  }, [load]);

  if (state.kind === 'loading') {
    return (
      <View style={styles.centered}>
        <ActivityIndicator size="large" />
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
          { kind: 'ungrouped' as const, tasks: ungrouped },
          ...spaces.map((s) => ({ kind: 'grouped' as const, space: s, tasks: tasks.filter((t) => t.SpaceID === s.ID) })),
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
              item.tasks.map((task) => (
                <TouchableOpacity key={task.ID} style={styles.taskRow} onPress={() => onOpenTask(task)}>
                  <Text style={styles.taskTitle}>{task.Title}</Text>
                  <Text style={styles.taskStatus}>{task.Status}</Text>
                </TouchableOpacity>
              ))
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

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#fff',
    paddingTop: 64,
    paddingHorizontal: 16,
  },
  centered: {
    flex: 1,
    backgroundColor: '#fff',
    alignItems: 'center',
    justifyContent: 'center',
    padding: 32,
  },
  headerRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    marginBottom: 16,
  },
  header: {
    fontSize: 22,
    fontWeight: '700',
    flex: 1,
  },
  disconnect: {
    color: '#2563eb',
    fontSize: 14,
  },
  section: {
    marginBottom: 24,
  },
  sectionTitle: {
    fontSize: 13,
    fontWeight: '600',
    color: '#666',
    textTransform: 'uppercase',
    marginBottom: 8,
    letterSpacing: 0.5,
  },
  sectionEmpty: {
    color: '#999',
    fontSize: 14,
  },
  taskRow: {
    paddingVertical: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: '#e5e5e5',
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  taskTitle: {
    fontSize: 16,
    flex: 1,
  },
  taskStatus: {
    fontSize: 12,
    color: '#666',
    marginLeft: 8,
  },
  empty: {
    alignItems: 'center',
    marginTop: 64,
    padding: 16,
  },
  emptyTitle: {
    fontSize: 16,
    fontWeight: '600',
    marginBottom: 8,
  },
  emptyDetail: {
    fontSize: 14,
    color: '#666',
    textAlign: 'center',
  },
  errorTitle: {
    fontSize: 16,
    fontWeight: '600',
    color: '#dc2626',
    marginBottom: 8,
  },
  errorDetail: {
    fontSize: 13,
    color: '#666',
    textAlign: 'center',
    fontFamily: 'monospace',
    marginBottom: 24,
  },
  retryButton: {
    backgroundColor: '#2563eb',
    borderRadius: 8,
    paddingVertical: 12,
    paddingHorizontal: 32,
  },
  retryButtonText: {
    color: '#fff',
    fontSize: 15,
    fontWeight: '600',
  },
});

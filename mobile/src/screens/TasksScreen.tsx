// TasksScreen.tsx is the task list: resolve the paired daemon's workspaces
// (workspace.list), then list the first one's spaces and tasks
// (space.list/task.list) over the existing RelayConnection -- never a new
// admit/handshake. Pull-to-refresh re-fetches over the same connection; an
// empty daemon renders an honest empty state; a call failure shows the error
// with a retry. Tapping a task hands off to the task detail screen.
//
// The ZCode-look list (mobile-zcode-redesign M3): ONE virtualized FlatList
// (vercel-react-native-skills) whose data is flattened by taskListItems --
// section headers, memoised rows grouped into one card per section, and
// per-section empties. Tasks are sorted attention-first via a live
// permission.pending subscription (taskAttention.ts -- see its header for
// why the pending set is session-scoped, not persisted ground truth).

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { FlatList, RefreshControl, StyleSheet, View } from 'react-native';
import { Stack, useFocusEffect } from 'expo-router';
import { Button } from '@expo/ui';
import { fetchOverview, Space, Task, Workspace } from '../api';
import { RelayConnection } from '../relay/RelayConnection';
import { useConnectionState } from '../relay/useConnectionState';
import { PendingApprovalSet, subscribeToPendingApprovals } from '../taskAttention';
import { buildTaskSections } from '../taskSections';
import { TaskListItem, taskListItems } from '../taskListItems';
import { AppTheme } from '../theme';
import { useAppTheme } from '../theme/ThemeProvider';
import { ReconnectingBanner } from '../ui/Banner';
import { EmptyState } from '../ui/EmptyState';
import { UiHost } from '../ui/UiHost';
import { EmptySection } from './tasks/EmptySection';
import { SectionHeader } from './tasks/SectionHeader';
import { TaskListSkeleton } from './tasks/TaskListSkeleton';
import { TaskRow } from './tasks/TaskRow';

type LoadState =
  | { kind: 'loading' }
  | { kind: 'ready'; workspace: Workspace; spaces: Space[]; tasks: Task[] }
  | { kind: 'error'; message: string };

interface Props {
  conn: RelayConnection;
  onOpenTask: (task: Task) => void;
}

export function TasksScreen({ conn, onOpenTask }: Props) {
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

  // Loaded again after every reconnect: a drop interrupts whatever was loading.
  const { epoch, state: connState } = useConnectionState(conn);
  // Also loaded again whenever the screen regains focus: the native stack keeps this screen mounted under a task
  // (the old hand-rolled stack remounted it), so what was answered or sent there must show on return.
  useFocusEffect(
    useCallback(() => {
      load();
    }, [load, epoch]),
  );

  useEffect(() => {
    return subscribeToPendingApprovals(conn, (taskId) => {
      if (pendingApprovals.has(taskId)) return;
      pendingApprovals.markPending(taskId);
      setPendingTaskIds(new Set(pendingApprovals.ids));
    });
  }, [conn, pendingApprovals]);

  const banner =
    connState.kind === 'connecting' || connState.kind === 'reconnecting' ? <ReconnectingBanner>Reconnecting to the daemon…</ReconnectingBanner> : null;

  if (state.kind === 'loading') {
    return (
      <View style={styles.screen}>
        {banner}
        <View style={styles.content}>
          <TaskListSkeleton />
        </View>
      </View>
    );
  }

  if (state.kind === 'error') {
    return (
      <View style={styles.screen}>
        {banner}
        <View style={styles.state}>
          <EmptyState title="Couldn't load the workspace" body={state.message}>
            <UiHost>
              <Button label="Retry" onPress={load} style={{ height: 44 }} />
            </UiHost>
          </EmptyState>
        </View>
      </View>
    );
  }

  const { workspace, spaces, tasks } = state;

  return (
    <View style={styles.screen}>
      {/* expo-native-ui: the stack title is the workspace title, not a Text on the page. */}
      <Stack.Screen options={{ title: workspace.Title }} />
      {banner}
      <TasksFlatList
        spaces={spaces}
        tasks={tasks}
        pendingTaskIds={pendingTaskIds}
        refreshing={refreshing}
        onRefresh={() => {
          setRefreshing(true);
          load();
        }}
        onOpenTask={onOpenTask}
      />
    </View>
  );
}

/** The list itself, split out so the ready state's identity stays stable across re-renders. */
function TasksFlatList({
  spaces,
  tasks,
  pendingTaskIds,
  refreshing,
  onRefresh,
  onOpenTask,
}: {
  spaces: Space[];
  tasks: Task[];
  pendingTaskIds: ReadonlySet<number>;
  refreshing: boolean;
  onRefresh: () => void;
  onOpenTask: (task: Task) => void;
}) {
  const theme = useAppTheme();
  const styles = useMemo(() => makeStyles(theme), [theme]);
  const items = useMemo(() => taskListItems(buildTaskSections(spaces, tasks, pendingTaskIds), pendingTaskIds), [spaces, tasks, pendingTaskIds]);

  // vercel-react-native-skills: rows take PRIMITIVE props and a stable
  // callback that receives the id; the task object is looked up here.
  const tasksById = useRef(new Map<number, Task>());
  tasksById.current = new Map(tasks.map((t) => [t.ID, t]));
  const openById = useCallback(
    (id: number) => {
      const task = tasksById.current.get(id);
      if (task) onOpenTask(task);
    },
    [onOpenTask],
  );

  const renderItem = useCallback(
    ({ item }: { item: TaskListItem }) => {
      if (item.type === 'header') return <SectionHeader title={item.title} />;
      if (item.type === 'empty') return <EmptySection text={item.text} />;
      return <TaskRow id={item.taskId} title={item.title} status={item.status} attention={item.attention} position={item.position} onPress={openById} />;
    },
    [openById],
  );

  return (
    <FlatList
      contentInsetAdjustmentBehavior="automatic" // expo-native-ui: the stack header owns the inset
      contentContainerStyle={styles.listContent}
      data={items}
      keyExtractor={(item) => item.key}
      renderItem={renderItem}
      refreshControl={<RefreshControl refreshing={refreshing} onRefresh={onRefresh} />}
      ListEmptyComponent={
        <View style={styles.state}>
          <EmptyState
            title="No workspaces yet"
            body="The paired daemon tracks no workspaces. Create one from the daemon or web UI, then pull to refresh."
          >
            <UiHost>
              <Button variant="outlined" label="Refresh" onPress={onRefresh} style={{ height: 44 }} />
            </UiHost>
          </EmptyState>
        </View>
      }
    />
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
    screen: {
      flex: 1,
      backgroundColor: theme.background,
    },
    content: {
      flex: 1,
      padding: theme.spacing[4],
    },
    listContent: {
      paddingHorizontal: theme.spacing[4],
      paddingBottom: theme.spacing[4],
    },
    state: {
      flex: 1,
      alignItems: 'center',
      justifyContent: 'center',
      padding: theme.spacing[8],
    },
  });
}

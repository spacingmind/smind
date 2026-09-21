// App.tsx is Milestone 2's app shell (docs/plans/active/
// mobile-app-milestone-2.md): a hand-rolled screen stack -- no navigation
// library, just useState over a small Screen union (the plan's Item 2
// Decision) -- over the pairing screen from Milestone 1, the workspace/
// task list screen (Item 2), and the task detail screen with its
// realtime timeline (Item 3). One RelayConnection lives for the whole
// connected session and is passed down; it is closed when the user
// disconnects.

import { useCallback, useState } from 'react';
import { StatusBar } from 'expo-status-bar';
import { PairingScreen } from './src/screens/PairingScreen';
import { TaskDetailScreen } from './src/screens/TaskDetailScreen';
import { TasksScreen } from './src/screens/TasksScreen';
import { Task } from './src/api';
import { RelayConnection } from './src/relay/RelayConnection';
import { AppThemeProvider } from './src/theme/ThemeProvider';

type Screen = { kind: 'pairing' } | { kind: 'tasks' } | { kind: 'task'; task: Task };

function AppShell() {
  const [conn, setConn] = useState<RelayConnection | null>(null);
  const [screen, setScreen] = useState<Screen>({ kind: 'pairing' });

  const handleConnected = useCallback((c: RelayConnection) => {
    setConn(c);
    setScreen({ kind: 'tasks' });
  }, []);

  const handleDisconnect = useCallback(() => {
    conn?.close();
    setConn(null);
    setScreen({ kind: 'pairing' });
  }, [conn]);

  const handleOpenTask = useCallback((task: Task) => {
    setScreen({ kind: 'task', task });
  }, []);

  const handleBack = useCallback(() => {
    setScreen({ kind: 'tasks' });
  }, []);

  if (!conn || screen.kind === 'pairing') {
    return <PairingScreen onConnected={handleConnected} />;
  }

  return (
    <>
      <StatusBar style="auto" />
      {screen.kind === 'task' ? (
        <TaskDetailScreen conn={conn} taskId={screen.task.ID} taskTitle={screen.task.Title} onBack={handleBack} />
      ) : (
        <TasksScreen conn={conn} onOpenTask={handleOpenTask} onDisconnect={handleDisconnect} />
      )}
    </>
  );
}

export default function App() {
  return (
    <AppThemeProvider>
      <AppShell />
    </AppThemeProvider>
  );
}

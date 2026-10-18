// App.tsx is Milestone 2's app shell (docs/plans/active/
// mobile-app-milestone-2.md): a hand-rolled screen stack -- no navigation
// library, just useState over a small Screen union (the plan's Item 2
// Decision) -- over the pairing screen from Milestone 1, the workspace/
// task list screen (Item 2), and the task detail screen with its
// realtime timeline (Item 3). One RelayConnection lives for the whole
// connected session and is passed down; it is closed when the user
// disconnects.
//
// No root <Host> here: each @expo/ui subtree carries its own Host via
// src/ui/UiHost (the root-Host pattern from PR #183 broke layout -- see
// UiHost.tsx's header).

import { useCallback, useState } from 'react';
import { StatusBar } from 'expo-status-bar';
import { PairingScreen } from './src/screens/PairingScreen';
import { TaskDetailScreen } from './src/screens/TaskDetailScreen';
import { TasksScreen } from './src/screens/TasksScreen';
import { Task } from './src/api';
import { RelayConnection } from './src/relay/RelayConnection';
import { describeState } from './src/relay/connectionStatus';
import { resetDeviceIdentity } from './src/relay/platformKeyStore';
import { useConnectionState } from './src/relay/useConnectionState';
import { ConnectionStatusCard, ReconnectingBanner } from './src/screens/ConnectionStatusCard';
import { DaemonPasswordPrompt } from './src/screens/DaemonPasswordPrompt';
import { useAppTheme } from './src/theme/ThemeProvider';
import { AppThemeProvider } from './src/theme/ThemeProvider';
import { View } from 'react-native';

type Screen = { kind: 'pairing' } | { kind: 'tasks' } | { kind: 'task'; task: Task };

function AppShell() {
  const [conn, setConn] = useState<RelayConnection | null>(null);
  const [screen, setScreen] = useState<Screen>({ kind: 'pairing' });
  const theme = useAppTheme();
  const { state } = useConnectionState(conn);

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

  const handleResetIdentity = useCallback(async () => {
    // The old key is gone before the pairing screen can start a new connection.
    // If it cannot be removed, stay on this card: leaving would reconnect with
    // the same (revoked) identity.
    try {
      await resetDeviceIdentity();
    } catch {
      return;
    }
    handleDisconnect();
  }, [handleDisconnect]);

  if (!conn || screen.kind === 'pairing') {
    return <PairingScreen onConnected={handleConnected} />;
  }

  // The daemon got a password (or its password changed) while this session was
  // down: ask for it here, over the same screen, instead of retrying blindly.
  if (state.kind === 'password' || state.kind === 'locked') {
    return (
      <View style={{ flex: 1, backgroundColor: theme.surface[0], paddingTop: theme.spacing[16], paddingHorizontal: theme.spacing[4] }}>
        <StatusBar style="auto" />
        <DaemonPasswordPrompt state={state} onSubmit={(password) => conn.submitPassword(password)} actions={[{ label: 'Back', onPress: handleDisconnect }]} />
      </View>
    );
  }

  // A live session can still end up refused: the daemon owner revoked this
  // device (or forgot it, so it is pending again), or the daemon at the other
  // end changed keys. Say so instead of
  // leaving screens whose calls all reject.
  if (state.kind === 'revoked' || state.kind === 'failed' || state.kind === 'pending') {
    const view = describeState(state)!;
    const actions =
      state.kind === 'revoked'
        ? [
            { label: 'Reset device identity', onPress: handleResetIdentity },
            { label: 'Back', onPress: handleDisconnect },
          ]
        : [{ label: 'Back', onPress: handleDisconnect }];
    return (
      <View style={{ flex: 1, backgroundColor: theme.surface[0], paddingTop: theme.spacing[16], paddingHorizontal: theme.spacing[4] }}>
        <StatusBar style="auto" />
        <ConnectionStatusCard view={view} actions={actions} />
      </View>
    );
  }

  return (
    <>
      <StatusBar style="auto" />
      {state.kind === 'reconnecting' ? <ReconnectingBanner>Reconnecting to the daemon…</ReconnectingBanner> : null}
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

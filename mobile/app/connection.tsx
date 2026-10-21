// The former full-screen status cards (ADR-0036 decision 3, `status` gate).

import { View } from 'react-native';
import { Stack } from 'expo-router';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { describeState } from '../src/relay/connectionStatus';
import { ConnectionStatusCard } from '../src/screens/ConnectionStatusCard';
import { DaemonPasswordPrompt } from '../src/screens/DaemonPasswordPrompt';
import { useAppTheme } from '../src/theme/ThemeProvider';

export default function ConnectionRoute() {
  const ctx = useConnectionOrNull();
  const theme = useAppTheme();
  if (!ctx?.conn) return null;
  const { conn, state, disconnect, resetIdentity } = ctx;
  const body = { flex: 1, paddingHorizontal: theme.spacing[4] } as const;

  // The daemon got a password (or its password changed) while this session was
  // down: ask for it here instead of retrying blindly.
  if (state.kind === 'password' || state.kind === 'locked') {
    return (
      <View style={body}>
        <Stack.Screen options={{ title: 'Connection' }} />
        <DaemonPasswordPrompt state={state} onSubmit={(password) => conn.submitPassword(password)} actions={[{ label: 'Back', onPress: disconnect }]} />
      </View>
    );
  }

  // The daemon owner revoked this device (or forgot it, so it is pending
  // again), or the daemon at the other end changed keys.
  const view = describeState(state);
  if (!view) return null;
  const actions =
    state.kind === 'revoked'
      ? [
          { label: 'Reset device identity', onPress: resetIdentity },
          { label: 'Back', onPress: disconnect },
        ]
      : [{ label: 'Back', onPress: disconnect }];
  return (
    <View style={body}>
      <Stack.Screen options={{ title: view.title }} />
      <ConnectionStatusCard view={view} actions={actions} />
    </View>
  );
}

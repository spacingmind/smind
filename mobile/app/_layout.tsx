// Root layout (ADR-0036): the theme and the connection wrap one native Stack
// whose Protected groups are chosen by routeGate. A guard flip unmounts the
// screens it hides, as the old hand-rolled App.tsx did.

import { Stack } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { KeyboardProvider } from 'react-native-keyboard-controller';
import { ConnectionProvider, useConnection } from '../src/connection/ConnectionProvider';
import { routeGate } from '../src/navigation/gate';
import { PluginThemeController } from '../src/plugins/PluginThemeController';
import { PushController } from '../src/push/native/PushController';
import { taskHeaderTitle } from '../src/navigation/routes';
import { AppThemeProvider, useAppTheme } from '../src/theme/ThemeProvider';

function RootStack() {
  const theme = useAppTheme();
  const { conn, state } = useConnection();
  const gate = routeGate(!!conn, state);
  return (
    <>
      <StatusBar style="auto" />
      <Stack
        screenOptions={{
          // expo-native-ui / expo-router: native stack header, tokens only.
          headerStyle: { backgroundColor: theme.background },
          headerTintColor: theme.foreground,
          headerTitleStyle: { fontSize: theme.type.label.fontSize, fontWeight: theme.type.label.fontWeight },
          headerShadowVisible: false,
          headerBackButtonDisplayMode: 'minimal',
          contentStyle: { backgroundColor: theme.background },
        }}
      >
        <Stack.Protected guard={gate === 'pairing'}>
          <Stack.Screen name="pairing" options={{ title: 'smind pairing' }} />
        </Stack.Protected>
        <Stack.Protected guard={gate === 'status'}>
          <Stack.Screen name="connection" options={{ title: 'Connection' }} />
        </Stack.Protected>
        <Stack.Protected guard={gate === 'app'}>
          <Stack.Screen name="index" options={{ title: 'Tasks' }} />
          <Stack.Screen name="new-task" options={{ title: 'New task', presentation: 'modal' }} />
          <Stack.Screen name="plugins" options={{ title: 'Plugins' }} />
          <Stack.Screen name="plugin/[id]" options={{ title: 'Plugin' }} />
          <Stack.Screen name="hosts" options={{ title: 'Hosts' }} />
          <Stack.Screen name="inbox" options={{ title: 'Inbox' }} />
          <Stack.Screen name="review/[id]" options={{ title: 'Review' }} />
          <Stack.Screen name="policy" options={{ title: 'Permission policy' }} />
          <Stack.Screen name="automations" options={{ title: 'Automations' }} />
          <Stack.Screen name="automation/[id]" options={{ title: 'Automation' }} />
          <Stack.Screen name="task/[id]" options={({ route }) => ({ title: taskHeaderTitle(route.params as { id?: string; title?: string } | undefined) })} />
        </Stack.Protected>
      </Stack>
    </>
  );
}

export default function RootLayout() {
  return (
    <KeyboardProvider>
      <AppThemeProvider>
        <ConnectionProvider>
          <RootStack />
          <PushController />
          <PluginThemeController />
        </ConnectionProvider>
      </AppThemeProvider>
    </KeyboardProvider>
  );
}

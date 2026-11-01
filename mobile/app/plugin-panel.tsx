// One plugin panel (ADR-0042 section 5): the panel's view document, drawn read-only.

import { Stack, useLocalSearchParams } from 'expo-router';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { parsePluginPanelParams } from '../src/navigation/routes';
import { PluginPanelScreen } from '../src/screens/PluginPanelScreen';
import { EmptyState } from '../src/ui/EmptyState';

export default function PluginPanelRoute() {
  const ctx = useConnectionOrNull();
  const params = useLocalSearchParams<{ plugin?: string; panel?: string }>();
  if (!ctx?.conn) return null;
  const parsed = parsePluginPanelParams(params);
  if (!parsed.ok) {
    return (
      <>
        <Stack.Screen options={{ title: 'Panel' }} />
        <EmptyState title="Unknown panel" body="This link does not name a plugin panel." />
      </>
    );
  }
  return (
    <>
      <Stack.Screen options={{ title: 'Panel' }} />
      <PluginPanelScreen conn={ctx.conn} pluginId={parsed.plugin} panelId={parsed.panel} />
    </>
  );
}

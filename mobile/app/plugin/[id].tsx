// One plugin's read-only detail (ADR-0042 PA.5).

import { Stack, useLocalSearchParams } from 'expo-router';
import { useConnectionOrNull } from '../../src/connection/ConnectionProvider';
import { parsePluginParams } from '../../src/navigation/routes';
import { PluginDetailScreen } from '../../src/screens/PluginDetailScreen';
import { EmptyState } from '../../src/ui/EmptyState';

export default function PluginRoute() {
  const ctx = useConnectionOrNull();
  const params = useLocalSearchParams<{ id?: string }>();
  if (!ctx?.conn) return null;
  const parsed = parsePluginParams(params);
  if (!parsed.ok) {
    return (
      <>
        <Stack.Screen options={{ title: 'Plugin' }} />
        <EmptyState title="Unknown plugin" body="This link does not name a plugin." />
      </>
    );
  }
  return (
    <>
      <Stack.Screen options={{ title: 'Plugin' }} />
      <PluginDetailScreen conn={ctx.conn} id={parsed.id} />
    </>
  );
}

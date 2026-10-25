// The read-only Plugins list (ADR-0042 PA.5): a guarded route in the app group, reached from the Connection sheet.

import { Stack, useRouter } from 'expo-router';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { pluginHref } from '../src/navigation/routes';
import { PluginsScreen } from '../src/screens/PluginsScreen';

export default function PluginsRoute() {
  const ctx = useConnectionOrNull();
  const router = useRouter();
  if (!ctx?.conn) return null;
  return (
    <>
      <Stack.Screen options={{ title: 'Plugins' }} />
      <PluginsScreen conn={ctx.conn} onOpenPlugin={(id) => router.push(pluginHref(id))} />
    </>
  );
}

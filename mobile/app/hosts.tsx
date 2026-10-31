// The Hosts screen (ADR-0048 F1c): a guarded route in the app group, reached from the Connection sheet and from host inbox rows.

import { Stack, useLocalSearchParams } from 'expo-router';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { HostsScreen } from '../src/screens/HostsScreen';

export default function HostsRoute() {
  const ctx = useConnectionOrNull();
  const params = useLocalSearchParams<{ focus?: string | string[] }>();
  if (!ctx?.conn) return null;
  // A route param is a string or an array of them; anything else is ignored.
  const focus = typeof params.focus === 'string' && params.focus !== '' ? params.focus : undefined;
  return (
    <>
      <Stack.Screen options={{ title: 'Hosts' }} />
      <HostsScreen conn={ctx.conn} focus={focus} />
    </>
  );
}

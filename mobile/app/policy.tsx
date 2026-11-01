// The read-only Permission policy screen: a guarded route in the app group (the layout entry lives in
// _layout.tsx).

import { Stack } from 'expo-router';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { PolicyScreen } from '../src/screens/PolicyScreen';

export default function PolicyRoute() {
  const ctx = useConnectionOrNull();
  if (!ctx?.conn) return null;
  return (
    <>
      <Stack.Screen options={{ title: 'Permission policy' }} />
      <PolicyScreen conn={ctx.conn} />
    </>
  );
}

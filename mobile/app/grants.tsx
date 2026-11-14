// The Saved grants screen: a guarded route in the app group (the layout entry lives in _layout.tsx).

import { Stack } from 'expo-router';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { SavedGrantsScreen } from '../src/screens/SavedGrantsScreen';

export default function GrantsRoute() {
  const ctx = useConnectionOrNull();
  if (!ctx?.conn) return null;
  return (
    <>
      <Stack.Screen options={{ title: 'Saved grants' }} />
      <SavedGrantsScreen conn={ctx.conn} />
    </>
  );
}

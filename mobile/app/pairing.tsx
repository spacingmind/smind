import { Stack } from 'expo-router';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { PairingScreen } from '../src/screens/PairingScreen';

export default function PairingRoute() {
  const ctx = useConnectionOrNull();
  if (!ctx) return null;
  return (
    <>
      <Stack.Screen options={{ title: 'smind pairing' }} />
      <PairingScreen onConnected={ctx.connect} />
    </>
  );
}

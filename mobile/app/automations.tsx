// The read-only Automations list (ADR-0044 section 10): a guarded route in the app group, reached from the Connection sheet.

import { Stack, useRouter } from 'expo-router';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { automationHref } from '../src/navigation/routes';
import { AutomationsScreen } from '../src/screens/AutomationsScreen';

export default function AutomationsRoute() {
  const ctx = useConnectionOrNull();
  const router = useRouter();
  if (!ctx?.conn) return null;
  return (
    <>
      <Stack.Screen options={{ title: 'Automations' }} />
      <AutomationsScreen conn={ctx.conn} onOpenAutomation={(id) => router.push(automationHref(id))} />
    </>
  );
}

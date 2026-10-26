// One automation's read-only run history and Pause brake (ADR-0044 section 10).

import { Stack, useLocalSearchParams } from 'expo-router';
import { useConnectionOrNull } from '../../src/connection/ConnectionProvider';
import { parseAutomationParams } from '../../src/navigation/routes';
import { AutomationDetailScreen } from '../../src/screens/AutomationDetailScreen';
import { EmptyState } from '../../src/ui/EmptyState';

export default function AutomationRoute() {
  const ctx = useConnectionOrNull();
  const params = useLocalSearchParams<{ id?: string }>();
  if (!ctx?.conn) return null;
  const parsed = parseAutomationParams(params);
  if (!parsed.ok) {
    return (
      <>
        <Stack.Screen options={{ title: 'Automation' }} />
        <EmptyState title="Unknown automation" body="This link does not name an automation." />
      </>
    );
  }
  return (
    <>
      <Stack.Screen options={{ title: 'Automation' }} />
      <AutomationDetailScreen conn={ctx.conn} id={parsed.id} />
    </>
  );
}

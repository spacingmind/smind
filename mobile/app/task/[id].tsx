import { Stack, useLocalSearchParams } from 'expo-router';
import { useConnectionOrNull } from '../../src/connection/ConnectionProvider';
import { parseTaskParams } from '../../src/navigation/routes';
import { TaskDetailScreen, TaskLoadError } from '../../src/screens/TaskDetailScreen';

export default function TaskRoute() {
  const ctx = useConnectionOrNull();
  const params = useLocalSearchParams<{ id?: string; title?: string }>();
  if (!ctx?.conn) return null;
  const parsed = parseTaskParams(params);
  if (!parsed.ok) {
    return (
      <>
        <Stack.Screen options={{ title: 'Task' }} />
        <TaskLoadError />
      </>
    );
  }
  return (
    <>
      <Stack.Screen options={{ title: parsed.title }} />
      <TaskDetailScreen conn={ctx.conn} taskId={parsed.id} />
    </>
  );
}

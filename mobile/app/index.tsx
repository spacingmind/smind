import { Stack, useRouter } from 'expo-router';
import { Button } from '@expo/ui';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { taskHref } from '../src/navigation/routes';
import { TasksScreen } from '../src/screens/TasksScreen';
import { UiHost } from '../src/ui/UiHost';

export default function TasksRoute() {
  const ctx = useConnectionOrNull();
  const router = useRouter();
  if (!ctx?.conn) return null;
  const { conn, disconnect } = ctx;
  return (
    <>
      <Stack.Screen
        options={{
          title: 'Tasks',
          headerRight: () => (
            <UiHost>
              <Button variant="text" label="Disconnect" onPress={disconnect} />
            </UiHost>
          ),
        }}
      />
      <TasksScreen conn={conn} onOpenTask={(task) => router.push(taskHref(task))} />
    </>
  );
}

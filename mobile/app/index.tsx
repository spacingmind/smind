import { View } from 'react-native';
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
          // The Host sizes to its content and top-aligns on web; centre it on the header's own height.
          headerRight: () => (
            <View style={{ justifyContent: 'center', alignSelf: 'stretch' }}>
              <UiHost>
                <Button variant="text" label="Disconnect" onPress={disconnect} />
              </UiHost>
            </View>
          ),
        }}
      />
      <TasksScreen conn={conn} onOpenTask={(task) => router.push(taskHref(task))} />
    </>
  );
}

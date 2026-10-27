import { useState } from 'react';
import { View } from 'react-native';
import { Stack, useRouter } from 'expo-router';
import { useConnectionOrNull } from '../src/connection/ConnectionProvider';
import { AUTOMATIONS_HREF, NEW_TASK_HREF, PLUGINS_HREF, taskHref } from '../src/navigation/routes';
import { TasksScreen } from '../src/screens/TasksScreen';
import { ConnectionSheet } from '../src/screens/tasks/ConnectionSheet';
import { TextButton } from '../src/ui/TextButton';

export default function TasksRoute() {
  const ctx = useConnectionOrNull();
  const router = useRouter();
  const [sheetOpen, setSheetOpen] = useState(false);
  if (!ctx?.conn) return null;
  const { conn, disconnect, state } = ctx;
  return (
    <>
      <Stack.Screen
        options={{
          title: 'Tasks',
          // The Host sizes to its content and top-aligns on web; centre it on the header's own height.
          // Connection moved left so "New task" owns the right side; both stay labelled 44 pt TextButtons.
          headerLeft: () => (
            <View style={{ justifyContent: 'center', alignSelf: 'stretch' }}>
              <TextButton label="Connection" onPress={() => setSheetOpen(true)} />
            </View>
          ),
          headerRight: () => (
            <View style={{ justifyContent: 'center', alignSelf: 'stretch' }}>
              <TextButton label="New task" onPress={() => router.push(NEW_TASK_HREF)} />
            </View>
          ),
        }}
      />
      <TasksScreen conn={conn} onOpenTask={(task) => router.push(taskHref(task))} />
      <ConnectionSheet open={sheetOpen} onClose={() => setSheetOpen(false)} onDisconnect={disconnect} onOpenPlugins={() => router.push(PLUGINS_HREF)} onOpenAutomations={() => router.push(AUTOMATIONS_HREF)} state={state} />
    </>
  );
}

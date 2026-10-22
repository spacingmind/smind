// Native glue for the Android background push task. Imported once, for its side
// effects, from index.ts: the headless JS context only runs module-scope code,
// so the task must be defined here at module scope. iOS gets nothing (it uses an
// alert push plus a notification service extension, a later PR). Never logs.

import * as Notifications from 'expo-notifications';
import * as TaskManager from 'expo-task-manager';
import { Platform } from 'react-native';
import { ATTENTION_CHANNEL, handleBackgroundPush, PUSH_TASK_NAME, type PresentContent } from '../backgroundHandler';
import { platformPushKeyStore } from './platformPushKeys';

async function present(c: PresentContent): Promise<void> {
  await Notifications.scheduleNotificationAsync({
    ...(c.identifier !== undefined ? { identifier: c.identifier } : {}),
    content: { title: c.title, body: c.body, data: c.data },
    trigger: { channelId: ATTENTION_CHANNEL },
  });
}

if (Platform.OS === 'android') {
  TaskManager.defineTask(PUSH_TASK_NAME, async ({ data, error }) => {
    if (error) return;
    await handleBackgroundPush(data, {
      keys: platformPushKeyStore(),
      nowSeconds: () => Math.floor(Date.now() / 1000),
      present,
    });
  });
  Notifications.registerTaskAsync(PUSH_TASK_NAME).catch(() => {});
}

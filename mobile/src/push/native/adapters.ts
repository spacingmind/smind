// Platform adapters for the push registration flow (ADR-0040 section 3). Never log:
// the push token is a secret.

import Constants from 'expo-constants';
import * as Notifications from 'expo-notifications';
import { Platform } from 'react-native';
import { ATTENTION_CHANNEL } from '../backgroundHandler';

export function platformPushEnv(): { platform: string; executionEnvironment: string | undefined } {
  return { platform: Platform.OS, executionEnvironment: Constants.executionEnvironment };
}

export async function ensureAttentionChannel(): Promise<void> {
  if (Platform.OS !== 'android') return;
  await Notifications.setNotificationChannelAsync(ATTENTION_CHANNEL, {
    name: 'Needs attention',
    importance: Notifications.AndroidImportance.HIGH,
  });
}

export async function requestNotificationPermission(): Promise<boolean> {
  const current = await Notifications.getPermissionsAsync();
  if (current.granted) return true;
  if (!current.canAskAgain) return false;
  return (await Notifications.requestPermissionsAsync()).granted;
}

export async function getExpoPushToken(): Promise<string | null> {
  const projectId: unknown = Constants.expoConfig?.extra?.eas?.projectId ?? Constants.easConfig?.projectId;
  if (typeof projectId !== 'string' || projectId === '') return null;
  try {
    return (await Notifications.getExpoPushTokenAsync({ projectId })).data || null;
  } catch {
    return null;
  }
}

export function onPushTokenChange(cb: () => void): () => void {
  const sub = Notifications.addPushTokenListener(() => cb());
  return () => sub.remove();
}

// haptics.ts: one haptic per action, iOS only (expo-animation haptics table).
// Light = Send / Send now, Medium = Stop and permission answers, Error
// notification = a failed send. Never on web/Android, never the only feedback.

export type HapticAction = 'send' | 'stop' | 'permission' | 'error';

/** Haptics are an iOS-only affordance here; Android's motors and web have no equivalent worth firing. */
export function isHapticPlatform(os: string | undefined): boolean {
  return os === 'ios';
}

export function hapticFor(action: HapticAction): void {
  if (!isHapticPlatform(process.env.EXPO_OS)) return;
  // Lazy import keeps the pure guard above loadable in node tests (expo-haptics pulls in the expo runtime).
  void import('expo-haptics')
    .then((Haptics) =>
      action === 'send'
        ? Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light)
        : action === 'error'
          ? Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error)
          : Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium),
    )
    .catch(() => {}); // feedback only: a failing haptic engine must never break the action
}

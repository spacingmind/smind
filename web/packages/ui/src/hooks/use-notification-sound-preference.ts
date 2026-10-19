import { useNotificationSoundStore } from "@/stores/notification-sound-store";

/** The Notifications settings section's sound toggle (AC3), read live by useAttentionNotifications' caller. */
export function useNotificationSoundPreference(): {
  enabled: boolean;
  setEnabled: (enabled: boolean) => void;
} {
  const enabled = useNotificationSoundStore((s) => s.value);
  const setEnabled = useNotificationSoundStore((s) => s.set);
  return { enabled, setEnabled };
}

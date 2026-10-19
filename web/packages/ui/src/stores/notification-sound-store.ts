import { createPreferenceStore } from "@/stores/create-preference-store";
import { readNotificationSoundEnabled, writeNotificationSoundEnabled } from "@/lib/sidebar-preferences";

/**
 * The Notifications sound toggle, shared by the settings section (where it's set) and AppSidebar (which
 * reads it before firing attention notifications) so both stay live.
 */
export const useNotificationSoundStore = createPreferenceStore<boolean>(
  readNotificationSoundEnabled,
  writeNotificationSoundEnabled,
);

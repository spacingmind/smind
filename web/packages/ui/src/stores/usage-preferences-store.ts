import { createPreferenceStore } from "@/stores/create-preference-store";
import {
  readUsagePreferences,
  writeUsagePreferences,
  type UsagePreferences,
} from "@/lib/account-usage/preferences";

/**
 * The plan-usage preferences (display mode, account filter, sidebar summary, pins) as one shared store
 * for Settings -> Usage, the usage modal, and the sidebar footer item, replacing the hand-rolled
 * module store in the old use-usage-preferences hook.
 */
export const useUsagePreferencesStore = createPreferenceStore<UsagePreferences>(
  readUsagePreferences,
  writeUsagePreferences,
);

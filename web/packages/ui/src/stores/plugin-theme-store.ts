import { readStoredPluginTheme, writeStoredPluginTheme, type StoredPluginTheme } from "@/lib/plugin-theme";
import { createPreferenceStore } from "@/stores/create-preference-store";

/** The selected plugin theme (null = the app's own light/dark), one shared copy persisted in localStorage. */
export const usePluginThemeStore = createPreferenceStore<StoredPluginTheme | null>(readStoredPluginTheme, writeStoredPluginTheme);

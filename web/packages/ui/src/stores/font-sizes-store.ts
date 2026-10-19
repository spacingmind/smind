import { createPreferenceStore } from "@/stores/create-preference-store";
import { readStoredFontSizes, writeStoredFontSizes, type FontSizes } from "@/lib/settings-preferences";

/**
 * The three Appearance font-size steps as one shared copy, persisted under the same storage key and shape
 * (lib/settings-preferences.ts).
 */
export const useFontSizesStore = createPreferenceStore<FontSizes>(readStoredFontSizes, writeStoredFontSizes);

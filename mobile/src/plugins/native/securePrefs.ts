// The theme preference on the real expo-secure-store (already linked: no dev-client rebuild). Only the selected
// `<plugin>:<id>` string is stored, never colours. Where there is no secure store (Expo web, the smoke target) the
// choice lives in memory for the page. Kept under native/ because it imports expo-secure-store.

import * as SecureStore from 'expo-secure-store';
import { ThemePrefs } from '../themeStore';

const KEY = 'smind.pluginTheme';
let memory: string | null = null;

export const securePluginThemePrefs: ThemePrefs = {
  async load() {
    try {
      if (await SecureStore.isAvailableAsync()) return await SecureStore.getItemAsync(KEY);
    } catch {
      // unreadable: default colours
    }
    return memory;
  },
  async save(id) {
    memory = id;
    try {
      if (!(await SecureStore.isAvailableAsync())) return;
      if (id === null) await SecureStore.deleteItemAsync(KEY);
      else await SecureStore.setItemAsync(KEY, id);
    } catch {
      // best effort
    }
  },
};

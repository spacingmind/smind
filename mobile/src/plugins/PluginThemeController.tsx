// PluginThemeController: mounted once in the root layout inside the connection. Renders nothing. Restores the
// remembered selection, keeps the store's theme list in step with plugin.themes (refetched on plugin.updated and on
// reconnect), and drops the selection when its plugin no longer offers it.

import { useEffect, useSyncExternalStore } from 'react';
import { useConnectionOrNull } from '../connection/ConnectionProvider';
import { fetchPluginThemes } from './api';
import { securePluginThemePrefs } from './native/securePrefs';
import { pluginThemeStore, reconcileSelection } from './themeStore';
import { usePluginResource } from './usePluginResource';

export function PluginThemeController() {
  const conn = useConnectionOrNull()?.conn ?? null;
  const { state } = usePluginResource(conn, fetchPluginThemes);
  const selected = useSyncExternalStore(pluginThemeStore.subscribe, () => pluginThemeStore.get().selected);

  useEffect(() => {
    let alive = true;
    securePluginThemePrefs.load().then((id) => {
      if (alive && id !== null && pluginThemeStore.get().selected === null) pluginThemeStore.select(id);
    });
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (!conn) {
      pluginThemeStore.setThemes([]); // another daemon's themes never outlive its connection
      return;
    }
    if (state.kind !== 'ready') return;
    pluginThemeStore.setThemes(state.value);
    const kept = reconcileSelection(selected, state.value);
    if (kept !== selected) {
      pluginThemeStore.select(kept);
      void securePluginThemePrefs.save(kept);
    }
  }, [conn, state, selected]);

  return null;
}

// themeStore.ts: the plugin theme selection as a tiny external store (React's useSyncExternalStore reads it from the
// theme provider, which sits ABOVE the connection). `selected` is the remembered `<plugin>:<id>`; `themes` is what the
// connected daemon offers now. The active theme is the selected one only while the daemon still offers it: a plugin
// that was disabled or removed stops applying on the next plugin.themes answer, with no stale colours kept anywhere.

import { PluginTheme } from './types';

export interface ThemeStoreState {
  selected: string | null;
  themes: readonly PluginTheme[];
}

let state: ThemeStoreState = { selected: null, themes: [] };
const listeners = new Set<() => void>();

function set(next: ThemeStoreState): void {
  if (next.selected === state.selected && next.themes === state.themes) return;
  state = next;
  for (const l of [...listeners]) l();
}

export const pluginThemeStore = {
  get: (): ThemeStoreState => state,
  subscribe(listener: () => void): () => void {
    listeners.add(listener);
    return () => listeners.delete(listener);
  },
  select: (id: string | null): void => set({ ...state, selected: id }),
  setThemes: (themes: readonly PluginTheme[]): void => set({ ...state, themes }),
  /** Tests only. */
  reset: (): void => set({ selected: null, themes: [] }),
};

/** The selected theme if the daemon still offers it, else null (the app's own colours). */
export function activeTheme(s: ThemeStoreState): PluginTheme | null {
  return s.selected === null ? null : (s.themes.find((t) => t.qualified === s.selected) ?? null);
}

/**
 * What to remember after a plugin.themes answer: the selection survives while its theme is offered and is dropped when
 * it is not (plugin disabled, removed or switch off), so the next launch starts on the default as well.
 */
export function reconcileSelection(selected: string | null, offered: readonly PluginTheme[]): string | null {
  return selected !== null && offered.some((t) => t.qualified === selected) ? selected : null;
}

/** Where the selected id is remembered across launches (never the colours: they are re-read from the daemon). */
export interface ThemePrefs {
  load(): Promise<string | null>;
  save(id: string | null): Promise<void>;
}

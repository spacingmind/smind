// themeCaption.ts: the picker's one-line description of what a theme does on this phone.

import { PluginTheme } from './types';

export interface ThemeSupport {
  light: boolean;
  dark: boolean;
}

export const NOT_SUPPORTED_ON_MOBILE = 'Not supported on mobile';

export function themeCaption(t: PluginTheme, support: ThemeSupport): string {
  const from = `from ${t.plugin}`;
  if (!support.light && !support.dark) return `${NOT_SUPPORTED_ON_MOBILE} · ${from}`;
  const which = support.light && support.dark ? 'Light and dark' : support.light ? 'Light only' : 'Dark only';
  return `${which} · ${from}`;
}

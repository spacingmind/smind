// themeMap.ts: how a plugin theme (a per-scheme map keyed by the WEB's CSS custom property names) becomes a mobile
// AppTheme. ONE explicit table: every colour key of AppTheme is either mapped from a canonical token or listed with
// the reason it is not themeable (pluginTheme.test.ts proves the table is complete and only names canonical tokens).
// Nothing is executed or interpolated: values are parsed by themeColors.ts and either become a colour or the whole
// scheme is refused, leaving the app's own colours.

import { AppTheme } from '../theme';
import { PLUGIN_THEME_TOKENS } from './themeTokens';
import { isSafeThemeValue, toMobileColor } from './themeColors';
import { PluginTheme, ThemeScheme } from './types';

export type ThemeColorKey =
  | 'background'
  | 'card'
  | 'surfaceSubtle'
  | 'accent'
  | 'header'
  | 'overlay'
  | 'border'
  | 'borderStrong'
  | 'foreground'
  | 'foregroundSubtle'
  | 'foregroundSubtlest'
  | 'primary'
  | 'primaryForeground'
  | 'secondary'
  | 'status.success'
  | 'status.danger'
  | 'status.warning'
  | 'status.running'
  | 'statusDot.success'
  | 'statusDot.danger'
  | 'statusDot.warning'
  | 'statusDot.running'
  | 'waiting.surface'
  | 'waiting.foreground'
  | 'unread'
  | 'input.background'
  | 'input.border'
  | 'input.borderFocused'
  | 'diff.addition'
  | 'diff.deletion';

/** A web token, or why the key stays the app's own. */
export type ThemeMapping = { token: string } | { notThemeable: string };

export const THEME_MAP: Record<ThemeColorKey, ThemeMapping> = {
  background: { token: '--background' },
  card: { token: '--card' },
  surfaceSubtle: { token: '--surface' },
  accent: { token: '--accent' },
  header: { token: '--header' },
  overlay: { token: '--popover' },
  border: { token: '--border' },
  borderStrong: { token: '--border-hover' },
  foreground: { token: '--foreground' },
  foregroundSubtle: { token: '--foreground-subtle' },
  foregroundSubtlest: { token: '--foreground-subtlest' },
  primary: { token: '--primary' },
  primaryForeground: { token: '--primary-foreground' },
  secondary: { token: '--secondary' },
  'status.success': { token: '--success' },
  'status.danger': { token: '--destructive' },
  'status.warning': { token: '--warning' },
  'status.running': { token: '--warning' },
  'statusDot.success': { token: '--success' },
  'statusDot.danger': { token: '--destructive' },
  'statusDot.warning': { token: '--warning' },
  'statusDot.running': { token: '--warning' },
  'waiting.surface': { token: '--interaction-confirmation-surface' },
  'waiting.foreground': { token: '--interaction-confirmation-foreground' },
  unread: { notThemeable: 'no web token: the unread dot is a mobile-only cue' },
  'input.background': { token: '--input-bg' },
  'input.border': { token: '--input-border' },
  'input.borderFocused': { token: '--input-border-focused' },
  'diff.addition': { token: '--diff-addition' },
  'diff.deletion': { token: '--diff-deletion' },
};

/** AppTheme keys that are not colours, so a colour theme leaves them alone. */
export const NON_COLOR_THEME_KEYS: Record<string, string> = {
  mode: 'the scheme is the system setting, not the theme',
  'boxShadow.overlay': 'a shadow string, not a colour',
  type: 'typography',
  spacing: 'layout',
  radius: 'layout',
  duration: 'motion',
};

const TOKEN_SET: ReadonlySet<string> = new Set(PLUGIN_THEME_TOKENS);

/**
 * The mobile colours for one scheme of a theme, or null when the set is not exactly the canonical tokens with every
 * value safe, or a mapped value is not a colour. All-or-nothing: a half-applied theme can be unreadable.
 */
export function mobileColorsFor(set: Record<string, string> | undefined): Partial<Record<ThemeColorKey, string>> | null {
  if (!set || typeof set !== 'object') return null;
  const keys = Object.keys(set);
  if (keys.length !== TOKEN_SET.size) return null;
  for (const key of keys) if (!TOKEN_SET.has(key) || !isSafeThemeValue(set[key])) return null; // one unsafe value anywhere refuses the scheme, as on web
  const out: Partial<Record<ThemeColorKey, string>> = {};
  for (const [key, mapping] of Object.entries(THEME_MAP) as [ThemeColorKey, ThemeMapping][]) {
    if (!('token' in mapping)) continue;
    const color = toMobileColor(set[mapping.token]);
    if (color === null) return null;
    out[key] = color;
  }
  return out;
}

/** What selecting this theme does on this phone, for the picker's caption. */
export function themeSupport(theme: PluginTheme): { light: boolean; dark: boolean } {
  return { light: mobileColorsFor(theme.schemes.light) !== null, dark: mobileColorsFor(theme.schemes.dark) !== null };
}

function setPath(target: Record<string, unknown>, path: string, value: string): void {
  const [head, tail] = path.split('.');
  if (tail === undefined) target[head] = value;
  else target[head] = { ...(target[head] as Record<string, string>), [tail]: value };
}

/**
 * `base` (the app's own theme for the scheme in effect) with the plugin theme's colours laid over it. A theme that
 * does not declare the scheme, or whose set for it is refused, returns `base` unchanged: a theme never leaks into the
 * scheme it did not declare.
 */
export function applyPluginTheme(base: AppTheme, theme: PluginTheme | null | undefined, scheme: ThemeScheme): AppTheme {
  if (!theme) return base;
  const colors = mobileColorsFor(theme.schemes[scheme]);
  if (!colors) return base;
  const next: Record<string, unknown> = { ...base };
  for (const [key, value] of Object.entries(colors)) setPath(next, key, value);
  return next as unknown as AppTheme;
}

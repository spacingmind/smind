// theme.ts holds mobile's design tokens in ONE system (expo-design-system:
// tokens live in a single theme.ts, never a second token system). Values
// now follow ZCode's Zai Light/Zai Dark themes -- the same family as
// web/packages/ui/src/index.css -- re-valuing the old vocabulary rather
// than replacing it (the legacy keys -- surface[], foregroundMuted, ring,
// elevation and the old type roles -- are gone now that every screen uses
// the named tokens). `status` is
// the AA text tier (4.5:1 on background and card); `statusDot` is the
// fill tier (higher chroma, for dots/badges where AA text contrast
// doesn't apply). expo-native-ui: `boxShadow` carries CSS-style shadow
// strings, never RN shadow/elevation objects.
//
// AppThemeProvider/useAppTheme (theme/ThemeProvider.tsx) resolve `system`
// via useColorScheme() by default. They're kept in a separate file, not
// colocated here: this codebase's vitest setup has no react-native
// transform, so this file -- which theme.test.ts imports directly --
// must stay free of any `from 'react-native'` import transitively.

/** Mirrors react-native's Appearance.ColorSchemeName without importing react-native. */
export type ColorSchemeName = 'light' | 'dark' | 'unspecified';

export interface TypeRole {
  fontSize: number;
  lineHeight: number;
  fontWeight: '400' | '500' | '600';
  fontFamily?: string;
}

export interface AppTheme {
  mode: 'light' | 'dark';
  background: string;
  card: string;
  surfaceSubtle: string;
  accent: string;
  /** Native stack header / docked bars. */
  header: string;
  /** Sheets, menus, toasts. */
  overlay: string;
  border: string;
  borderStrong: string;
  foreground: string;
  foregroundSubtle: string;
  /** Placeholders only. */
  foregroundSubtlest: string;
  primary: string;
  primaryForeground: string;
  /** User bubble, chips. */
  secondary: string;
  /** Text/icon tier (AA on background and card). */
  status: { success: string; danger: string; warning: string; running: string };
  /** Fill tier: higher chroma than the text tier, for dots/badges. */
  statusDot: { success: string; danger: string; warning: string; running: string };
  /** Every "needs you" badge (ZCode confirmation treatment). */
  waiting: { surface: string; foreground: string };
  unread: string;
  input: { background: string; border: string; borderFocused: string };
  boxShadow: { overlay: string };
  diff: { addition: string; deletion: string };
  type: {
    heading1: TypeRole;
    heading2: TypeRole;
    body: TypeRole;
    reading: TypeRole;
    label: TypeRole;
    caption: TypeRole;
    small: TypeRole;
    micro: TypeRole;
    mono: TypeRole;
  };
  /** Tailwind's numeric scale (docs/design.md §4): spacing(4) === 16px. */
  spacing: Record<number, number>;
  radius: Record<'sm' | 'md' | 'lg' | 'xl' | '2xl' | 'full', number>;
  duration: { hover: number; menu: number; panel: number };
}

// vercel-react-native-skills 9.2: a small, closed set of type roles, not
// one size per usage site.
const TYPE: AppTheme['type'] = {
  heading1: { fontSize: 18, lineHeight: 26, fontWeight: '600' },
  heading2: { fontSize: 16, lineHeight: 24, fontWeight: '600' },
  body: { fontSize: 14, lineHeight: 20, fontWeight: '400' },
  reading: { fontSize: 14, lineHeight: 24, fontWeight: '400' },
  label: { fontSize: 14, lineHeight: 20, fontWeight: '500' },
  caption: { fontSize: 13, lineHeight: 18, fontWeight: '400' },
  small: { fontSize: 12, lineHeight: 16, fontWeight: '400' },
  micro: { fontSize: 10, lineHeight: 14, fontWeight: '500' },
  mono: { fontSize: 12, lineHeight: 18, fontWeight: '400', fontFamily: 'monospace' },
};

const SPACING: AppTheme['spacing'] = { 0: 0, 0.5: 2, 1: 4, 1.5: 6, 2: 8, 2.5: 10, 3: 12, 3.5: 14, 4: 16, 6: 24, 8: 32, 12: 48, 16: 64 };

const RADIUS: AppTheme['radius'] = { sm: 4, md: 6, lg: 8, xl: 12, '2xl': 16, full: 999 };

const DURATION: AppTheme['duration'] = { hover: 150, menu: 200, panel: 300 };

export const lightTheme: AppTheme = {
  mode: 'light',
  background: '#f8f8f8',
  card: '#ffffff',
  surfaceSubtle: 'rgba(13, 13, 13, 0.03)',
  accent: '#ebf4ff',
  header: '#ffffff',
  overlay: '#ffffff',
  border: 'rgba(13, 13, 13, 0.1)',
  borderStrong: 'rgba(13, 13, 13, 0.15)',
  foreground: '#262626',
  foregroundSubtle: 'rgba(38, 38, 38, 0.65)',
  foregroundSubtlest: 'rgba(38, 38, 38, 0.4)',
  primary: '#000000',
  primaryForeground: '#ffffff',
  secondary: '#e6e6e6',
  status: { success: '#166b32', danger: '#c42b2b', warning: '#a85f00', running: '#a85f00' },
  statusDot: { success: '#1e8a3e', danger: '#e03131', warning: '#e07b00', running: '#e07b00' },
  waiting: { surface: '#eaf7ee', foreground: '#166b32' },
  unread: '#0ea5e9',
  input: { background: '#ffffff', border: 'rgba(13, 13, 13, 0.1)', borderFocused: 'rgba(13, 13, 13, 0.15)' },
  boxShadow: { overlay: '0 4px 12px rgba(0, 0, 0, 0.1)' },
  diff: { addition: '#1e8a3e', deletion: '#e03131' },
  type: TYPE,
  spacing: SPACING,
  radius: RADIUS,
  duration: DURATION,
};

export const darkTheme: AppTheme = {
  mode: 'dark',
  background: '#161616',
  card: '#2b2b2b',
  surfaceSubtle: 'rgba(255, 255, 255, 0.05)',
  accent: '#001d3d',
  header: '#202020',
  overlay: '#2b2b2b',
  border: 'rgba(255, 255, 255, 0.1)',
  borderStrong: 'rgba(255, 255, 255, 0.15)',
  foreground: '#d4d4d4',
  foregroundSubtle: 'rgba(212, 212, 212, 0.64)',
  foregroundSubtlest: 'rgba(212, 212, 212, 0.3)',
  primary: '#ffffff',
  primaryForeground: '#000000',
  secondary: '#363636',
  status: { success: '#46bf72', danger: '#ff5c5c', warning: '#ff8a30', running: '#ff8a30' },
  statusDot: { success: '#46bf72', danger: '#ff5c5c', warning: '#ff8a30', running: '#ff8a30' },
  waiting: { surface: 'rgba(70, 191, 114, 0.16)', foreground: '#87d9a4' },
  unread: '#38bdf8',
  input: { background: '#2b2b2b', border: 'rgba(255, 255, 255, 0.1)', borderFocused: 'rgba(255, 255, 255, 0.15)' },
  boxShadow: { overlay: '0 4px 12px rgba(0, 0, 0, 0.4)' },
  diff: { addition: '#46bf72', deletion: '#ff5c5c' },
  type: TYPE,
  spacing: SPACING,
  radius: RADIUS,
  duration: DURATION,
};

/**
 * Pure resolution (system scheme -> theme), kept out of the component so
 * it's unit-testable without rendering. Accepts null/undefined too --
 * older RN/OS combinations can report no preference at all, and that
 * case falls back to light, same as 'unspecified'.
 */
export function resolveAppTheme(scheme: ColorSchemeName | null | undefined): AppTheme {
  return scheme === 'dark' ? darkTheme : lightTheme;
}

/** Accepts `#rgb` or `#rrggbb` (lowercase or uppercase); returns `rgba(r, g, b, alpha)` with alpha as given. Throws on anything else. */
export function withAlpha(color: string, alpha: number): string {
  const hex = color.startsWith('#') ? color.slice(1) : null;
  const expanded = hex === null ? null : hex.length === 3 ? hex.split('').map((c) => c + c).join('') : hex.length === 6 ? hex : null;
  if (expanded === null || !/^[0-9a-fA-F]{6}$/.test(expanded)) {
    throw new Error(`withAlpha: expected #rgb or #rrggbb, got: ${color}`);
  }
  const r = parseInt(expanded.slice(0, 2), 16);
  const g = parseInt(expanded.slice(2, 4), 16);
  const b = parseInt(expanded.slice(4, 6), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

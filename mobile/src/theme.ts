// theme.ts ports smind's web design-token vocabulary (docs/design.md) to
// the mobile app: the same semantic names -- surface-0..3, status/
// statusDot, diff, the type-role scale, elevation, duration -- valued
// independently per platform (mobile-ui-polish plan's Decisions: one
// vocabulary, two renderers, no shared npm package, see docs/plans/
// active/mobile-ui-polish.md). Hex values are hand-ported from
// web/packages/ui/src/index.css's :root/.dark blocks (oklch literals
// converted to sRGB hex); `foregroundMuted` isn't in that port list but
// is the standard shadcn muted-foreground companion to the exact
// background/foreground/muted oklch triad used here (oklch(0.556 0 0)
// light / oklch(0.708 0 0) dark), so it's derived the same way rather
// than invented from nothing.
//
// AppThemeProvider/useAppTheme (theme/ThemeProvider.tsx) resolve `system`
// via useColorScheme() by default (Decisions: no manual light/dark/
// system picker yet). They're kept in a separate file, not colocated
// here: this codebase's vitest setup has no react-native transform (no
// screen/component has ever been unit-imported, only logic-layer
// modules), so this file -- which theme.test.ts imports directly --
// must stay free of any `from 'react-native'` import transitively.

/** Mirrors react-native's Appearance.ColorSchemeName without importing react-native. */
export type ColorSchemeName = 'light' | 'dark' | 'unspecified';

export interface TypeRole {
  fontSize: number;
  lineHeight: number;
  fontWeight: '400' | '500' | '600';
  fontFamily?: string;
}

export interface ElevationShadow {
  shadowColor: string;
  shadowOffset: { width: number; height: number };
  shadowOpacity: number;
  shadowRadius: number;
  /** Android's shadow model has no shadowOpacity/shadowRadius; elevation is the closest equivalent. */
  elevation: number;
}

export interface AppTheme {
  mode: 'light' | 'dark';
  /** surface-0..3: background, card, muted, accent (docs/design.md §1's aliasing). */
  surface: [string, string, string, string];
  border: string;
  foreground: string;
  foregroundMuted: string;
  /** Brand cyan; primary and ring share one value on the web side too. */
  primary: string;
  ring: string;
  /** Text/icon tier (docs/design.md §1) -- danger doubles as `--destructive`. */
  status: { success: string; danger: string; warning: string; running: string };
  /** Dot tier: higher chroma than the text tier, for a 6px status dot. */
  statusDot: { success: string; danger: string; warning: string; running: string };
  diff: { addition: string; deletion: string };
  type: {
    workspaceTitle: TypeRole;
    sectionTitle: TypeRole;
    panelTitle: TypeRole;
    metadataLabel: TypeRole;
    codeAnnotation: TypeRole;
    interface: TypeRole;
    content: TypeRole;
  };
  elevation: { sm: ElevationShadow; md: ElevationShadow; lg: ElevationShadow };
  /** Tailwind's numeric scale (docs/design.md §4): spacing(4) === 16px. */
  spacing: Record<number, number>;
  radius: { sm: number; md: number; lg: number; full: number };
  duration: { hover: number; menu: number; panel: number };
}

const TYPE: AppTheme['type'] = {
  workspaceTitle: { fontSize: 14, lineHeight: 20, fontWeight: '600' },
  sectionTitle: { fontSize: 16, lineHeight: 24, fontWeight: '500' },
  panelTitle: { fontSize: 14, lineHeight: 20, fontWeight: '500' },
  metadataLabel: { fontSize: 12, lineHeight: 16, fontWeight: '500' },
  codeAnnotation: { fontSize: 12, lineHeight: 16, fontWeight: '500', fontFamily: 'monospace' },
  interface: { fontSize: 14, lineHeight: 20, fontWeight: '400' },
  content: { fontSize: 15, lineHeight: 24, fontWeight: '400' },
};

const SPACING: AppTheme['spacing'] = { 0: 0, 0.5: 2, 1: 4, 1.5: 6, 2: 8, 2.5: 10, 3: 12, 3.5: 14, 4: 16, 6: 24, 8: 32, 12: 48, 16: 64 };

const RADIUS: AppTheme['radius'] = { sm: 6, md: 8, lg: 12, full: 999 };

const DURATION: AppTheme['duration'] = { hover: 150, menu: 200, panel: 300 };

/** docs/design.md §13: shadow tiers are theme-asymmetric -- light is a whisper, dark is harder, both black. */
function elevation(opacity: { sm: number; md: number; lg: number }): AppTheme['elevation'] {
  return {
    sm: { shadowColor: '#000', shadowOffset: { width: 0, height: 1 }, shadowOpacity: opacity.sm, shadowRadius: 2, elevation: 1 },
    md: { shadowColor: '#000', shadowOffset: { width: 0, height: 2 }, shadowOpacity: opacity.md, shadowRadius: 4, elevation: 3 },
    lg: { shadowColor: '#000', shadowOffset: { width: 0, height: 4 }, shadowOpacity: opacity.lg, shadowRadius: 8, elevation: 6 },
  };
}

export const lightTheme: AppTheme = {
  mode: 'light',
  surface: ['#ffffff', '#ffffff', '#f5f5f5', '#f5f5f5'],
  border: '#e5e5e5',
  foreground: '#0a0a0a',
  foregroundMuted: '#737373',
  primary: '#0c6e6e',
  ring: '#0c6e6e',
  status: { success: '#3e704a', danger: '#e7000c', warning: '#7b5d39', running: '#396694' },
  statusDot: { success: '#299f51', danger: '#f12e2f', warning: '#b37824', running: '#268ae0' },
  diff: { addition: '#15803d', deletion: '#b91c1c' },
  type: TYPE,
  elevation: elevation({ sm: 0.04, md: 0.06, lg: 0.08 }),
  spacing: SPACING,
  radius: RADIUS,
  duration: DURATION,
};

export const darkTheme: AppTheme = {
  mode: 'dark',
  surface: ['#0a0a0a', '#0a0a0a', '#262626', '#262626'],
  border: '#292929',
  foreground: '#fafafa',
  foregroundMuted: '#a1a1a1',
  primary: '#51fbfd',
  ring: '#51fbfd',
  status: { success: '#6cb17b', danger: '#ff6467', warning: '#c09664', running: '#78a3cf' },
  statusDot: { success: '#35c264', danger: '#f7796d', warning: '#db932e', running: '#5caaf6' },
  diff: { addition: '#4ade80', deletion: '#ef4444' },
  type: TYPE,
  elevation: elevation({ sm: 0.24, md: 0.32, lg: 0.4 }),
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

// pluginTheme.test.ts: AM4. The token list mirrors the daemon's, one mapping table covers every AppTheme colour key,
// hostile values are refused, oklch converts, and a theme never applies outside its scheme or after its plugin is gone.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { darkTheme, lightTheme, withAlpha } from '../theme';
import { isSafeThemeValue, toMobileColor } from '../plugins/themeColors';
import { applyPluginTheme, mobileColorsFor, NON_COLOR_THEME_KEYS, THEME_MAP, themeSupport } from '../plugins/themeMap';
import { PLUGIN_THEME_TOKENS } from '../plugins/themeTokens';
import { activeTheme, pluginThemeStore, reconcileSelection } from '../plugins/themeStore';
import { PluginTheme } from '../plugins/types';

function leaves(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return prefix ? [prefix] : [];
  return Object.entries(value as Record<string, unknown>).flatMap(([k, v]) => leaves(v, prefix ? `${prefix}.${k}` : k));
}

/** A complete canonical set where every token is `fill` unless overridden. */
function tokenSet(over: Record<string, string> = {}, fill = '#102030'): Record<string, string> {
  return { ...Object.fromEntries(PLUGIN_THEME_TOKENS.map((t) => [t, fill])), ...over };
}

function theme(schemes: PluginTheme['schemes'], plugin = 'acme', id = 'dusk'): PluginTheme {
  return { plugin, id, qualified: `${plugin}:${id}`, label: 'Dusk', schemes };
}

describe('TestTheme_TokenListDrift (mobile copy vs the daemon list)', () => {
  it('PLUGIN_THEME_TOKENS equals internal/plugins/themetokens.json', () => {
    const daemon = JSON.parse(readFileSync(join(__dirname, '..', '..', '..', 'internal', 'plugins', 'themetokens.json'), 'utf8')) as string[];
    expect([...PLUGIN_THEME_TOKENS]).toEqual(daemon);
  });
});

describe('theme mapping completeness', () => {
  it('every AppTheme leaf is mapped from a token, listed as not themeable, or a documented non-colour', () => {
    const colorLeaves = leaves(lightTheme).filter((p) => !Object.keys(NON_COLOR_THEME_KEYS).some((k) => p === k || p.startsWith(`${k}.`)));
    expect(colorLeaves.sort()).toEqual(Object.keys(THEME_MAP).sort());
    expect(leaves(darkTheme).sort()).toEqual(leaves(lightTheme).sort());
  });

  it('every mapped token is a canonical token; every not-themeable key says why', () => {
    const canonical = new Set(PLUGIN_THEME_TOKENS);
    for (const [key, m] of Object.entries(THEME_MAP)) {
      if ('token' in m) expect(canonical.has(m.token), key).toBe(true);
      else expect(m.notThemeable.length, key).toBeGreaterThan(10);
    }
  });

  it('every mapped key lands on the AppTheme when applied (no key is mapped to nowhere)', () => {
    const out = applyPluginTheme(lightTheme, theme({ light: tokenSet({}, '#0a0b0c') }), 'light');
    for (const [key, m] of Object.entries(THEME_MAP)) {
      if (!('token' in m)) continue;
      const value = key.split('.').reduce<unknown>((o, k) => (o as Record<string, unknown>)[k], out);
      expect(value, key).toBe('#0a0b0c');
    }
    expect(out.unread).toBe(lightTheme.unread);
    expect(out.mode).toBe('light');
    expect(out.type).toBe(lightTheme.type);
  });
});

describe('TestTheme_HostileValues_Rejected', () => {
  const hostile = [
    'url(http://evil/x.png)',
    'url("data:image/svg+xml;base64,AAAA")',
    'var(--x)',
    'calc(1px + 2px)',
    'red; background: url(x)',
    '#fff; x',
    'rgb(0,0,0); }',
    '"#fff"',
    "'#fff'",
    '\\72 ed',
    'expression(alert(1))',
    'javascript:alert(1)',
    '#ff',
    '#fffff',
    'rgb(1,2)',
    'oklch(1 0)',
    'rgb(1 2 3 4 5)',
    '#ｆｆｆ', // fullwidth lookalikes
    '#fff\n',
    '#fff\u0000',
    '{}',
    '@import "x"',
    'x'.repeat(65),
    '',
    ' ',
    'rgb(0,0,0)/*',
    '‮#fff',
    'transparent;',
    '1e3',
    'inherit',
  ];
  it('none is accepted by the grammar and none converts to a colour', () => {
    for (const v of hostile) {
      expect(isSafeThemeValue(v), JSON.stringify(v)).toBe(false);
      expect(toMobileColor(v), JSON.stringify(v)).toBeNull();
    }
    expect(isSafeThemeValue(42)).toBe(false);
    expect(isSafeThemeValue(null)).toBe(false);
  });

  it('url( in ANY token refuses the whole scheme; the theme does not apply', () => {
    const bad = tokenSet({ '--terminal-red': 'url(http://evil/x)' });
    expect(mobileColorsFor(bad)).toBeNull();
    expect(applyPluginTheme(lightTheme, theme({ light: bad }), 'light')).toBe(lightTheme);
  });

  it('an unlisted extra key, a missing key, or a non-colour mapped value refuses the scheme', () => {
    expect(mobileColorsFor({ ...tokenSet(), '--evil': '#000000' })).toBeNull();
    const missing = tokenSet();
    delete missing['--background'];
    expect(mobileColorsFor(missing)).toBeNull();
    expect(mobileColorsFor(tokenSet({ '--background': '12px' }))).toBeNull();
    expect(mobileColorsFor(undefined)).toBeNull();
  });
});

describe('colour conversion', () => {
  it('hex, rgb, hsl pass through to #rrggbb or rgba()', () => {
    expect(toMobileColor('#fff')).toBe('#ffffff');
    expect(toMobileColor('#1A2B3C')).toBe('#1a2b3c');
    expect(toMobileColor('#1a2b3c80')).toBe('rgba(26, 43, 60, 0.502)');
    expect(toMobileColor('rgb(16, 32, 48)')).toBe('#102030');
    expect(toMobileColor('rgb(16 32 48 / 50%)')).toBe('rgba(16, 32, 48, 0.5)');
    expect(toMobileColor('hsl(0 100% 50%)')).toBe('#ff0000');
    expect(toMobileColor('hsla(120, 100%, 25%, 1)')).toBe('#008000');
    expect(toMobileColor('transparent')).toBe('rgba(0, 0, 0, 0)');
  });

  it('oklch converts (white, black, a saturated red within rounding), with alpha', () => {
    expect(toMobileColor('oklch(1 0 0)')).toBe('#ffffff');
    expect(toMobileColor('oklch(0 0 0)')).toBe('#000000');
    const red = toMobileColor('oklch(0.628 0.2577 29.23)')!;
    expect(red).toMatch(/^#[0-9a-f]{6}$/);
    expect(parseInt(red.slice(1, 3), 16)).toBeGreaterThanOrEqual(253);
    expect(parseInt(red.slice(3, 5), 16)).toBeLessThanOrEqual(3);
    expect(toMobileColor('oklch(50% 0 0 / 0.5)')).toMatch(/^rgba\(\d+, \d+, \d+, 0\.5\)$/);
  });

  it('withAlpha accepts the forms the converter emits', () => {
    expect(withAlpha('#102030', 0.14)).toBe('rgba(16, 32, 48, 0.14)');
    expect(withAlpha('rgba(16, 32, 48, 0.5)', 0.14)).toBe('rgba(16, 32, 48, 0.14)');
  });
});

describe('TestTheme_AppliesPerScheme / fallback', () => {
  const dark = theme({ dark: tokenSet({ '--background': '#102030' }) });

  it('a dark-only theme changes dark and leaves light alone', () => {
    expect(applyPluginTheme(darkTheme, dark, 'dark').background).toBe('#102030');
    expect(applyPluginTheme(lightTheme, dark, 'light')).toBe(lightTheme);
    expect(themeSupport(dark)).toEqual({ light: false, dark: true });
  });

  it('oklch tokens apply', () => {
    const t = theme({ light: tokenSet({ '--background': 'oklch(1 0 0)' }) });
    expect(applyPluginTheme(lightTheme, t, 'light').background).toBe('#ffffff');
  });

  it('TestTheme_FallbackWhenPluginDisabled: the selection applies only while the daemon still offers the theme', () => {
    pluginThemeStore.reset();
    pluginThemeStore.select('acme:dusk');
    expect(activeTheme(pluginThemeStore.get())).toBeNull(); // not offered yet: default colours
    pluginThemeStore.setThemes([dark]);
    expect(activeTheme(pluginThemeStore.get())).toBe(dark);
    pluginThemeStore.setThemes([]); // plugin disabled / removed / switch off
    expect(activeTheme(pluginThemeStore.get())).toBeNull();
    expect(reconcileSelection('acme:dusk', [])).toBeNull();
    expect(reconcileSelection('acme:dusk', [dark])).toBe('acme:dusk');
    expect(reconcileSelection(null, [dark])).toBeNull();
    pluginThemeStore.reset();
  });

  it('an update that replaces the tokens replaces the colours', () => {
    pluginThemeStore.reset();
    pluginThemeStore.select('acme:dusk');
    pluginThemeStore.setThemes([dark]);
    const v2 = theme({ dark: tokenSet({ '--background': '#001122' }) });
    pluginThemeStore.setThemes([v2]);
    expect(applyPluginTheme(darkTheme, activeTheme(pluginThemeStore.get()), 'dark').background).toBe('#001122');
    pluginThemeStore.reset();
  });
});

// theme.test.ts covers Item 1's Test Scenarios at the logic layer: both
// lightTheme/darkTheme define every token key the other does (mirrors
// web's token-presence.test.ts -- no mode with a missing token), and
// resolveAppTheme maps a system color scheme to the right theme object.

import { describe, expect, it } from 'vitest';
import { darkTheme, lightTheme, resolveAppTheme } from '../theme';

/** Every leaf key path in an object, e.g. {a:{b:1}} -> ['a.b']. Arrays and non-plain-object leaves stop recursion. */
function keyPaths(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return prefix ? [prefix] : [];
  const paths: string[] = [];
  for (const [key, val] of Object.entries(value as Record<string, unknown>)) {
    const path = prefix ? `${prefix}.${key}` : key;
    paths.push(...(val !== null && typeof val === 'object' && !Array.isArray(val) ? keyPaths(val, path) : [path]));
  }
  return paths;
}

describe('lightTheme/darkTheme token presence', () => {
  it('define exactly the same set of token keys', () => {
    expect(keyPaths(lightTheme).sort()).toEqual(keyPaths(darkTheme).sort());
  });

  it('every color value is a non-empty string', () => {
    for (const theme of [lightTheme, darkTheme]) {
      for (const path of keyPaths(theme)) {
        const value = path.split('.').reduce<unknown>((obj, key) => (obj as Record<string, unknown>)[key], theme);
        if (typeof value === 'string') expect(value.length).toBeGreaterThan(0);
      }
    }
  });
});

describe('resolveAppTheme', () => {
  it('resolves "dark" to darkTheme', () => {
    expect(resolveAppTheme('dark')).toBe(darkTheme);
  });

  it('resolves "light", null, and undefined to lightTheme', () => {
    expect(resolveAppTheme('light')).toBe(lightTheme);
    expect(resolveAppTheme(null)).toBe(lightTheme);
    expect(resolveAppTheme(undefined)).toBe(lightTheme);
  });
});

// --- Zai token values, withAlpha, contrast (M1-A) ---

import { withAlpha } from '../theme';

describe('withAlpha', () => {
  it('expands #rgb and #rrggbb to rgba()', () => {
    expect(withAlpha('#000', 0.5)).toBe('rgba(0, 0, 0, 0.5)');
    expect(withAlpha('#d4d4d4', 0.6)).toBe('rgba(212, 212, 212, 0.6)');
  });

  it('throws on anything else', () => {
    expect(() => withAlpha('red', 0.5)).toThrow();
    expect(() => withAlpha('#12345', 0.5)).toThrow();
    expect(() => withAlpha('#1234567', 0.5)).toThrow();
    expect(() => withAlpha('123456', 0.5)).toThrow();
  });
});

describe('Zai token values', () => {
  it('light theme matches the Zai Light table', () => {
    expect(lightTheme.background).toBe('#f8f8f8');
    expect(lightTheme.card).toBe('#ffffff');
    expect(lightTheme.primary).toBe('#000000');
    expect(lightTheme.primaryForeground).toBe('#ffffff');
    expect(lightTheme.status.danger).toBe('#c42b2b');
    expect(lightTheme.statusDot.danger).toBe('#e03131');
    expect(lightTheme.secondary).toBe('#e6e6e6');
    expect(lightTheme.waiting.surface).toBe('#eaf7ee');
  });

  it('dark theme matches the Zai Dark table', () => {
    expect(darkTheme.background).toBe('#161616');
    expect(darkTheme.card).toBe('#2b2b2b');
    expect(darkTheme.primary).toBe('#ffffff');
    expect(darkTheme.primaryForeground).toBe('#000000');
    expect(darkTheme.status.danger).toBe('#ff5c5c');
    expect(darkTheme.statusDot.danger).toBe('#ff5c5c');
    expect(darkTheme.secondary).toBe('#363636');
    expect(darkTheme.waiting.surface).toBe('rgba(70, 191, 114, 0.16)');
  });

  it('contains no teal from the old palette', () => {
    expect(JSON.stringify(lightTheme)).not.toContain('#0c6e6e');
    expect(JSON.stringify(lightTheme)).not.toContain('#51fbfd');
    expect(JSON.stringify(darkTheme)).not.toContain('#0c6e6e');
    expect(JSON.stringify(darkTheme)).not.toContain('#51fbfd');
  });

  it('radius scale is the Zai scale', () => {
    expect(lightTheme.radius).toEqual({ sm: 4, md: 6, lg: 8, xl: 12, '2xl': 16, full: 999 });
    expect(darkTheme.radius).toEqual(lightTheme.radius);
  });

  it('surface aliases foregroundSubtle === foregroundMuted', () => {
    for (const theme of [lightTheme, darkTheme]) {
      expect(theme.surface).toEqual([theme.background, theme.card, theme.surfaceSubtle, theme.accent]);
      expect(theme.foregroundMuted).toBe(theme.foregroundSubtle);
    }
  });
});

/** Parse '#rrggbb' or 'rgba(r, g, b, a)' to [r, g, b, a?]. */
function parseColor(color: string): [number, number, number] | [number, number, number, number] {
  const rgb = color.match(/^rgb\((\d+),\s*(\d+),\s*(\d+)\)$/);
  if (rgb) return [Number(rgb[1]), Number(rgb[2]), Number(rgb[3])];
  const rgba = color.match(/^rgba\((\d+),\s*(\d+),\s*(\d+),\s*([\d.]+)\)$/);
  if (rgba) return [Number(rgba[1]), Number(rgba[2]), Number(rgba[3]), Number(rgba[4])];
  const hex = color.match(/^#([0-9a-fA-F]{6})$/);
  if (!hex) throw new Error(`unparseable color: ${color}`);
  return [parseInt(hex[1].slice(0, 2), 16), parseInt(hex[1].slice(2, 4), 16), parseInt(hex[1].slice(4, 6), 16)];
}

/** Blend an (optionally alpha) color over an opaque background. */
function blend(color: string, background: string): [number, number, number] {
  const [r, g, b, a] = parseColor(color) as [number, number, number, number?];
  const [br, bg, bb] = parseColor(background) as [number, number, number];
  if (a === undefined) return [r, g, b];
  return [Math.round(r * a + br * (1 - a)), Math.round(g * a + bg * (1 - a)), Math.round(b * a + bb * (1 - a))];
}

function luminance([r, g, b]: [number, number, number]): number {
  const lin = (c: number) => {
    const s = c / 255;
    return s <= 0.04045 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

function contrast(fg: string, bg: string): number {
  const l1 = luminance(blend(fg, bg));
  const l2 = luminance(blend(bg, bg));
  const [hi, lo] = l1 >= l2 ? [l1, l2] : [l2, l1];
  return (hi + 0.05) / (lo + 0.05);
}

describe('WCAG contrast (Zai tokens)', () => {
  const ratios: string[] = [];

  it('foreground >= 4.5 on background and card', () => {
    for (const theme of [lightTheme, darkTheme]) {
      for (const bg of [theme.background, theme.card]) {
        const r = contrast(theme.foreground, bg);
        ratios.push(`${theme.mode} foreground/${bg === theme.background ? 'background' : 'card'} ${r.toFixed(2)}`);
        expect(r).toBeGreaterThanOrEqual(4.5);
      }
    }
  });

  it('foregroundSubtle >= 4.5 on background and card', () => {
    for (const theme of [lightTheme, darkTheme]) {
      for (const bg of [theme.background, theme.card]) {
        const r = contrast(theme.foregroundSubtle, bg);
        ratios.push(`${theme.mode} foregroundSubtle/${bg === theme.background ? 'background' : 'card'} ${r.toFixed(2)}`);
        expect(r).toBeGreaterThanOrEqual(4.5);
      }
    }
  });

  it('each status.* >= 4.5 on background and card', () => {
    for (const theme of [lightTheme, darkTheme]) {
      for (const [name, value] of Object.entries(theme.status)) {
        for (const bg of [theme.background, theme.card]) {
          const r = contrast(value, bg);
          ratios.push(`${theme.mode} status.${name}/${bg === theme.background ? 'background' : 'card'} ${r.toFixed(2)}`);
          expect(r).toBeGreaterThanOrEqual(4.5);
        }
      }
    }
  });

  it('waiting.foreground >= 4.5 on waiting.surface blended over background', () => {
    for (const theme of [lightTheme, darkTheme]) {
      const blended = blend(theme.waiting.surface, theme.background);
      const blendedCss = `rgb(${blended[0]}, ${blended[1]}, ${blended[2]})`;
      const r = contrast(theme.waiting.foreground, blendedCss);
      ratios.push(`${theme.mode} waiting.foreground/waiting.surface-over-background ${r.toFixed(2)}`);
      expect(r).toBeGreaterThanOrEqual(4.5);
    }
  });

  it('primaryForeground >= 7 on primary', () => {
    for (const theme of [lightTheme, darkTheme]) {
      const r = contrast(theme.primaryForeground, theme.primary);
      ratios.push(`${theme.mode} primaryForeground/primary ${r.toFixed(2)}`);
      expect(r).toBeGreaterThanOrEqual(7);
    }
  });

  it('foregroundSubtlest >= 2 on background (placeholders-only tier)', () => {
    // Placeholder tier: 2:1 is accepted because placeholders are non-essential text.
    for (const theme of [lightTheme, darkTheme]) {
      const r = contrast(theme.foregroundSubtlest, theme.background);
      ratios.push(`${theme.mode} foregroundSubtlest/background ${r.toFixed(2)}`);
      expect(r).toBeGreaterThanOrEqual(2);
    }
  });

  it('records measured ratios', () => {
    console.log('CONTRAST RATIOS:\n' + ratios.join('\n'));
  });
});

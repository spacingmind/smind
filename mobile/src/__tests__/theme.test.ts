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

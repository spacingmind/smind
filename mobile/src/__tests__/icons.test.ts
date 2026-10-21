// icons.test.ts keeps the icon registry honest (mirrors web's lib/icons.ts
// drift test): (a) every entry is fully specified, (b) every android name
// exists as a @expo/material-symbols XML asset, (c) AppIcon.tsx's NATIVE map
// covers exactly the registry (the @expo/ui Babel plugin needs literal
// Icon.select call sites, so the map is hand-written and can drift), and
// (d) no consumer names an icon outside the registry. Pure node -- no
// react-native import.

import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { ICONS, IconName } from '../ui/icons';

const ROOT = join(__dirname, '..', '..');
const ICONS_ENTRIES = Object.entries(ICONS) as Array<[IconName, { ios: string; android: string; glyph: string }]>;

describe('icon registry drift', () => {
  it('every registry entry has non-empty ios, android and glyph', () => {
    for (const [name, spec] of ICONS_ENTRIES) {
      expect(spec.ios.length, `${name}.ios`).toBeGreaterThan(0);
      expect(spec.android.length, `${name}.android`).toBeGreaterThan(0);
      expect(spec.glyph.length, `${name}.glyph`).toBeGreaterThan(0);
    }
  });

  it('every android name has a @expo/material-symbols XML asset', () => {
    for (const [name, spec] of ICONS_ENTRIES) {
      expect(
        existsSync(join(ROOT, 'node_modules', '@expo', 'material-symbols', 'icons', `${spec.android}.xml`)),
        `${name}: @expo/material-symbols/icons/${spec.android}.xml is missing`,
      ).toBe(true);
    }
  });

  it('AppIcon.tsx NATIVE map covers exactly the registry names', () => {
    const source = readFileSync(join(ROOT, 'src', 'ui', 'AppIcon.tsx'), 'utf-8');
    const nativeKeys = new Set<string>(
      [...source.matchAll(/^\s{2}(\w+): Icon\.select/gm)].map((m) => m[1] as string),
    );
    const registryKeys = new Set<string>(ICONS_ENTRIES.map(([name]) => name as string));
    expect([...registryKeys].filter((k) => !nativeKeys.has(k)), 'missing from NATIVE').toEqual([]);
    expect([...nativeKeys].filter((k) => !registryKeys.has(k)), 'in NATIVE but not the registry').toEqual([]);
  });

  it('every material-symbols import in AppIcon.tsx matches a registry android name', () => {
    const source = readFileSync(join(ROOT, 'src', 'ui', 'AppIcon.tsx'), 'utf-8');
    const imports = [...source.matchAll(/@expo\/material-symbols\/([\w.]+)\.xml/g)].map((m) => m[1]);
    expect(imports.length).toBe(ICONS_ENTRIES.length);
    const androidNames = new Set<string>(ICONS_ENTRIES.map(([, spec]) => spec.android as string));
    for (const name of imports) {
      expect(androidNames.has(name), `imported ${name}.xml is not a registry android name`).toBe(true);
    }
  });

  it('every <AppIcon name="..."> literal names a registry icon', () => {
    // Recursive .tsx scan of src/ and app/ (skipping __tests__).
    const files: string[] = [];
    const walk = (dir: string) => {
      for (const entry of readdirSync(dir, { withFileTypes: true })) {
        const full = join(dir, entry.name);
        if (entry.isDirectory()) {
          if (entry.name === '__tests__' || entry.name === 'node_modules') continue;
          walk(full);
        } else if (entry.name.endsWith('.tsx')) {
          files.push(full);
        }
      }
    };
    walk(join(ROOT, 'src'));
    walk(join(ROOT, 'app'));

    const namesUsed = (source: string) => [...source.matchAll(/<AppIcon name="([\w-]+)"/g)].map((m) => m[1]);
    const registry = new Set(ICONS_ENTRIES.map(([name]) => name));
    for (const file of files) {
      for (const used of namesUsed(readFileSync(file, 'utf-8'))) {
        expect(registry.has(used as IconName), `${file}: <AppIcon name="${used}"> is not in the registry`).toBe(true);
      }
    }
    // Prove the scan flags an unknown literal (no consumers exist yet).
    expect(namesUsed('<AppIcon name="nope" />')).toEqual(['nope']);
  });
});

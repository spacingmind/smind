/// <reference types="node" />
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const pkgSrc = join(__dirname, '..', '..', '..', 'web', 'packages', 'relay-client', 'src');
const relayDir = join(__dirname, '..', 'relay');

// Derived from the package, so the list never goes stale.
const moved = readdirSync(pkgSrc)
  .filter((n) => n.endsWith('.ts') && n !== 'index.ts')
  .map((n) => n.replace(/\.ts$/, ''))
  .sort();

describe('mobile relay shims', () => {
  it('mobile_imports_the_shared_package_only', () => {
    expect(moved.length).toBeGreaterThan(10);
    for (const name of moved) {
      const p = join(relayDir, `${name}.ts`);
      expect(existsSync(p), `${name}.ts shim is missing`).toBe(true);
      expect(readFileSync(p, 'utf8').trimEnd(), name).toBe(`export * from '@smind/relay-client/src/${name}';`);
    }
    // No other copy of a moved module name anywhere under mobile/src (tests and helpers included).
    const copies: string[] = [];
    const walk = (dir: string) => {
      for (const e of readdirSync(dir, { withFileTypes: true })) {
        const p = join(dir, e.name);
        if (e.isDirectory()) walk(p);
        else if (/\.tsx?$/.test(e.name) && moved.includes(e.name.replace(/\.tsx?$/, '')) && dir !== relayDir) copies.push(p);
      }
    };
    walk(join(__dirname, '..'));
    expect(copies).toEqual([]);
  });
});

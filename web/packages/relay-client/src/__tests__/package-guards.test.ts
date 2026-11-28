/// <reference types="node" />
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';

const srcDir = join(__dirname, '..');
const repoRoot = join(__dirname, '..', '..', '..', '..', '..');

// Code only: comments may name the things the guard forbids.
const code = (f: string) => readFileSync(f, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');

describe('relay client package guards', () => {
  it('relay_client_package_has_no_platform_imports', () => {
    // Every source file under src/ except the tests (src/__tests__ is skipped).
    const files: string[] = [];
    const walk = (dir: string) => {
      for (const e of readdirSync(dir, { withFileTypes: true })) {
        if (e.name === '__tests__' || e.name === 'node_modules') continue;
        const p = join(dir, e.name);
        if (e.isDirectory()) walk(p);
        else if (/\.(ts|tsx)$/.test(e.name)) files.push(p);
      }
    };
    walk(srcDir);
    expect(files.length).toBeGreaterThan(10);
    const importOf = /(?:from|import|require\()\s*['"](react|react-native|expo|expo-[^'"]*|react\/[^'"]*|react-native\/[^'"]*)['"]/;
    const banned = [/\bwindow\./, /\bdocument\./, /\bnavigator\./, /\blocalStorage\b/, /\bsessionStorage\b/, /\bAsyncStorage\b/, /\bPlatform\./, /\bBuffer\b/, /\bprocess\./, /\brequire\(/, /(?:from|import)\s*\(?\s*['"]node:/];
    const specifiers = (c: string) => [...c.matchAll(/(?:from|import)\s*\(?\s*['"]([^'"]+)['"]/g)].map((m) => m[1]);
    const leavesPackage = (f: string, c: string) =>
      specifiers(c).some((sp) => sp.startsWith('.') && relative(srcDir, resolve(dirname(f), sp)).startsWith('..'));
    const bad = files.filter((f) => {
      const c = code(f);
      return importOf.test(c) || banned.some((re) => re.test(c)) || leavesPackage(f, c);
    });
    expect(bad).toEqual([]);
  });

  it('vectors_match_go_v2', () => {
    const vectors = join(repoRoot, 'internal', 'relay', 'e2ee', 'testdata', 'e2ee_v2_vectors.json');
    expect(existsSync(vectors), 'the Go v2 vector file must exist').toBe(true);
    expect(readFileSync(join(__dirname, 'e2ee.test.ts'), 'utf8')).toContain('e2ee_v2_vectors.json');
  });

  it('relay_client_noble_ranges_match_mobile', () => {
    const pkg = JSON.parse(readFileSync(join(__dirname, '..', '..', 'package.json'), 'utf8'));
    const mobile = JSON.parse(readFileSync(join(repoRoot, 'mobile', 'package.json'), 'utf8'));
    for (const name of ['@noble/ciphers', '@noble/curves', '@noble/hashes']) {
      expect(pkg.dependencies[name], `${name} in the package`).toBeDefined();
      expect(pkg.dependencies[name], name).toBe(mobile.dependencies[name]);
    }
  });
});

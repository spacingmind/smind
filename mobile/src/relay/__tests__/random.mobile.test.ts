/// <reference types="node" />
import { randomFillSync } from 'node:crypto';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { installRandomSource } from '../random';

const nodeSource = (bytes: Uint8Array) => {
  randomFillSync(bytes);
};

describe('platformRandom', () => {
  afterEach(() => {
    vi.doUnmock('expo-crypto');
    vi.resetModules();
    installRandomSource(nodeSource);
  });

  it('installs expo-crypto getRandomValues (not getRandomBytes) as the source', async () => {
    const getRandomValues = vi.fn((a: Uint8Array) => {
      a.fill(0xab);
      return a;
    });
    const getRandomBytes = vi.fn();
    vi.resetModules();
    vi.doMock('expo-crypto', () => ({ getRandomValues, getRandomBytes }));
    const random = await import('../random');
    random.installRandomSource(null);
    await import('../platformRandom');
    expect(Array.from(random.randomBytes(32))).toEqual(new Array(32).fill(0xab));
    expect(getRandomValues).toHaveBeenCalledTimes(1);
    expect(getRandomBytes).not.toHaveBeenCalled();
  });

  it('a missing native module surfaces as CsprngUnavailableError, not weak bytes', async () => {
    vi.resetModules();
    vi.doMock('expo-crypto', () => ({
      getRandomValues: () => {
        throw new Error("Cannot find native module 'ExpoCrypto'");
      },
    }));
    const random = await import('../random');
    random.installRandomSource(null);
    await import('../platformRandom');
    expect(() => random.randomBytes(32)).toThrow(random.CsprngUnavailableError);
  });
});

// Drift guard: every random byte in the relay client comes from randomBytes.
describe('relay sources use only the explicit CSPRNG', () => {
  const srcRoot = join(__dirname, '..', '..');
  const files: string[] = [];
  const walk = (dir: string) => {
    for (const name of readdirSync(dir)) {
      const p = join(dir, name);
      if (name === '__tests__' || name === 'node_modules') continue;
      if (statSync(p).isDirectory()) walk(p);
      else if (/\.(ts|tsx)$/.test(name)) files.push(p);
    }
  };
  walk(srcRoot);
  walk(join(srcRoot, '..', 'scripts'));
  walk(join(srcRoot, '..', 'app')); // the Expo Router routes (ADR-0036) replaced App.tsx
  files.push(join(srcRoot, '..', 'index.ts'));

  // Comments may name the things this guard forbids; only code counts.
  const code = (f: string) => readFileSync(f, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');

  const forbidden: [RegExp, string][] = [
    [/\bcrypto\.getRandomValues\b/, 'the implicit global crypto.getRandomValues'],
    [/\brandomSecretKey\b/, 'noble randomSecretKey (reads the global crypto)'],
    [/\butils\.randomBytes\b|\brandomBytes\s*\(\s*\)/, 'noble randomBytes'],
    [/@noble\/[a-z/.]*\/utils\.js/, 'noble utils (randomBytes lives there)'],
    [/\brandomUUID\b/, 'crypto.randomUUID'],
  ];

  it.each(forbidden)('no source uses %s (%s)', (re) => {
    const hits = files.filter((f) => re.test(code(f)));
    expect(hits.map((f) => relative(srcRoot, f))).toEqual([]);
  });

  it('Math.random appears only as the retry-backoff jitter', () => {
    const hits = files.filter((f) => /\bMath\.random\b/.test(code(f))).map((f) => relative(srcRoot, f));
    expect(hits).toEqual([join('relay', 'RelayConnection.ts')]);
    const lines = code(join(srcRoot, 'relay', 'RelayConnection.ts')).split('\n').filter((l) => l.includes('Math.random'));
    expect(lines).toHaveLength(1);
    expect(lines[0]).toMatch(/ms \* \(0\.8 \+ Math\.random\(\) \* 0\.4\)/);
  });

  it('expo-crypto is imported only by platformRandom.ts, and index.ts imports it first', () => {
    const hits = files.filter((f) => /from 'expo-crypto'/.test(code(f))).map((f) => relative(srcRoot, f));
    expect(hits).toEqual([join('relay', 'platformRandom.ts')]);
    const index = readFileSync(join(srcRoot, '..', 'index.ts'), 'utf8');
    const firstImport = index.split('\n').find((l) => l.startsWith('import '));
    expect(firstImport).toBe("import './src/relay/platformRandom';");
  });
});

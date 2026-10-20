/// <reference types="node" />
import { randomFillSync } from 'node:crypto';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CsprngUnavailableError, installRandomSource, MAX_RANDOM_BYTES, randomBytes } from '../random';

const nodeSource = (bytes: Uint8Array) => {
  randomFillSync(bytes);
};

describe('randomBytes', () => {
  beforeEach(() => installRandomSource(null));
  afterEach(() => installRandomSource(nodeSource));

  it('fails closed with a clear error when no source is installed', () => {
    expect(() => randomBytes(32)).toThrow(CsprngUnavailableError);
    expect(() => randomBytes(32)).toThrow(/none installed/);
  });

  it('does not fall back to a global crypto or Math.random when nothing is installed', () => {
    const getRandomValues = vi.fn((a: Uint8Array) => a);
    vi.stubGlobal('crypto', { getRandomValues });
    const mathRandom = vi.spyOn(Math, 'random');
    try {
      expect(() => randomBytes(32)).toThrow(CsprngUnavailableError);
      expect(getRandomValues).not.toHaveBeenCalled();
      expect(mathRandom).not.toHaveBeenCalled();
    } finally {
      vi.unstubAllGlobals();
      mathRandom.mockRestore();
    }
  });

  it('returns exactly n bytes from the installed source, fresh each call', () => {
    installRandomSource(nodeSource);
    const a = randomBytes(32);
    const b = randomBytes(32);
    expect(a).toHaveLength(32);
    expect(Buffer.from(a).equals(Buffer.from(b))).toBe(false);
  });

  it('hands the source a zeroed array of the requested size and returns what it filled', () => {
    const seen: number[] = [];
    installRandomSource((bytes) => {
      seen.push(bytes.length, bytes.reduce((x, y) => x + y, 0));
      bytes.fill(7);
    });
    expect(Array.from(randomBytes(20))).toEqual(new Array(20).fill(7));
    expect(seen).toEqual([20, 0]);
  });

  it('wraps a throwing source as CsprngUnavailableError, keeping the cause', () => {
    const boom = new Error('native module missing');
    installRandomSource(() => {
      throw boom;
    });
    let caught: unknown;
    try {
      randomBytes(32);
    } catch (e) {
      caught = e;
    }
    expect(caught).toBeInstanceOf(CsprngUnavailableError);
    expect((caught as Error).message).toMatch(/native module missing/);
    expect((caught as Error).cause).toBe(boom);
  });

  it('rejects a source that leaves 16 or more bytes zero', () => {
    installRandomSource(() => {});
    expect(() => randomBytes(32)).toThrow(/only zero bytes/);
    expect(() => randomBytes(16)).toThrow(/only zero bytes/);
  });

  it.each([0, -1, 1.5, NaN, MAX_RANDOM_BYTES + 1])('rejects the size %s', (n) => {
    installRandomSource(nodeSource);
    expect(() => randomBytes(n)).toThrow(RangeError);
  });
});

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
  files.push(join(srcRoot, '..', 'App.tsx'), join(srcRoot, '..', 'index.ts'));

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

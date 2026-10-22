import { readFileSync, readdirSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';

const FORBIDDEN = [
  'getDevicePushTokenAsync',
  'contentAvailable',
  'content-available',
  'console.log',
  'console.warn',
  'console.error',
  "from 'react-native'",
  "from 'expo-",
];

describe('push logic', () => {
  it('push logic is pure and never touches device tokens or the console', () => {
    const dir = join(dirname(fileURLToPath(import.meta.url)), '..');
    const files = readdirSync(dir).filter((f) => f.endsWith('.ts') && statSync(join(dir, f)).isFile());
    expect(files.length).toBeGreaterThan(0);
    const hits: string[] = [];
    for (const f of files) {
      const src = readFileSync(join(dir, f), 'utf8');
      for (const s of FORBIDDEN) if (src.includes(s)) hits.push(`${f}: ${s}`);
    }
    expect(hits).toEqual([]);
  });
});

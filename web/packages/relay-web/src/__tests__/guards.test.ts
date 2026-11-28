import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const SRC = join(__dirname, '..');

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    if (n === '__tests__') return [];
    return statSync(p).isDirectory() ? files(p) : p.endsWith('.ts') ? [p] : [];
  });
}

const stripComments = (s: string) =>
  s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:'"`\\])\/\/.*$/gm, '$1');

describe('guards', () => {
  it('relay_web_has_no_extractable_key_or_logging', () => {
    const banned: Array<[string, RegExp]> = [
      ['extractable true', /extractable\s*:\s*true/],
      ['exportKey of a non-raw format', /exportKey\(\s*(?!['"]raw['"])/],
      ['pkcs8', /pkcs8/],
      ['jwk', /\bjwk\b/i],
      ['console', /console\./],
      ['localStorage', /localStorage/],
      ['sessionStorage', /sessionStorage/],
      ['document.cookie', /document\.cookie/],
      ['eval', /\beval\(/],
      ['new Function', /new Function/],
    ];
    const all = files(SRC);
    expect(all.length).toBeGreaterThan(3);
    const hits: string[] = [];
    for (const f of all) {
      const text = stripComments(readFileSync(f, 'utf8'));
      for (const [name, re] of banned) if (re.test(text)) hits.push(`${f}: ${name}`);
    }
    expect(hits).toEqual([]);
  });
});

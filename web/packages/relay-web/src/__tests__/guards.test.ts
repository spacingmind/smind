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

/** The top-level comma-separated arguments of every `generateKey(` call in text. */
function generateKeyCalls(text: string): string[][] {
  const calls: string[][] = [];
  const re = /generateKey\s*\(/g;
  for (let m = re.exec(text); m; m = re.exec(text)) {
    const args: string[] = [];
    let depth = 1;
    let cur = '';
    let i = m.index + m[0].length;
    for (; i < text.length && depth > 0; i++) {
      const c = text[i];
      if (c === '(' || c === '[' || c === '{') depth++;
      else if (c === ')' || c === ']' || c === '}') depth--;
      if (depth === 0) break;
      if (c === ',' && depth === 1) {
        args.push(cur.trim());
        cur = '';
      } else cur += c;
    }
    if (cur.trim() !== '') args.push(cur.trim());
    calls.push(args);
  }
  return calls;
}

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

  it('generate_key_extractable_argument_is_the_literal_false', () => {
    let calls = 0;
    const bad: string[] = [];
    for (const f of files(SRC)) {
      for (const args of generateKeyCalls(stripComments(readFileSync(f, 'utf8')))) {
        calls++;
        if (args[1] !== 'false') bad.push(`${f}: generateKey(${args.join(', ')})`);
      }
    }
    expect(calls).toBeGreaterThan(0);
    expect(bad).toEqual([]);
  });
});

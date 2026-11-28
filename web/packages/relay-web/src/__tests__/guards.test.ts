import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { scanSource, type Violation } from './scan';

const SRC = join(__dirname, '..');

function files(dir: string): string[] {
  return readdirSync(dir).flatMap((n) => {
    const p = join(dir, n);
    if (n === '__tests__') return [];
    return statSync(p).isDirectory() ? files(p) : p.endsWith('.ts') ? [p] : [];
  });
}

const scanReal = (): Violation[] => files(SRC).flatMap((f) => scanSource(f, readFileSync(f, 'utf8')));
const show = (v: Violation[]) => v.map((x) => `${x.file}:${x.line}: ${x.rule}: ${x.detail}`);
const only = (v: Violation[], rules: string[]) => v.filter((x) => rules.includes(x.rule));

describe('guards', () => {
  it('relay_web_has_no_extractable_key_or_logging', () => {
    expect(files(SRC).length).toBeGreaterThan(3);
    const v = only(scanReal(), ['forbidden-name', 'node-module', 'extractable-true', 'key-format', 'export-key-format']);
    expect(show(v)).toEqual([]);
  });

  it('generate_key_extractable_argument_is_the_literal_false', () => {
    // the scan must have something to look at
    expect(readFileSync(join(SRC, 'identity.ts'), 'utf8')).toContain('.generateKey(');
    const v = only(scanReal(), ['generate-key-extractable', 'crypto-reference', 'crypto-forbidden']);
    expect(show(v)).toEqual([]);
  });

  it('no_computed_access_or_forbidden_strings', () => {
    expect(show(only(scanReal(), ['computed-access', 'forbidden-string']))).toEqual([]);
  });

  it('guard_scanner_catches_known_bypasses', () => {
    const bypasses: Array<[string, string]> = [
      ["const s = 'a//b'; console.log(s);", 'forbidden-name'],
      ['const l = console; l.log(x);', 'forbidden-name'],
      ["globalThis['local' + 'Storage'].setItem('a', b);", 'computed-access'],
      ["globalThis['con' + 'sole'].log(x);", 'computed-access'],
      ["window['localStorage'];", 'computed-access'],
      ['subtle.generateKey.bind(subtle)(a, false, b);', 'crypto-reference'],
      ['const g = subtle.generateKey;', 'crypto-reference'],
      ['const { generateKey } = subtle;', 'crypto-reference'],
      ["subtle['generate' + 'Key'](a, !0, b);", 'computed-access'],
      ['subtle.generateKey(a, !0, b);', 'generate-key-extractable'],
      ['subtle.generateKey(a, true, b);', 'generate-key-extractable'],
      ['subtle.generateKey(a, flag, b);', 'generate-key-extractable'],
      ['(subtle.generateKey)(a, false, b);', 'crypto-reference'],
      ["subtle.exportKey('jwk', k);", 'export-key-format'],
      ["subtle.exportKey('pkcs8', k);", 'export-key-format'],
      ['subtle.exportKey(fmt, k);', 'export-key-format'],
      ["subtle.importKey('raw', k, a, false, []);", 'crypto-forbidden'],
      ['subtle.deriveBits(a, k, 8);', 'crypto-forbidden'],
      ["import fs from 'node:fs';", 'node-module'],
      ["const m = await import('node:fs');", 'node-module'],
      ['new Function("return 1");', 'forbidden-name'],
      ['eval(code);', 'forbidden-name'],
      ['document.cookie = x;', 'forbidden-name'],
      ['navigator.sendBeacon(u, d);', 'forbidden-name'],
      ["const k = 'generateKey';", 'forbidden-string'],
      ['const o = { extractable: true };', 'extractable-true'],
      ["const f = 'jwk';", 'key-format'],
    ];
    for (const [src, rule] of bypasses) {
      const got = scanSource('x.ts', src).map((v) => v.rule);
      expect(got, src).toContain(rule);
    }

    const clean = [
      "subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify']);",
      "await this.o.subtle.generateKey({ name: 'Ed25519' }, false, ['sign']);",
      "subtle.exportKey('raw', k);",
      '// console.log and localStorage and eval( in a comment\nconst a = 1;',
      '/* document.cookie, subtle.exportKey(jwk) */ const b = 2;',
      "const s = 'a//b'; const t = s.length;",
      "import { x } from './y';",
      'const c = crypto.subtle;',
    ];
    for (const src of clean) expect(show(scanSource('x.ts', src)), src).toEqual([]);
  });
});

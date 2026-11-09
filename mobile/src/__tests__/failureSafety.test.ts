// failureSafety.test.ts (R3a AC7): the mobile failure wording must never
// read as an approval -- no sentence or label of the failure classes may
// say merge, approve or always-allow. Source scan in stewardSafety.test.ts's
// style: read the sources, strip comments, then check the string literals.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const ROOT = join(__dirname, '..', '..');
const SOURCES = [join(ROOT, 'src', 'failureText.ts'), join(ROOT, 'src', 'runFooter.ts')];

const strip = (s: string) => s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');
/** Every string literal of a comment-free source ('...', "..." or `...`). */
const literals = (s: string): string[] =>
  [...s.matchAll(/'([^'\\\n]|\\.)*'|"(?:[^"\\\n]|\\.)*"|`(?:[^`\\]|\\.)*`/g)].map((m) => m[0]);
const UNSAFE = /merge|approve|always[ _-]?allow/i;

describe('failure wording safety (R3a AC7)', () => {
  it('no failure sentence or label says merge, approve or always-allow', () => {
    for (const f of SOURCES) {
      const found = literals(strip(readFileSync(f, 'utf8'))).filter((l) => UNSAFE.test(l));
      expect(found, f).toEqual([]);
    }
  });

  it('the scanner catches a planted always-allow literal', () => {
    expect(literals(strip('const a = "Always allow"; // merge'))).toEqual(['"Always allow"']);
    expect(literals(strip('const a = "Always allow"; // merge')).some((l) => UNSAFE.test(l))).toBe(true);
    // A comment alone never trips it: only string literals are read.
    expect(literals(strip('// approve the merge\nconst a = 1;')).some((l) => UNSAFE.test(l))).toBe(false);
  });
});

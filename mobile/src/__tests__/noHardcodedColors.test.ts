// noHardcodedColors.test.ts mirrors web's no-hardcoded-colors.test.ts:
// every UI-bearing file must contain zero hex color literals of its own --
// every color comes from useAppTheme(), so light/dark stay correct without
// touching component code. The scan is recursive over mobile/src/screens,
// mobile/src/ui and mobile/app (skipping __tests__), so new files in M2-M4
// are covered without editing this test again.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const HEX_LITERAL = /#[0-9a-fA-F]{3,8}\b/g;

/** Recursive .ts/.tsx scan under a directory, skipping __tests__ subtrees. */
function discover(dir: string): string[] {
  const found: string[] = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      if (entry === '__tests__') continue;
      found.push(...discover(full));
    } else if (/\.(ts|tsx)$/.test(entry)) {
      found.push(full);
    }
  }
  return found;
}

// Strip line and block comments so a comment may mention a colour in prose.
function stripComments(source: string): string {
  return source
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(^|[^:])\/\/[^\n]*/g, '$1');
}

const ROOT = join(__dirname, '..', '..');
const FILES = ['src/screens', 'src/ui', 'app'].flatMap((dir) => discover(join(ROOT, dir)));

describe('UI files use the token system, not hardcoded hex colors', () => {
  expect(FILES.length).toBeGreaterThan(0);
  for (const full of FILES) {
    const relativePath = full.slice(ROOT.length + 1);
    it(`${relativePath} contains no hex color literal`, () => {
      const contents = stripComments(readFileSync(full, 'utf-8'));
      expect(contents.match(HEX_LITERAL)).toBeNull();
    });
  }
});

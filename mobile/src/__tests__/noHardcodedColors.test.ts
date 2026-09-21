// noHardcodedColors.test.ts mirrors web's no-hardcoded-colors.test.ts:
// every screen rebuilt on the token system (mobile-ui-polish plan) must
// contain zero hex/rgb color literals of its own -- every color comes
// from useAppTheme(), so light/dark stay correct without touching
// component code. Extended with one path per item as each screen lands.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const HEX_LITERAL = /#[0-9a-fA-F]{3,8}\b/g;

const SCREENS_REBUILT_ON_TOKENS = ['src/screens/PairingScreen.tsx', 'src/screens/TasksScreen.tsx'];

describe('screens rebuilt on the token system have no hardcoded hex literals', () => {
  for (const relativePath of SCREENS_REBUILT_ON_TOKENS) {
    it(`${relativePath} contains no hex color literal`, () => {
      const contents = readFileSync(join(__dirname, '..', '..', relativePath), 'utf-8');
      expect(contents.match(HEX_LITERAL)).toBeNull();
    });
  }
});

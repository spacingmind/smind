import { describe, expect, it } from 'vitest';
import { LONG_OPTION_LABEL_CHARS, isLongOptionLabel } from '../transcript/permissionLabels';

describe('isLongOptionLabel', () => {
  it('a short label is not long', () => {
    expect(isLongOptionLabel('Allow once')).toBe(false);
    expect(isLongOptionLabel('Deny')).toBe(false);
  });
  it("the daemon's long grant labels are long", () => {
    expect(
      isLongOptionLabel('Allow opening any page the browser policy allows (localhost and your added hosts) and closing tabs, for this run'),
    ).toBe(true);
    expect(
      isLongOptionLabel('Allow closing tabs, and opening any page the browser policy allows (localhost and your added hosts), for this run'),
    ).toBe(true);
  });
  it('the threshold is exclusive and ignores surrounding space', () => {
    const exactly = 'a'.repeat(LONG_OPTION_LABEL_CHARS);
    expect(isLongOptionLabel(exactly)).toBe(false);
    expect(isLongOptionLabel('a'.repeat(LONG_OPTION_LABEL_CHARS + 1))).toBe(true);
    expect(isLongOptionLabel(`  ${exactly}  `)).toBe(false);
  });
});

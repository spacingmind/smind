import { describe, expect, it } from 'vitest';
import { isHapticPlatform } from '../haptics';

describe('isHapticPlatform', () => {
  it('is true only on ios', () => {
    expect(isHapticPlatform('ios')).toBe(true);
    for (const os of ['android', 'web', 'macos', '', undefined]) expect(isHapticPlatform(os)).toBe(false);
  });
});

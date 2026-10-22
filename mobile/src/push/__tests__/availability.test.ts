import { describe, expect, it } from 'vitest';
import { pushAvailability } from '../availability';

describe('pushAvailability', () => {
  it('maps every platform and environment', () => {
    const table: [string, string | undefined, string][] = [
      ['web', undefined, 'web'],
      ['web', 'storeClient', 'web'],
      ['ios', 'standalone', 'ios_blocked'],
      ['ios', 'bare', 'ios_blocked'],
      ['android', 'standalone', 'available'],
      ['android', undefined, 'available'],
      ['android', 'storeClient', 'expo_go'],
      ['ios', 'storeClient', 'expo_go'],
      ['windows', undefined, 'web'],
    ];
    for (const [platform, executionEnvironment, want] of table) {
      expect(pushAvailability({ platform, executionEnvironment }), `${platform}/${executionEnvironment}`).toBe(want);
    }
  });
});

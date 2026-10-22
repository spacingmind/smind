import { describe, expect, it } from 'vitest';
import { foregroundBehavior } from '../foregroundBehavior';

const base = { appActive: true, connected: true, openTaskId: 5, eventTaskId: 5 };

describe('foregroundBehavior', () => {
  it('is suppressed for the open task on a live connection and shown otherwise', () => {
    expect(foregroundBehavior(base)).toEqual({ shouldShowBanner: false, shouldShowList: false, shouldPlaySound: false, shouldSetBadge: false });
    for (const patch of [{ eventTaskId: 6 }, { connected: false }, { eventTaskId: null }]) {
      expect(foregroundBehavior({ ...base, ...patch })).toEqual({
        shouldShowBanner: true,
        shouldShowList: true,
        shouldPlaySound: false,
        shouldSetBadge: false,
      });
    }
  });
});

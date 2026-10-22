import { describe, expect, it } from 'vitest';
import { ForegroundInput, shouldPresentInForeground } from '../foreground';

const base: ForegroundInput = { appActive: true, connected: true, openTaskId: 5, eventTaskId: 5 };

describe('shouldPresentInForeground', () => {
  const cases: [string, Partial<ForegroundInput>, boolean][] = [
    ['open task on a live connection is suppressed', {}, false],
    ['a different task is shown', { eventTaskId: 6 }, true],
    ['no event task id (generic fallback) is shown', { eventTaskId: null }, true],
    ['no open task is shown', { openTaskId: null }, true],
    ['app not active is shown', { appActive: false }, true],
    ['not connected is shown', { connected: false }, true],
    ['nothing open and no event task is shown', { openTaskId: null, eventTaskId: null }, true],
  ];
  for (const [name, patch, want] of cases) {
    it(name, () => {
      expect(shouldPresentInForeground({ ...base, ...patch })).toBe(want);
    });
  }
});

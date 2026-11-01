// composerInbox.test.ts: the one-slot inbox a plugin panel's prompt action
// puts text into and the composer takes it from. Only bounded plain text
// ever sits in the slot, and a take hands it over exactly once; nothing in
// the module can send anything.

import { describe, expect, it } from 'vitest';

import { MAX_INBOX_CHARS, peekPendingInsert, setPendingInsert, subscribePendingInsert, takePendingInsert } from '../plugins/composerInbox';

describe('composer inbox', () => {
  it('set then take returns once', () => {
    expect(takePendingInsert()).toBeNull(); // nothing waits: nothing to consume
    expect(setPendingInsert('Why did the suite slow down?')).toBe(true);
    expect(peekPendingInsert()).toBe('Why did the suite slow down?');
    expect(takePendingInsert()).toBe('Why did the suite slow down?');
    expect(takePendingInsert()).toBeNull(); // once: the slot is empty again
    expect(peekPendingInsert()).toBeNull();
  });

  it('empty, NUL and oversize are refused', () => {
    expect(setPendingInsert('held')).toBe(true);
    for (const bad of ['', '   \t\n ', 'a\u0000b', 'x'.repeat(MAX_INBOX_CHARS + 1)]) {
      expect(setPendingInsert(bad), JSON.stringify(bad.slice(0, 8))).toBe(false);
    }
    expect(takePendingInsert()).toBe('held'); // a refused set changed nothing
  });

  it('a later set replaces an earlier one', () => {
    setPendingInsert('first');
    expect(setPendingInsert('second')).toBe(true);
    expect(peekPendingInsert()).toBe('second'); // one slot, not a queue
    expect(takePendingInsert()).toBe('second');
  });

  it('subscribers hear changes', () => {
    const seen: (string | null)[] = [];
    const stop = subscribePendingInsert((text) => seen.push(text));
    setPendingInsert('one');
    takePendingInsert();
    expect(seen).toEqual(['one', null]); // after the set and after the take
    stop();
    setPendingInsert('two');
    takePendingInsert();
    expect(seen).toEqual(['one', null]); // stopped: no further calls
  });
});

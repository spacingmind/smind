// inboxApi.test.ts: the api module's method names and params, and degrade-to-empty on a malformed answer.

import { describe, expect, it } from 'vitest';
import { fetchInbox, fetchInboxCount, INBOX_METHODS, markInboxSeen, resolveInbox, snoozeInbox } from '../inbox/api';
import { fakeConnection, inboxItem } from './inboxFixtures';

describe('inbox api', () => {
  it('sends the documented method and params for each call', async () => {
    const { conn, calls } = fakeConnection((m) => (m === 'inbox.list' ? { items: [inboxItem()] } : m === 'inbox.count' ? { total: 2, unseen: 1, byKind: { verdict: 2 } } : { ok: true }));
    expect(await fetchInbox(conn)).toHaveLength(1);
    expect(await fetchInboxCount(conn)).toEqual({ total: 2, unseen: 1, byKind: { verdict: 2 } });
    await markInboxSeen(conn, ['verdict:1']);
    await snoozeInbox(conn, ['stall:3'], '2026-10-27T12:00:00Z');
    await resolveInbox(conn, ['verdict:1']);
    expect(calls).toEqual([
      ['inbox.list', {}],
      ['inbox.count', {}],
      ['inbox.seen', { keys: ['verdict:1'] }],
      ['inbox.snooze', { keys: ['stall:3'], until: '2026-10-27T12:00:00Z' }],
      ['inbox.resolve', { keys: ['verdict:1'] }],
    ]);
    expect(calls.every(([m]) => (INBOX_METHODS as readonly string[]).includes(m))).toBe(true);
  });

  it('a malformed answer degrades to empty', async () => {
    const { conn } = fakeConnection(() => null);
    expect(await fetchInbox(conn)).toEqual([]);
    expect(await fetchInboxCount(conn)).toEqual({ total: 0, unseen: 0, byKind: {} });
  });
});

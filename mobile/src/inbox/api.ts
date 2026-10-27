// api.ts is the inbox client surface of the mobile app: two reads and three row setters. A permission is only ever
// ANSWERED (run.respondPermission, from the task screen); the daemon refuses snooze/resolve on a "perm:" key, and the
// phone never sends one (inboxSafety.test.ts).

import { RelayConnection } from '../relay/RelayConnection';
import { InboxCount, InboxItem } from './types';

/** Every RPC this module may send. */
export const INBOX_METHODS = ['inbox.list', 'inbox.count', 'inbox.seen', 'inbox.snooze', 'inbox.resolve'] as const;

export async function fetchInbox(conn: RelayConnection): Promise<InboxItem[]> {
  const res = (await conn.call('inbox.list', {})) as { items?: InboxItem[] } | null;
  return Array.isArray(res?.items) ? res.items : [];
}

export async function fetchInboxCount(conn: RelayConnection): Promise<InboxCount> {
  const res = (await conn.call('inbox.count', {})) as Partial<InboxCount> | null;
  return {
    total: typeof res?.total === 'number' ? res.total : 0,
    unseen: typeof res?.unseen === 'number' ? res.unseen : 0,
    byKind: res?.byKind ?? {},
  };
}

export async function markInboxSeen(conn: RelayConnection, keys: string[]): Promise<void> {
  await conn.call('inbox.seen', { keys });
}

/** `until` is RFC 3339 and at most 24 h ahead (the daemon enforces it; inboxView caps the options). */
export async function snoozeInbox(conn: RelayConnection, keys: string[], until: string): Promise<void> {
  await conn.call('inbox.snooze', { keys, until });
}

export async function resolveInbox(conn: RelayConnection, keys: string[]): Promise<void> {
  await conn.call('inbox.resolve', { keys });
}

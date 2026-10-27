// inboxSubscription.test.ts: inbox.changed is subscribed and delivered; useInbox refetches on it and on reconnect.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { watchInboxChanged } from '../inbox/subscription';
import { makeRelayHarness } from '../relayHarness';

describe('inbox events', () => {
  it('subscribes to inbox.changed and calls back on every push', async () => {
    const { conn, daemon } = await makeRelayHarness();
    let n = 0;
    const off = watchInboxChanged(conn, () => n++);
    const [sub] = await daemon.waitForRequests(1);
    expect(sub.method).toBe('events.subscribe');
    expect(sub.params).toEqual({ topics: ['inbox.changed'] });
    daemon.reply(sub.id!, { topics: ['inbox.changed'] });
    daemon.pushNotification('inbox.changed', {});
    daemon.pushNotification('inbox.changed', {}, 2);
    await new Promise((r) => setTimeout(r, 10));
    expect(n).toBe(2);
    off();
  });
});

describe('useInbox wiring', () => {
  const src = readFileSync(join(__dirname, '..', 'inbox', 'useInbox.ts'), 'utf8');
  it('reloads on inbox.changed and refetches count and list on a new connection epoch', () => {
    expect(src).toMatch(/watchInboxChanged\(conn, reload\)/);
    expect(src).toMatch(/fetchInboxCount[\s\S]*\[conn, epoch, tick\]/);
    expect(src).toMatch(/fetchInbox\(conn\)[\s\S]*\[conn, withList, epoch, tick\]/);
  });
});

// automationsApi.test.ts: the api module's wire calls and the automation.* subscription (the client-mock style of
// pluginsReadOnly.test.ts).

import { describe, expect, it } from 'vitest';
import { disableAutomation, fetchAutomation, fetchAutomationAudit, fetchAutomations } from '../automations/api';
import { watchAutomationEvents } from '../automations/subscription';
import { makeRelayHarness } from '../relayHarness';
import { automation, auditRow, fakeConnection, listing } from './automationFixtures';

describe('automation api', () => {
  it('sends the documented params for each call', async () => {
    const { conn, calls } = fakeConnection((m) => (m === 'automation.list' ? listing([automation()]) : m === 'automation.audit' ? { rows: [auditRow()] } : m === 'automation.get' ? automation() : {}));
    expect((await fetchAutomations(conn)).automations).toHaveLength(1);
    await fetchAutomation(conn, 7);
    expect(await fetchAutomationAudit(conn, 7, 40, 10)).toHaveLength(1);
    await disableAutomation(conn, 7);
    expect(calls).toEqual([
      ['automation.list', undefined],
      ['automation.get', { id: 7 }],
      ['automation.audit', { id: 7, limit: 10, before: 40 }],
      ['automation.disable', { id: 7 }],
    ]);
  });

  it('a malformed answer degrades to empty rather than throwing', async () => {
    const { conn } = fakeConnection(() => null);
    expect(await fetchAutomations(conn)).toEqual({ enabled: false, paused: false, automations: [] });
    expect(await fetchAutomationAudit(conn, 1)).toEqual([]);
  });
});

describe('automation events', () => {
  it('subscribes to fired, paused and updated and delivers each', async () => {
    const { conn, daemon } = await makeRelayHarness();
    const seen: Array<{ topic: string; automationId: number }> = [];
    const off = watchAutomationEvents(conn, (e) => seen.push(e));
    const [sub] = await daemon.waitForRequests(1);
    expect(sub.method).toBe('events.subscribe');
    expect(sub.params).toEqual({ topics: ['automation.fired', 'automation.paused', 'automation.updated'] });
    daemon.reply(sub.id!, { topics: ['automation.fired', 'automation.paused', 'automation.updated'] });
    daemon.pushNotification('automation.paused', { automationId: 3, reason: 'budget' });
    daemon.pushNotification('automation.updated', { automationId: 4 }, 2);
    await new Promise((r) => setTimeout(r, 10));
    expect(seen).toEqual([
      { topic: 'automation.paused', automationId: 3 },
      { topic: 'automation.updated', automationId: 4 },
    ]);
    off();
  });
});

// automationsList.test.ts: the Automations list's states as data (off / empty / list / paused reason / error).

import { describe, expect, it } from 'vitest';
import { fetchAutomations } from '../automations/api';
import { AUTOMATIONS_EMPTY_TEXT, AUTOMATIONS_OFF_TEXT, AUTOMATIONS_PAUSED_TEXT, automationsListView, automationStatus, signalSummary } from '../automations/listView';
import { automation, fakeConnection, listing } from './automationFixtures';

describe('automationsListView', () => {
  it('global switch off: the read-only notice, no rows', () => {
    expect(automationsListView(listing([automation()], false))).toEqual({ kind: 'off', notice: AUTOMATIONS_OFF_TEXT });
    expect(AUTOMATIONS_OFF_TEXT).toBe('Automations are off on this daemon. Turn them on in config.yaml on the computer.');
  });

  it('empty state when on and none exist', () => {
    expect(automationsListView(listing([]))).toEqual({ kind: 'empty', text: AUTOMATIONS_EMPTY_TEXT, notice: null });
  });

  it('the global pause shows as a notice above the list', () => {
    const v = automationsListView(listing([automation()], true, true));
    expect(v.kind === 'list' && v.notice).toBe(AUTOMATIONS_PAUSED_TEXT);
  });

  it('status chip: disabled beats paused beats active', () => {
    expect(automationStatus({ enabled: true, status: 'active' }).label).toBe('Active');
    expect(automationStatus({ enabled: true, status: 'paused' }).label).toBe('Paused');
    expect(automationStatus({ enabled: false, status: 'active' }).label).toBe('Disabled');
    expect(automationStatus({ enabled: false, status: 'paused' }).label).toBe('Disabled');
  });

  it('signal summary: cron is UTC, a CI signal is "<workflow> on <branch>"', () => {
    expect(signalSummary({ kind: 'cron', cron: '17 19 * * *' })).toBe('17 19 * * * UTC');
    expect(signalSummary({ kind: 'ci', workflow: 'Windows full suite', branch: 'develop' })).toBe('Windows full suite on develop');
  });

  it('rows: name, chip, signal, last fired; a paused row carries pausedReason; Pause only for active ones', () => {
    const v = automationsListView(
      listing([
        automation({ id: 1, name: 'Nightly', lastFiredAt: '2026-10-21T19:17:30Z' }),
        automation({ id: 2, name: 'Suite', signal: { kind: 'ci', workflow: 'Windows full suite', branch: 'develop' }, status: 'paused', pausedReason: '3 consecutive failures' }),
        automation({ id: 3, name: 'Off', enabled: false, pausedReason: 'stale' }),
      ]),
    );
    expect(v.kind).toBe('list');
    const rows = v.kind === 'list' ? v.rows : [];
    expect(rows.map((r) => [r.id, r.name, r.status, r.signal, r.lastFired, r.pausedReason, r.canPause])).toEqual([
      [1, 'Nightly', 'Active', '17 19 * * * UTC', 'Last fired 2026-10-21 19:17 UTC', null, true],
      [2, 'Suite', 'Paused', 'Windows full suite on develop', 'Never fired', '3 consecutive failures', false],
      [3, 'Off', 'Disabled', '17 19 * * * UTC', 'Never fired', null, false],
    ]);
  });
});

describe('list load', () => {
  it('fetches automation.list once', async () => {
    const { conn, calls } = fakeConnection(() => listing([automation()], true));
    expect((await fetchAutomations(conn)).automations).toHaveLength(1);
    expect(calls.map(([m]) => m)).toEqual(['automation.list']);
  });

  it('a daemon error rejects with its text (the screen shows it with Retry)', async () => {
    const { conn } = fakeConnection(() => {
      throw new Error('automation.list: database is locked');
    });
    await expect(fetchAutomations(conn)).rejects.toThrow('automation.list: database is locked');
  });
});

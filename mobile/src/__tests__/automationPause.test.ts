// automationPause.test.ts: Pause asks first, calls automation.disable exactly once, and shows the daemon's error.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

import { describe, expect, it } from 'vitest';
import { disableAutomation } from '../automations/api';
import { PAUSE_TITLE, pauseMessage, pauseWithConfirm } from '../automations/pause';
import { automationRow } from '../automations/listView';
import { automation, fakeConnection } from './automationFixtures';

describe('pauseWithConfirm', () => {
  it('declined: no daemon call at all', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    const asked: string[][] = [];
    const res = await pauseWithConfirm({
      name: 'Nightly',
      confirm: async (t, m) => (asked.push([t, m]), false),
      disable: () => disableAutomation(conn, 1),
    });
    expect(res).toEqual({ kind: 'cancelled' });
    expect(calls).toEqual([]);
    expect(asked).toEqual([[PAUSE_TITLE, pauseMessage('Nightly')]]);
  });

  it('confirmed: automation.disable once, with the id', async () => {
    const { conn, calls } = fakeConnection(() => ({}));
    const res = await pauseWithConfirm({ name: 'Nightly', confirm: async () => true, disable: () => disableAutomation(conn, 7) });
    expect(res).toEqual({ kind: 'paused' });
    expect(calls).toEqual([['automation.disable', { id: 7 }]]);
  });

  it("a daemon error comes back as the daemon's text", async () => {
    const { conn, calls } = fakeConnection(() => {
      throw new Error('automation.disable: automation not found');
    });
    const res = await pauseWithConfirm({ name: 'Nightly', confirm: async () => true, disable: () => disableAutomation(conn, 7) });
    expect(res).toEqual({ kind: 'error', message: 'automation.disable: automation not found' });
    expect(calls).toHaveLength(1);
  });

  it('the confirm text names the automation and says it stays off until the computer turns it on', () => {
    expect(pauseMessage('Nightly')).toBe('Nightly will stop firing until it is turned back on from the computer.');
  });
});

describe('the Pause button is offered only for an enabled, not-yet-paused automation', () => {
  it('canPause', () => {
    expect(automationRow(automation()).canPause).toBe(true);
    expect(automationRow(automation({ status: 'paused' })).canPause).toBe(false);
    expect(automationRow(automation({ enabled: false })).canPause).toBe(false);
  });
});

describe('the screen reaches disable only through the confirm', () => {
  const src = readFileSync(join(__dirname, '..', 'screens', 'AutomationDetailScreen.tsx'), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|[^:])\/\/[^\n]*/g, '$1');

  it('disableAutomation is referenced once, as the disable callback of pauseWithConfirm, with the native confirm', () => {
    expect(src.match(/disableAutomation\(/g)).toHaveLength(1);
    expect(src).toMatch(/pauseWithConfirm\(\{[^}]*confirm: \(t, m\) => confirmNative\(t, m\)[^}]*disable: \(\) => disableAutomation\(conn, id\)[^}]*\}\)/);
  });
});

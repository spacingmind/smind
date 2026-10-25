// pluginsReadOnly.test.ts: AM2/AM5. The plugin client sends only the four read RPCs, the screens reach the daemon
// only through it, and plugin.updated refetches.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { fetchPlugin, fetchPluginCommands, fetchPluginList, fetchPluginThemes, PLUGIN_READ_METHODS } from '../plugins/api';
import { watchPluginUpdates } from '../plugins/subscription';
import { makeRelayHarness } from '../relayHarness';
import { cmd, fakeConnection, listing, plugin } from './pluginFixtures';

const SRC = join(__dirname, '..');
const ROOT = join(SRC, '..');

function sources(dir: string): string[] {
  const out: string[] = [];
  for (const e of readdirSync(dir)) {
    const full = join(dir, e);
    if (statSync(full).isDirectory()) {
      if (e !== '__tests__') out.push(...sources(full));
    } else if (/\.(ts|tsx)$/.test(e)) out.push(full);
  }
  return out;
}

describe('plugin client is read-only', () => {
  it('detail and list flows call only plugin.list / get / commands / themes', async () => {
    const { conn, calls } = fakeConnection((method) => {
      switch (method) {
        case 'plugin.list':
          return listing([plugin()]);
        case 'plugin.get':
          return plugin();
        case 'plugin.commands':
          return [cmd('acme', 'plan')];
        case 'plugin.themes':
          return [];
        default:
          throw new Error(`unexpected RPC ${method}`);
      }
    });
    await fetchPluginList(conn);
    await fetchPlugin(conn, 'acme');
    await fetchPluginCommands(conn);
    await fetchPluginThemes(conn);
    expect(calls.map(([m]) => m)).toEqual(['plugin.list', 'plugin.get', 'plugin.commands', 'plugin.themes']);
    expect(calls.every(([m]) => (PLUGIN_READ_METHODS as readonly string[]).includes(m))).toBe(true);
    expect(calls[1][1]).toEqual({ id: 'acme' });
  });

  it('no mobile source names a mutating plugin RPC', () => {
    const mutating = /plugin\.(install|link|enable|disable|remove|update|reload|call)\b/;
    const hits = [...sources(SRC), ...sources(join(ROOT, 'app'))].filter((f) => mutating.test(readFileSync(f, 'utf8').replace(/\/\/[^\n]*|\/\*[\s\S]*?\*\//g, '')));
    expect(hits).toEqual([]);
  });

  it('a malformed answer degrades to empty rather than throwing', async () => {
    const { conn } = fakeConnection(() => null);
    expect(await fetchPluginList(conn)).toEqual({ globalEnabled: false, plugins: [] });
    expect(await fetchPluginCommands(conn)).toEqual([]);
    expect(await fetchPluginThemes(conn)).toEqual([]);
  });
});

describe('plugin.updated', () => {
  it('refetches: the subscription delivers each event, then stops after unsubscribe', async () => {
    const { conn, daemon } = await makeRelayHarness();
    const seen: Array<{ id: string; globalEnabled: boolean }> = [];
    const off = watchPluginUpdates(conn, (p) => seen.push(p));
    const [sub] = await daemon.waitForRequests(1);
    expect(sub.method).toBe('events.subscribe');
    expect(sub.params).toEqual({ topics: ['plugin.updated'] });
    daemon.reply(sub.id!, { topics: ['plugin.updated'] });

    daemon.pushNotification('plugin.updated', { id: 'acme', globalEnabled: true });
    daemon.pushNotification('plugin.updated', { id: '', globalEnabled: false }, 2);
    await new Promise((r) => setTimeout(r, 10));
    expect(seen).toEqual([
      { id: 'acme', globalEnabled: true },
      { id: '', globalEnabled: false },
    ]);

    off();
    const unsub = (await daemon.waitForRequests(2))[1];
    expect(unsub.method).toBe('events.unsubscribe');
    daemon.reply(unsub.id!, { topics: [] });
    daemon.pushNotification('plugin.updated', { id: 'late', globalEnabled: true }, 3);
    await new Promise((r) => setTimeout(r, 10));
    expect(seen.length).toBe(2);
  });
});

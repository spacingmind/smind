// pluginsReadOnly.test.ts: AM2/AM5. The plugin client sends only the four read RPCs, the screens reach the daemon
// only through it, and plugin.updated refetches.

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import { fetchPlugin, fetchPluginCommands, fetchPluginDetail, fetchPluginList, fetchPluginPanels, fetchPluginThemes, fetchPluginView, PLUGIN_PANEL_METHODS, PLUGIN_READ_METHODS } from '../plugins/api';
import { watchPluginUpdates, watchPluginEvents } from '../plugins/subscription';
import { PluginEventPayload } from '../plugins/types';
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
    await fetchPluginDetail(conn, 'acme');
    expect(calls.map(([m]) => m)).toEqual(['plugin.list', 'plugin.get', 'plugin.commands', 'plugin.themes', 'plugin.get', 'plugin.commands']);
    const known: string[] = [...PLUGIN_READ_METHODS, ...PLUGIN_PANEL_METHODS];
    expect(calls.every(([m]) => known.includes(m))).toBe(true);
    expect(calls[1][1]).toEqual({ id: 'acme' });
  });

  it('panels client sends plugin.panels and plugin.view with ui:1 and nothing else', async () => {
    const view = { type: 'text', text: 'Acme overview' };
    const { conn, calls } = fakeConnection((method) => {
      switch (method) {
        case 'plugin.panels':
          return { panels: [{ plugin: 'acme', id: 'overview', title: 'Overview', where: 'task', ui: 1 }], sidebar: [] };
        case 'plugin.view':
          return { pluginId: 'acme', panelId: 'overview', view };
        default:
          throw new Error(`unexpected RPC ${method}`);
      }
    });
    const panels = await fetchPluginPanels(conn);
    const got = await fetchPluginView(conn, 'acme', 'overview');
    expect(calls).toEqual([
      ['plugin.panels', { ui: 1 }],
      ['plugin.view', { pluginId: 'acme', panelId: 'overview', ui: 1 }],
    ]);
    expect(calls.every(([m]) => (PLUGIN_PANEL_METHODS as readonly string[]).includes(m))).toBe(true);
    expect(panels.panels[0]).toMatchObject({ plugin: 'acme', id: 'overview', where: 'task', ui: 1 });
    expect(got).toBe(view);
  });

  it('no mobile source names a mutating plugin RPC', () => {
    const mutating = /plugin\.(install|link|enable|disable|remove|update|reload)\b/;
    const hits = [...sources(SRC), ...sources(join(ROOT, 'app'))].filter((f) => mutating.test(readFileSync(f, 'utf8').replace(/\/\/[^\n]*|\/\*[\s\S]*?\*\//g, '')));
    expect(hits).toEqual([]);
  });

  it('plugin.call is named in exactly one place: plugins/api.ts, for RPC-backed slash commands', () => {
    const named = /plugin\.call\b/;
    const strip = (f: string) => readFileSync(f, 'utf8').replace(/\/\/[^\n]*|\/\*[\s\S]*?\*\//g, '');
    const hits = [...sources(SRC), ...sources(join(ROOT, 'app'))].filter((f) => named.test(strip(f)));
    expect(hits.map((f) => f.slice(SRC.length + 1))).toEqual(['plugins/api.ts']);
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

describe('plugin.event', () => {
  it('delivers plugin.event payloads (coerced), then stops after unsubscribe', async () => {
    const { conn, daemon } = await makeRelayHarness();
    const seen: PluginEventPayload[] = [];
    const off = watchPluginEvents(conn, (p) => seen.push(p));
    const [sub] = await daemon.waitForRequests(1);
    expect(sub.method).toBe('events.subscribe');
    expect(sub.params).toEqual({ topics: ['plugin.event'] });
    daemon.reply(sub.id!, { topics: ['plugin.event'] });

    daemon.pushNotification('plugin.event', { pluginId: 'acme', kind: 'refresh', panelId: 'overview' });
    daemon.pushNotification('plugin.event', { pluginId: 7, kind: null }, 2);
    await new Promise((r) => setTimeout(r, 10));
    expect(seen).toEqual([
      { pluginId: 'acme', kind: 'refresh', panelId: 'overview' },
      { pluginId: '', kind: '' },
    ]);

    off();
    const unsub = (await daemon.waitForRequests(2))[1];
    expect(unsub.method).toBe('events.unsubscribe');
    daemon.reply(unsub.id!, { topics: [] });
    daemon.pushNotification('plugin.event', { pluginId: 'acme', kind: 'refresh' }, 3);
    await new Promise((r) => setTimeout(r, 10));
    expect(seen.length).toBe(2);
  });
});

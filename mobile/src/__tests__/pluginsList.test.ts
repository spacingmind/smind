// pluginsList.test.ts: the Plugins screen's states as data (AM1) and the read-only detail (AM2).

import { describe, expect, it } from 'vitest';
import { LINK_WARNING_TEXT, MANAGE_ON_COMPUTER_TEXT, PLUGINS_EMPTY_TEXT, PLUGINS_OFF_TEXT, pluginDetailView, pluginStatus, pluginsListView } from '../plugins/listView';
import { cmd, consent, listing, plugin } from './pluginFixtures';

describe('pluginsListView', () => {
  it('global switch off: the notice, no list state', () => {
    const v = pluginsListView(listing([], false));
    expect(v).toEqual({ kind: 'off', notice: PLUGINS_OFF_TEXT, rows: [] });
    expect(PLUGINS_OFF_TEXT).toBe('Plugins are off on this daemon. Turn them on in config.yaml on the computer.');
  });

  it('off with installed plugins still names them (the notice leads)', () => {
    const v = pluginsListView(listing([plugin()], false));
    expect(v.kind).toBe('off');
    expect(v.kind === 'off' && v.rows.map((r) => r.id)).toEqual(['acme']);
  });

  it('empty state when on and nothing is installed', () => {
    expect(pluginsListView(listing([]))).toEqual({ kind: 'empty', text: PLUGINS_EMPTY_TEXT });
  });

  it('every status chip, with Error > Pending update > Enabled/Disabled', () => {
    expect(pluginStatus(plugin()).label).toBe('Enabled');
    expect(pluginStatus(plugin({ enabled: false })).label).toBe('Disabled');
    expect(pluginStatus(plugin({ pending: { consent: consent() } })).label).toBe('Pending update');
    expect(pluginStatus(plugin({ lastError: 'boom' })).label).toBe('Error');
    expect(pluginStatus(plugin({ lastError: 'boom', pending: { consent: consent() }, enabled: false })).label).toBe('Error');
  });

  it('list rows: name, version, source kind chip, last error line, link warning only for link sources', () => {
    const v = pluginsListView(
      listing([
        plugin({ id: 'acme' }),
        plugin({ id: 'live', sourceKind: 'link', enabled: false }),
        plugin({ id: 'broken', sourceKind: 'git', lastError: 'manifest.json: bad', enabled: false }),
        plugin({ id: 'next', sourceKind: 'archive', pending: { consent: consent() } }),
      ]),
    );
    expect(v.kind).toBe('list');
    const rows = v.kind === 'list' ? v.rows : [];
    expect(rows.map((r) => [r.id, r.version, r.source, r.status, r.lastError, r.linkWarning])).toEqual([
      ['acme', '1.2.0', 'Folder', 'Enabled', null, null],
      ['live', '1.2.0', 'Link', 'Disabled', null, LINK_WARNING_TEXT],
      ['broken', '1.2.0', 'Git', 'Error', 'manifest.json: bad', null],
      ['next', '1.2.0', 'Archive', 'Pending update', null, null],
    ]);
    expect(LINK_WARNING_TEXT).toBe('Live directory: changes after consent are not re-reviewed');
  });
});

describe('pluginDetailView (read-only)', () => {
  const info = plugin({
    consent: consent({
      description: 'Acme helpers',
      dataOnly: false,
      process: ['node', 'srv.js'],
      capabilities: ['mcp', 'skills'],
      mcpServers: [{ name: 'db', transport: 'stdio', command: ['node', 'db.js'] }],
      skills: ['review'],
      profiles: [
        { name: 'calm', provider: 'glm' },
        { name: 'risky', provider: 'claude-native', escalations: ['auto-accepts every tool call'] },
      ],
    }),
    conflicts: [{ plugin: 'acme', kind: 'skill', name: 'review', reason: 'built-in skill wins' }],
  });

  it('shows the consent summary with owner labels, escalations flagged, conflicts, and its commands', () => {
    const v = pluginDetailView(info, [cmd('acme', 'plan'), cmd('other', 'x')]);
    expect(v.description).toBe('Acme helpers');
    expect(v.dataOnly).toBe(false);
    expect(v.capabilities).toEqual(['mcp', 'skills']);
    expect(v.mcpServers).toEqual([{ name: 'db', transport: 'stdio', owner: 'from acme' }]);
    expect(v.skills).toEqual([{ name: 'review', owner: 'from acme' }]);
    expect(v.profiles.map((p) => [p.name, p.escalations])).toEqual([
      ['calm', []],
      ['risky', ['auto-accepts every tool call']],
    ]);
    expect(v.commands).toEqual([{ label: '/acme:plan', owner: 'from acme' }]);
    expect(v.conflicts).toEqual([{ kind: 'skill', name: 'review', reason: 'built-in skill wins' }]);
  });

  it('carries only data and the where-to-manage line: no action, no handler', () => {
    const v = pluginDetailView(info, []);
    expect(v.manageHint).toBe(MANAGE_ON_COMPUTER_TEXT);
    expect(MANAGE_ON_COMPUTER_TEXT).toBe('Manage plugins on the computer: Settings -> Plugins');
    for (const value of JSON.stringify(v) ? Object.values(v) : []) expect(typeof value).not.toBe('function');
  });

  it('a pending update shows the pending consent', () => {
    const v = pluginDetailView(plugin({ pending: { consent: consent({ capabilities: ['hooks'] }) } }), []);
    expect(v.pending).toBe(true);
    expect(v.capabilities).toEqual(['hooks']);
  });
});

// pluginPanelView.test.ts: the panel screen's pure helpers -- the error words per daemon code
// (the view_invalid message names the JSON path and must survive verbatim), which plugin.event
// payloads refresh this panel, and which panels in a plugin.panels result belong to one plugin.

import { describe, expect, it } from 'vitest';
import { eventTouchesPanel, panelErrorView, panelsOfPlugin } from '../plugins/panelView';
import { DaemonError } from '../relay/RelayConnection';
import type { PluginPanelsResult } from '../plugins/types';

describe('panelErrorView', () => {
  it('view_invalid keeps the daemon message naming the path', () => {
    const err = new DaemonError('plugin.view: view document invalid at root.children[3].action', 'view_invalid');
    expect(panelErrorView(err)).toEqual({
      title: 'This panel is too large or invalid',
      body: 'view document invalid at root.children[3].action',
    });
  });

  it('ui_level_too_low asks for an update', () => {
    const err = new DaemonError('plugin.view: the panel needs ui level 2', 'ui_level_too_low');
    expect(panelErrorView(err)).toEqual({ title: 'This panel needs a newer app', body: 'Update the app to open it.' });
  });

  it('other errors fall back', () => {
    const err = new DaemonError('relay: not connected', 'call_timeout');
    expect(panelErrorView(err)).toEqual({ title: "Couldn't load this panel", body: 'relay: not connected' });
    expect(panelErrorView(new Error('boom'))).toEqual({ title: "Couldn't load this panel", body: 'boom' });
    expect(panelErrorView('nope')).toEqual({ title: "Couldn't load this panel", body: 'nope' });
  });
});

describe('eventTouchesPanel', () => {
  it('plugin.event refreshes only its own panel or a whole plugin', () => {
    expect(eventTouchesPanel({ pluginId: 'acme', panelId: 'overview' }, 'acme', 'overview')).toBe(true);
    expect(eventTouchesPanel({ pluginId: 'acme' }, 'acme', 'overview')).toBe(true); // no panelId: the whole plugin
    expect(eventTouchesPanel({ pluginId: 'acme', panelId: 'logs' }, 'acme', 'overview')).toBe(false); // another panel
    expect(eventTouchesPanel({ pluginId: 'other', panelId: 'overview' }, 'acme', 'overview')).toBe(false); // another plugin
  });
});

describe('panelsOfPlugin', () => {
  it('panelsOfPlugin filters by plugin', () => {
    const res: PluginPanelsResult = {
      panels: [
        { plugin: 'other', id: 'overview', title: 'Other', where: 'task', ui: 1 },
        { plugin: 'acme', id: 'overview', title: 'Overview', where: 'task', ui: 1 },
        { plugin: 'acme', id: 'logs', title: 'Logs', where: 'sidebar', ui: 1 },
      ],
      sidebar: [{ plugin: 'acme', id: 'logs-entry', title: 'Logs', opens: 'logs' }],
    };
    const mine = panelsOfPlugin(res, 'acme');
    expect(mine.map((p) => p.id)).toEqual(['overview', 'logs']); // that plugin's panels, in order
    expect(panelsOfPlugin(res, 'nobody')).toEqual([]);
  });
});

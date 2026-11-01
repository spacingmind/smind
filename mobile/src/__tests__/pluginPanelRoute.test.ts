// pluginPanelRoute.test.ts: the plugin-panel route helpers -- the href and the parser agree, and
// the parser refuses anything that is not a non-empty, at most 128-char route string (the
// parsePluginParams rules, applied to both ids).

import { describe, expect, it } from 'vitest';
import { parsePluginPanelParams, pluginPanelHref } from '../navigation/routes';

describe('pluginPanelHref', () => {
  it('href round trip', () => {
    const href = pluginPanelHref('acme', 'overview');
    expect(href).toEqual({ pathname: '/plugin-panel', params: { plugin: 'acme', panel: 'overview' } });
    expect(parsePluginPanelParams(href.params)).toEqual({ ok: true, plugin: 'acme', panel: 'overview' });
  });
});

describe('parsePluginPanelParams', () => {
  it('empty and overlong ids are refused', () => {
    for (const params of [
      {},
      { plugin: 'acme' },
      { panel: 'overview' },
      { plugin: '', panel: 'overview' },
      { plugin: 'acme', panel: '' },
      { plugin: ' ', panel: 'overview' },
      { plugin: 'acme', panel: ' ' },
      { plugin: 'a'.repeat(129), panel: 'overview' },
      { plugin: 'acme', panel: 'p'.repeat(129) },
      { plugin: 'acme', panel: [] },
      { plugin: [], panel: 'overview' },
      { plugin: [''], panel: 'overview' },
    ]) {
      expect(parsePluginPanelParams(params), JSON.stringify(params)).toEqual({ ok: false });
    }
  });

  it('array params use the first value', () => {
    expect(parsePluginPanelParams({ plugin: ['acme', 'other'], panel: ['overview', 'logs'] })).toEqual({ ok: true, plugin: 'acme', panel: 'overview' });
  });
});

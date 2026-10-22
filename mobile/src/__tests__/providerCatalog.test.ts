// providerCatalog.test.ts covers M6's pure selection logic: remembered
// wins / falls back when the remembered id left the catalog, per-provider
// re-resolution on provider switch, option lists, and what wireModelMode
// omits (the daemon default).

import { describe, expect, it } from 'vitest';
import { ProviderInfo } from '../api';
import {
  defaultSelection,
  modeOptions,
  modelOptions,
  providerLabel,
  selectProvider,
  wireModelMode,
} from '../providerCatalog';

const glm: ProviderInfo = {
  id: 'glm',
  label: 'GLM',
  defaultMode: 'default',
  modes: [
    { id: 'default', label: 'Default' },
    { id: 'acceptEdits', label: 'Accept edits', autoApproves: true },
  ],
  models: [
    { id: 'glm-4.7', label: 'GLM 4.7', default: true },
    { id: 'glm-air', label: 'GLM Air' },
  ],
};
const claude: ProviderInfo = {
  id: 'claude-native',
  modes: [{ id: 'plan', label: 'Plan' }, { id: 'bypassPermissions', label: 'Bypass', autoApproves: true }],
  defaultMode: 'plan',
  models: [{ id: 'sonnet', label: 'Sonnet', default: true }],
};

describe('defaultSelection', () => {
  it('first provider with its default model and defaultMode when nothing is remembered', () => {
    expect(defaultSelection([glm, claude])).toEqual({ provider: 'glm', model: 'glm-4.7', mode: 'default' });
  });

  it('remembered provider wins when it is in the catalog, with its remembered model/mode', () => {
    const remembered = { provider: 'claude-native', byProvider: { 'claude-native': { model: 'sonnet', mode: 'bypassPermissions' } } };
    expect(defaultSelection([glm, claude], remembered)).toEqual({ provider: 'claude-native', model: 'sonnet', mode: 'bypassPermissions' });
  });

  it('an unknown remembered provider falls back to the first provider', () => {
    expect(defaultSelection([glm], { provider: 'gone' })).toEqual({ provider: 'glm', model: 'glm-4.7', mode: 'default' });
  });

  it('remembered model/mode ignored when no longer in the catalog', () => {
    const remembered = { provider: 'glm', byProvider: { glm: { model: 'glm-5', mode: 'deleted' } } };
    expect(defaultSelection([glm], remembered)).toEqual({ provider: 'glm', model: 'glm-4.7', mode: 'default' });
  });

  it('a provider without models/modes yields "" for both', () => {
    const bare: ProviderInfo = { id: 'bare' };
    expect(defaultSelection([bare])).toEqual({ provider: 'bare', model: '', mode: '' });
  });

  it('empty catalog returns null', () => {
    expect(defaultSelection([])).toBeNull();
  });
});

describe('selectProvider', () => {
  it('re-resolves model+mode for the new provider (remembered else defaults)', () => {
    const remembered = { byProvider: { glm: { model: 'glm-air' } } };
    expect(selectProvider([glm, claude], 'claude-native', remembered)).toEqual({ provider: 'claude-native', model: 'sonnet', mode: 'plan' });
    expect(selectProvider([glm, claude], 'glm', remembered)).toEqual({ provider: 'glm', model: 'glm-air', mode: 'default' });
  });

  it('an unknown provider id yields empty model/mode', () => {
    expect(selectProvider([glm], 'nope')).toEqual({ provider: 'nope', model: '', mode: '' });
  });
});

describe('labels and options', () => {
  it('providerLabel is label ?? id', () => {
    expect(providerLabel(glm)).toBe('GLM');
    expect(providerLabel({ id: 'kimi' })).toBe('kimi');
  });

  it('modelOptions starts with the synthetic Default, then the catalog', () => {
    expect(modelOptions(glm)).toEqual([
      { value: '', label: 'Default' },
      { value: 'glm-4.7', label: 'GLM 4.7' },
      { value: 'glm-air', label: 'GLM Air' },
    ]);
    expect(modelOptions(undefined)).toEqual([{ value: '', label: 'Default' }]);
  });

  it('modeOptions lists the catalog and appends " (auto-approves)", with no synthetic Default', () => {
    expect(modeOptions(glm)).toEqual([
      { value: 'default', label: 'Default' },
      { value: 'acceptEdits', label: 'Accept edits (auto-approves)' },
    ]);
    expect(modeOptions(undefined)).toEqual([]);
  });
});

describe('wireModelMode', () => {
  it("omits '' (daemon default model) and the provider's defaultMode", () => {
    expect(wireModelMode(glm, { provider: 'glm', model: '', mode: 'default' })).toEqual({});
  });

  it('sends a chosen model and a non-default mode', () => {
    expect(wireModelMode(glm, { provider: 'glm', model: 'glm-air', mode: 'acceptEdits' })).toEqual({ model: 'glm-air', permissionMode: 'acceptEdits' });
  });

  it('with no provider entry, any non-empty selection still goes out', () => {
    expect(wireModelMode(undefined, { provider: 'x', model: 'm', mode: 'acceptEdits' })).toEqual({ model: 'm', permissionMode: 'acceptEdits' });
  });
});

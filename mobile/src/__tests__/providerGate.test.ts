// providerGate.test.ts covers the new-task picker's gate view-model (docs/plans/active/picker-gate-reasons.md PR4):
// hard gates block choosing a provider/model, advisory ones warn, a time gate that already ended is no gate, an
// unknown reason is hard and shows its action label, and the notice names the way out.

import { describe, expect, it } from 'vitest';
import { ProviderGate, ProviderInfo } from '../api';
import { defaultSelection, modelOptions, providerOptions, selectModelChecked, selectProviderChecked } from '../providerCatalog';
import { gateBadgeLabel, gateNotice, gateText, isHardGate, liveGate, selectionGate } from '../providerGate';

const NOW = Date.parse('2026-11-01T10:00:00Z');
const FUTURE = new Date(NOW + 2 * 3600_000).toISOString();
const PAST = new Date(NOW - 60_000).toISOString();

const signedOut: ProviderGate = { reason: 'not_signed_in', action: { kind: 'sign_in', target: 'settings:providers?provider=glm', label: 'Add the API key for GLM' } };
const cooling: ProviderGate = { reason: 'cooling_down', until: FUTURE, action: { kind: 'clear_cooldown', target: 'glm', label: 'Clear' } };
const outOfPlan: ProviderGate = { reason: 'out_of_plan', until: FUTURE, advisory: true, action: { kind: 'wait_until', label: 'Wait' } };

const claude: ProviderInfo = { id: 'claude-native', label: 'Claude Code', models: [{ id: 'opus', label: 'Opus' }, { id: 'sonnet', label: 'Sonnet', default: true }] };
const glm = (gate?: ProviderGate): ProviderInfo => ({ id: 'glm', label: 'GLM', models: [{ id: 'g5', label: 'GLM 5', default: true }], ...(gate ? { gate } : {}) });

describe('gate helpers', () => {
  it('an advisory gate is not hard; anything else is, an unknown reason included', () => {
    expect(isHardGate(signedOut)).toBe(true);
    expect(isHardGate(outOfPlan)).toBe(false);
    expect(isHardGate({ reason: 'from_the_future' })).toBe(true);
    expect(isHardGate(undefined)).toBe(false);
  });

  it('a time gate whose until passed is ungated; a gate without until stays', () => {
    expect(liveGate({ reason: 'cooling_down', until: PAST }, NOW)).toBeUndefined();
    expect(liveGate({ reason: 'out_of_plan', until: PAST }, NOW)).toBeUndefined();
    expect(liveGate({ reason: 'out_of_plan' }, NOW)).toBeDefined();
    expect(liveGate({ reason: 'not_signed_in', until: PAST }, NOW)).toBeDefined();
  });

  it('badge labels and text per reason; an unknown reason shows its action label', () => {
    expect(gateBadgeLabel(signedOut)).toBe('Signed out');
    expect(gateBadgeLabel({ reason: 'unsupported_by_harness' })).toBe('Unsupported');
    expect(gateBadgeLabel({ reason: 'model_not_in_plan' })).toBe('Not in plan');
    expect(gateText(signedOut, NOW)).toBe('Signed out. Add the API key for GLM');
    expect(gateText({ reason: 'fresh', action: { kind: 'x', label: 'Do the thing' } }, NOW)).toBe('Do the thing');
    expect(gateText({ reason: 'fresh' }, NOW)).toBe('fresh');
  });
});

describe('notice view-model', () => {
  it('sign_in has no button on mobile: it says where to sign in', () => {
    const n = gateNotice(signedOut, NOW, true);
    expect(n).toMatchObject({ badge: 'Signed out', tone: 'destructive', hard: true });
    expect(n.action).toEqual({ kind: 'hint', text: 'Sign in on the desktop app (Settings > Providers).' });
  });

  it('clear_cooldown is a tap target only where the caller may clear', () => {
    expect(gateNotice(cooling, NOW, true).action).toEqual({ kind: 'clear_cooldown', label: 'Clear cooldown', provider: 'glm' });
    expect(gateNotice(cooling, NOW, false).action).toBeUndefined();
  });

  it('advisory gates warn: warning tone, not hard, wait_until has no control', () => {
    const n = gateNotice(outOfPlan, NOW, true);
    expect(n).toMatchObject({ badge: 'Plan used up', tone: 'warning', hard: false });
    expect(n.action).toBeUndefined();
    expect(n.text).toContain('resets');
  });
});

describe('selection', () => {
  it('provider options carry a label suffix per live gate', () => {
    const opts = providerOptions([claude, glm(signedOut), glm({ ...cooling, until: PAST })].slice(0, 2), NOW);
    expect(opts).toEqual([
      { value: 'claude-native', label: 'Claude Code' },
      { value: 'glm', label: 'GLM — signed out' },
    ]);
    expect(providerOptions([glm({ ...cooling, until: PAST })], NOW)[0]!.label).toBe('GLM');
  });

  it('a hard-gated provider cannot be chosen; an advisory one can', () => {
    const current = { provider: 'claude-native', model: 'sonnet', mode: '' };
    expect(selectProviderChecked([claude, glm(signedOut)], 'glm', current, undefined, NOW)).toEqual(current);
    const advisory = selectProviderChecked([claude, glm(outOfPlan)], 'glm', current, undefined, NOW);
    expect(advisory.provider).toBe('glm');
  });

  it('a hard-gated model cannot be chosen; the model options carry the suffix', () => {
    const p: ProviderInfo = { ...claude, modelGates: { opus: { reason: 'model_not_in_plan', action: { kind: 'pick_model', label: 'Pick another model' } } } };
    const current = { provider: 'claude-native', model: 'sonnet', mode: '' };
    expect(selectModelChecked(p, current, 'opus', NOW)).toEqual(current);
    expect(selectModelChecked(p, current, 'sonnet', NOW).model).toBe('sonnet');
    expect(modelOptions(p, NOW).map((o) => o.label)).toEqual(['Default', 'Opus — not in plan', 'Sonnet']);
  });

  it('the default selection skips a hard-gated remembered or first provider', () => {
    const list = [glm(signedOut), claude];
    expect(defaultSelection(list, { provider: 'glm' }, NOW)?.provider).toBe('claude-native');
    expect(defaultSelection(list, undefined, NOW)?.provider).toBe('claude-native');
    // Everything gated: the first provider still opens the form (the notice explains).
    expect(defaultSelection([glm(signedOut)], undefined, NOW)?.provider).toBe('glm');
  });

  it('selectionGate: the model gate wins over the provider gate; the default model inherits the provider gate', () => {
    const p: ProviderInfo = { ...glm(cooling), modelGates: { g5: { reason: 'model_not_in_plan' } } };
    expect(selectionGate(p, 'g5', NOW)?.reason).toBe('model_not_in_plan');
    expect(selectionGate(p, '', NOW)?.reason).toBe('cooling_down');
    expect(selectionGate(glm(), '', NOW)).toBeUndefined();
  });
});

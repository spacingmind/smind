// newTaskPrefs.test.ts: session-only remembering of the last selection,
// per provider, with recall returning nothing before a first use.

import { beforeEach, describe, expect, it } from 'vitest';
import { recallSelection, rememberSelection, resetNewTaskPrefs } from '../newTaskPrefs';

describe('newTaskPrefs', () => {
  beforeEach(() => resetNewTaskPrefs());

  it('recalls nothing before a first remember', () => {
    expect(recallSelection()).toEqual({});
  });

  it('remembers the provider and its model/mode', () => {
    rememberSelection({ provider: 'glm', model: 'glm-air', mode: 'default' });
    expect(recallSelection()).toEqual({ provider: 'glm', byProvider: { glm: { model: 'glm-air', mode: 'default' } } });
  });

  it('keeps per-provider entries when the provider changes back', () => {
    rememberSelection({ provider: 'glm', model: 'glm-air', mode: 'acceptEdits' });
    rememberSelection({ provider: 'claude-native', model: '', mode: 'plan' });
    expect(recallSelection().provider).toBe('claude-native');
    expect(recallSelection().byProvider).toEqual({
      glm: { model: 'glm-air', mode: 'acceptEdits' },
      'claude-native': { model: '', mode: 'plan' },
    });
  });

  it('an unknown provider in byProvider recalls nothing for others', () => {
    rememberSelection({ provider: 'glm', model: '', mode: '' });
    expect(recallSelection().byProvider?.['claude-native']).toBeUndefined();
  });
});

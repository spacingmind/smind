// firstSend.test.ts covers M6-B2's provider resolution for a task's
// first prompt (the four cases from the plan's acceptance criterion 7).

import { describe, expect, it } from 'vitest';
import { ProviderInfo } from '../api';
import { firstSendProvider } from '../firstSend';

const catalog: ProviderInfo[] = [
  { id: 'claude', label: 'Claude' },
  { id: 'codex', label: 'Codex' },
];

describe('firstSendProvider', () => {
  it("a latest run's provider wins, even if absent from the catalog", () => {
    expect(firstSendProvider(catalog, 'glm/hidden', {})).toBe('glm/hidden');
    expect(firstSendProvider([], 'claude', {})).toBe('claude');
  });

  it('the remembered provider is used when the run has none and it is still listed', () => {
    expect(firstSendProvider(catalog, null, { provider: 'codex' })).toBe('codex');
  });

  it('without a run or memory the first catalog provider is used', () => {
    expect(firstSendProvider(catalog, null, {})).toBe('claude');
    expect(firstSendProvider(catalog, null, { provider: 'gone' })).toBe('claude'); // stale memory ignored
  });

  it('an empty catalog with no run resolves to null', () => {
    expect(firstSendProvider([], null, {})).toBeNull();
  });
});

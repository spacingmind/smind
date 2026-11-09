// failureText.test.ts: the daemon sentence table — every known kind has one
// plain sentence, the wire's action.summary wins, and an unknown kind stays
// empty rather than guessing.

import { describe, expect, it } from 'vitest';
import { FAILURE_FALLBACK_SENTENCE, failureSentence } from '../failureText';

const KINDS = [
  'context_window_exceeded',
  'session_budget_exceeded',
  'too_many_denials',
  'cli_exited',
  'plan_window_exhausted',
  'transient_429',
  'rate_limited',
  'auth_expired',
  'model_not_in_plan',
];

describe('failureSentence', () => {
  it('every known kind has exactly one plain sentence (capital start, trailing period)', () => {
    expect(Object.keys(FAILURE_FALLBACK_SENTENCE).sort()).toEqual([...KINDS].sort());
    for (const kind of KINDS) expect(FAILURE_FALLBACK_SENTENCE[kind], kind).toMatch(/^[A-Z][^.]*[.;]$/);
  });

  it('the daemon sentence (action.summary) wins when non-empty', () => {
    expect(failureSentence('cli_exited', { summary: 'Daemon wording wins.' })).toBe('Daemon wording wins.');
    expect(failureSentence('unknown_kind', { summary: 'A sentence for a kind this table has not met.' })).toBe('A sentence for a kind this table has not met.');
  });

  it('an empty or absent summary falls back to the table', () => {
    expect(failureSentence('rate_limited', { summary: '' })).toBe('The provider limited how fast requests can be sent.');
    expect(failureSentence('rate_limited')).toBe('The provider limited how fast requests can be sent.');
  });

  it('the three human-action classes read as the daemon words them', () => {
    expect(failureSentence('context_window_exceeded')).toBe("The conversation no longer fits the model's context window.");
    expect(failureSentence('session_budget_exceeded')).toBe("The provider's session budget for this run is used up.");
    expect(failureSentence('too_many_denials')).toBe('The agent kept asking for actions that were denied, so the provider stopped the run.');
  });

  it('cli_exited reads as the readable sentence, not the raw CLI text', () => {
    expect(failureSentence('cli_exited')).toBe(
      'The agent stopped before finishing; its session is saved, so you can continue where it left off.',
    );
  });

  it('an unknown or missing kind is empty, never a guess', () => {
    expect(failureSentence('future_kind')).toBe('');
    expect(failureSentence(undefined)).toBe('');
    expect(failureSentence('')).toBe('');
  });
});

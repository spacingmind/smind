// promptLine.test.ts: the finished-run load path's prompt-line decision.

import { describe, expect, it } from 'vitest';
import { withRunPrompt } from '../promptLine';
import { TimelineLine } from '../runTimeline';

const assistant = { key: 'e1', role: 'assistant', text: 'on it' } as TimelineLine;
const user = { key: 'e0', role: 'user', text: 'earlier prompt' } as TimelineLine;

describe('withRunPrompt', () => {
  it('prepends the run prompt as a user line when the logs have none', () => {
    const out = withRunPrompt([assistant], 'run the tests');
    expect(out).toEqual([{ key: 'prun the tests', role: 'user', text: 'run the tests' }, assistant]);
    expect(out[0].key.startsWith('p')).toBe(true); // the echo-dedupe scheme
  });

  it('returns the lines unchanged when a user line is present', () => {
    const lines = [user, assistant];
    expect(withRunPrompt(lines, 'run the tests')).toBe(lines);
  });

  it('returns the lines unchanged for an empty prompt', () => {
    const lines = [assistant];
    expect(withRunPrompt(lines, '')).toBe(lines);
  });

  it('does not mutate the input', () => {
    const lines = [assistant];
    withRunPrompt(lines, 'run the tests');
    expect(lines).toEqual([assistant]);
  });
});

import { describe, expect, it } from 'vitest';

import { describeRunError, resetZoneNote, stripErrorEcho } from '../runError';

const QUOTA = '429 Usage limit reached for 5 hour. Your limit will reset at 2026-10-27 22:44:59';
const CHAIN = 'taskrunner: glm prompt: acp: session/prompt: acp: rpc error -32603: Internal error';

describe('describeRunError', () => {
  it("drops the provider's zone-less reset time from a rate-limited message and keeps the raw text", () => {
    const err = `${QUOTA} (${CHAIN})`;
    const v = describeRunError(err, { rateLimited: true, hasReset: true });
    expect(v.message).toBe('Usage limit reached for the 5-hour window.');
    expect(v.message).not.toMatch(/22:44|2026/);
    expect(v.raw).toBe(err);
  });

  it("keeps the provider's reset clause when the daemon found no reset to show instead", () => {
    const v = describeRunError(QUOTA, { rateLimited: true, hasReset: false });
    expect(v.message).toContain('will reset at 2026-10-27 22:44:59');
  });

  it('turns a bare RPC chain into a plain sentence', () => {
    const v = describeRunError(CHAIN);
    expect(v.message).toBe('The agent stopped with an internal error.');
    expect(v.raw).toBe(CHAIN);
  });

  it('names a non-generic RPC failure', () => {
    expect(describeRunError('acp: rpc error -32000: model overloaded').message).toBe('The agent failed: model overloaded.');
  });

  it("leads with the agent's cause when a chain follows it", () => {
    const v = describeRunError(`Could not read the repo (${CHAIN})`);
    expect(v.message).toBe('Could not read the repo');
    expect(v.raw).not.toBeNull();
  });

  it('leaves an ordinary error untouched with no raw copy', () => {
    expect(describeRunError('git: not a repository')).toEqual({ message: 'git: not a repository', raw: null });
  });
});

describe('stripErrorEcho', () => {
  const err = `${QUOTA} (${CHAIN})`;
  it('removes the [error] line the error box repeats', () => {
    expect(stripErrorEcho(`[error] ${QUOTA}`, err)).toBe('');
  });
  it("keeps the agent's text before the marker", () => {
    expect(stripErrorEcho(`Working on it.\n[error] ${QUOTA}`, err)).toBe('Working on it.');
  });
  it('keeps an [error] line whose cause the run error does not state', () => {
    expect(stripErrorEcho('[error] something else entirely happened', err)).toBe('[error] something else entirely happened');
  });
  it('does nothing without a run error', () => {
    expect(stripErrorEcho(`[error] ${QUOTA}`, undefined)).toBe(`[error] ${QUOTA}`);
  });
});

describe('resetZoneNote', () => {
  const CLAUDE = "You've hit your session limit · resets 1pm (Asia/Saigon)";

  it('says nothing without a parsed reset time or outside a rate-limit error', () => {
    expect(resetZoneNote(QUOTA, { provider: 'glm', rateLimited: true, hasReset: false })).toBeNull();
    expect(resetZoneNote(CLAUDE, { provider: 'claude-native', rateLimited: true, hasReset: false, inferred: true })).toBeNull();
    expect(resetZoneNote(QUOTA, { provider: 'glm', rateLimited: false, hasReset: true, inferred: true })).toBeNull();
  });

  it('names UTC+8 for GLM and the printed IANA zone for Claude', () => {
    expect(resetZoneNote(QUOTA, { provider: 'glm', rateLimited: true, hasReset: true, inferred: true })).toContain('UTC+8');
    expect(resetZoneNote(CLAUDE, { provider: 'claude-native', rateLimited: true, hasReset: true })).toContain('Asia/Saigon');
  });

  it("an inferred zone-less time is the daemon's; an absolute instant gets no note", () => {
    expect(resetZoneNote('resets 3pm', { provider: 'claude-native', rateLimited: true, hasReset: true, inferred: true })).toContain("daemon's time zone");
    expect(resetZoneNote('usage limit reached|1790000000', { provider: 'claude-native', rateLimited: true, hasReset: true })).toBeNull();
  });
});

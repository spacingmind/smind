// runFooter.test.ts covers describeRunFooter's wording (the port of web
// PR #373 / ADR-0027 D13): disposition-driven sentences for done runs cut
// at a cap, the legacy stop-reason fallback for older daemons, and the
// no-footer cases.

import { describe, expect, it } from 'vitest';
import { describeContinues, describeRunFooter } from '../runFooter';

describe('describeRunFooter', () => {
  it('needs-continue on a done run reads "Hit the tool-turn limit · not finished" with a Continue action', () => {
    expect(
      describeRunFooter({ Status: 'done', StopReason: 'max_turn_requests', Disposition: 'needs-continue' }),
    ).toEqual({
      sentence: 'Hit the tool-turn limit · not finished',
      tone: 'warning',
      action: 'continue',
    });
  });

  it('max_tokens with the disposition reads "Stopped at the output limit · not finished"', () => {
    expect(describeRunFooter({ Status: 'done', StopReason: 'max_tokens', Disposition: 'needs-continue' })).toEqual({
      sentence: 'Stopped at the output limit · not finished',
      tone: 'warning',
      action: 'continue',
    });
  });

  it('budget_exceeded reads "Stopped at the budget limit · not finished"', () => {
    expect(describeRunFooter({ Status: 'done', StopReason: 'budget_exceeded', Disposition: 'needs-continue' })).toEqual(
      {
        sentence: 'Stopped at the budget limit · not finished',
        tone: 'warning',
        action: 'continue',
      },
    );
  });

  it('an unknown cap with the disposition reads "Stopped at a limit · not finished"', () => {
    expect(describeRunFooter({ Status: 'done', StopReason: 'something_new', Disposition: 'needs-continue' })).toEqual({
      sentence: 'Stopped at a limit · not finished',
      tone: 'warning',
      action: 'continue',
    });
  });

  it('an older daemon (no disposition) falls back to the stop reason for max_turn_requests and max_tokens', () => {
    expect(describeRunFooter({ Status: 'done', StopReason: 'max_turn_requests' })).toEqual({
      sentence: 'Hit the tool-turn limit',
      tone: 'warning',
      action: 'continue',
    });
    expect(describeRunFooter({ Status: 'done', StopReason: 'max_tokens' })).toEqual({
      sentence: 'Stopped at the output limit',
      tone: 'warning',
      action: 'continue',
    });
  });

  it('the disposition is ignored unless the run is done', () => {
    for (const status of ['running', 'error', 'stopped', 'interrupted'] as const) {
      expect(describeRunFooter({ Status: status, StopReason: 'max_tokens', Disposition: 'needs-continue' })).toBeNull();
    }
  });

  it('a normal done run has no footer', () => {
    expect(describeRunFooter({ Status: 'done', StopReason: 'end_turn' })).toBeNull();
    expect(describeRunFooter({ Status: 'done' })).toBeNull();
  });

  describe('rate limited (ADR-0041)', () => {
    // Local-time fixtures: the sentence is local HH:MM. 2026-10-13 is a Tuesday.
    const now = new Date(2026, 9, 13, 10, 0, 0);
    const limited = { Status: 'error' as const, FailureKind: 'rate_limited', FailureWindow: '5h' };

    it('footer has ≈ for inferred reset', () => {
      const at = new Date(2026, 9, 13, 17, 35).toISOString();
      expect(describeRunFooter({ ...limited, FailureResetAt: at, FailureResetInferred: true }, now)?.sentence).toBe('Rate limited (5h window) · resets ≈17:35');
      expect(describeRunFooter({ ...limited, FailureResetAt: new Date(2026, 9, 15, 9, 5).toISOString(), FailureResetInferred: true }, now)?.sentence).toBe(
        'Rate limited (5h window) · resets Thu ≈09:05',
      );
    });

    it('stated reset unchanged', () => {
      const at = new Date(2026, 9, 13, 17, 35).toISOString();
      expect(describeRunFooter({ ...limited, FailureResetAt: at, FailureResetInferred: false }, now)?.sentence).toBe('Rate limited (5h window) · resets 17:35');
      expect(describeRunFooter({ ...limited, FailureResetAt: at }, now)?.sentence).toBe('Rate limited (5h window) · resets 17:35');
    });

    it('rate limited with a future reset', () => {
      expect(describeRunFooter({ ...limited, FailureResetAt: new Date(2026, 9, 13, 17, 35).toISOString() }, now)).toEqual({
        sentence: 'Rate limited (5h window) · resets 17:35',
        tone: 'warning',
        action: 'continue',
      });
    });

    it('rate limited reset today vs another day', () => {
      expect(describeRunFooter({ ...limited, FailureResetAt: new Date(2026, 9, 13, 23, 59).toISOString() }, now)?.sentence).toBe(
        'Rate limited (5h window) · resets 23:59',
      );
      expect(describeRunFooter({ ...limited, FailureResetAt: new Date(2026, 9, 15, 9, 5).toISOString() }, now)?.sentence).toBe(
        'Rate limited (5h window) · resets Thu 09:05',
      );
      expect(
        describeRunFooter({ ...limited, FailureWindow: undefined, FailureResetAt: new Date(2026, 9, 13, 17, 35).toISOString() }, now)?.sentence,
      ).toBe('Rate limited · resets 17:35');
    });

    it('rate limited with unknown reset', () => {
      expect(describeRunFooter({ ...limited, FailureResetAt: null }, now)?.sentence).toBe('Rate limited (5h window) · reset time unknown');
      expect(describeRunFooter(limited, now)?.sentence).toBe('Rate limited (5h window) · reset time unknown');
      expect(describeRunFooter({ ...limited, FailureResetAt: 'nope' }, now)?.sentence).toBe('Rate limited (5h window) · reset time unknown');
    });

    it('rate limited after the reset passed', () => {
      expect(describeRunFooter({ ...limited, FailureResetAt: new Date(2026, 9, 13, 9, 0).toISOString() }, now)?.sentence).toBe(
        'Rate limited (5h window) · limit has reset',
      );
    });

    it('plain error footer unchanged: still none', () => {
      expect(describeRunFooter({ Status: 'error' }, now)).toBeNull();
      expect(describeRunFooter({ Status: 'error', FailureKind: 'other' }, now)).toBeNull();
      expect(describeRunFooter({ Status: 'done', FailureKind: 'rate_limited' }, now)).toBeNull();
    });
  });
});

describe('failover context (ADR-0041)', () => {
  const limited = { Status: 'error', FailureKind: 'rate_limited', FailureWindow: '5h', FailureResetAt: null } as const;
  const now = new Date(2026, 9, 23, 10, 0);

  it('queued note has ≈ when the reset was inferred', () => {
    const v = describeRunFooter({ ...limited, FailureResetInferred: true }, now, { heldUntil: new Date(2026, 9, 23, 17, 35) });
    expect(v?.note).toBe('Queued · starts after ≈17:35');
  });

  it('wait state shows queued note', () => {
    const v = describeRunFooter(limited, now, { heldUntil: new Date(2026, 9, 23, 17, 35) });
    expect(v?.note).toBe('Queued · starts after 17:35');
    expect(v?.waiting).toBe(true);
    expect(v?.action).toBe('continue');
    expect(describeRunFooter(limited, now, { heldUntil: new Date(2026, 9, 24, 9, 5) })?.note).toMatch(/^Queued · starts after \w{3} 09:05$/);
  });

  it('no note without held retry', () => {
    const v = describeRunFooter(limited, now, { heldUntil: null });
    expect(v?.note).toBeUndefined();
    expect(v?.waiting).toBeUndefined();
  });

  it('continued on text', () => {
    expect(describeRunFooter(limited, now, { continuedOn: { provider: 'GLM', chatId: 3 } })?.note).toBe('continued on GLM in chat #3');
  });

  it('continues hint', () => {
    expect(describeContinues({ FailoverOf: '0123456789abcdef' })).toBe('continues run 01234567');
    expect(describeContinues({})).toBeNull();
  });

  it('existing cases unchanged', () => {
    expect(describeRunFooter(limited, now)).toEqual({
      sentence: 'Rate limited (5h window) · reset time unknown',
      tone: 'warning',
      action: 'continue',
    });
    expect(describeRunFooter({ Status: 'done' }, now, { heldUntil: new Date(2026, 9, 23, 17, 35) })).toBeNull();
  });
});

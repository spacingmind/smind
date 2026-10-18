// runFooter.test.ts covers describeRunFooter's wording (the port of web
// PR #373 / ADR-0027 D13): disposition-driven sentences for done runs cut
// at a cap, the legacy stop-reason fallback for older daemons, and the
// no-footer cases.

import { describe, expect, it } from 'vitest';
import { describeRunFooter } from '../runFooter';

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
});

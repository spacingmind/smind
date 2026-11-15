// permissionCardView.test.ts: the pure copy rules of the cards -- a resolution never names an option id, the error is
// a plain sentence plus the technical detail, and the deny reason field stays hidden until its toggle opens it.

import { describe, expect, it } from 'vitest';
import { errorCopy, resolvedCopy, showReasonField, showReasonToggle } from '../permissionCardView';

describe('permissionCardView', () => {
  it('resolved-copy-by-kind', () => {
    expect(resolvedCopy({ by: 'tap', optionKind: 'reject' })).toBe('You denied this');
    expect(resolvedCopy({ by: 'tap', optionKind: 'reject_forever' })).toBe('You denied this');
    expect(resolvedCopy({ by: 'tap', optionKind: 'allow' })).toBe('You allowed this');
    expect(resolvedCopy({ by: 'tap', optionKind: 'allow_once' })).toBe('You allowed this');
    expect(resolvedCopy({ by: 'tap', optionKind: 'ask_user' })).toBe('You answered this');
    expect(resolvedCopy({ by: 'tap', optionKind: '' })).toBe('You answered this');
    expect(resolvedCopy({ by: 'tap', optionKind: undefined })).toBe('You answered this');
    expect(resolvedCopy({ by: 'tap' })).toBe('You answered this');
    // the daemon's resolved event overwrites a tap: a human reason still reads as the person's own answer
    expect(resolvedCopy({ by: 'event', reason: 'human', optionKind: 'reject_once' })).toBe('You denied this');
    expect(resolvedCopy({ by: 'event', reason: 'human', optionKind: 'allow_once' })).toBe('You allowed this');
    expect(resolvedCopy({ by: 'event', reason: 'timeout', optionKind: 'reject_once' })).toBe('Timed out');
    expect(resolvedCopy({ by: 'event', reason: 'policy_deny' })).toBe('Denied by policy');
    expect(resolvedCopy({ by: 'event', reason: 'policy_allow' })).toBe('Allowed by policy');
    expect(resolvedCopy({ by: 'event', reason: 'auto_accept' })).toBe('Auto-accepted');
    expect(resolvedCopy({ by: 'event', reason: 'provider_cancellation' })).toBe('Cancelled by the agent');
    // unknown reason, or an older daemon with none and no tap: plain "Resolved", never the raw wire text
    expect(resolvedCopy({ by: 'event', reason: 'something_new' })).toBe('Resolved');
    expect(resolvedCopy({ by: 'event' })).toBe('Resolved');
    expect(resolvedCopy({ by: 'event', optionKind: 'reject' })).toBe('Resolved');
    expect(resolvedCopy({})).toBe('Resolved');
    expect(resolvedCopy({ optionKind: 'reject' })).toBe('Resolved');
  });

  it('resolved-copy-never-shows-an-option-id', () => {
    const kinds = ['reject', 'reject_once', 'allow', 'allow_forever', 'ask_user', '', undefined];
    const ids = ['', 'deny-1', 'allow_once', 'reject-2', 'chosen:opt-9', undefined];
    for (const by of ['tap', 'event', undefined] as const) {
      for (const kind of kinds) {
        for (const reason of [undefined, 'human', 'timeout', 'deny-1', 'chose:deny-1']) {
          const text = resolvedCopy({ by, optionKind: kind, reason });
          expect(text).not.toContain('?');
          expect(text).not.toContain('chose');
          for (const id of ids) {
            if (!id) continue; // '' is "contained" in every string; the copy is built from no id at all
            expect(text, `${text} vs ${id}`).not.toContain(id);
          }
        }
      }
    }
  });

  it('error-copy-is-plain-with-detail', () => {
    const copy = errorCopy('run.respondPermission: connection closed');
    expect(copy.headline).toBe('Could not send your answer. Try again.');
    expect(copy.detail).toBe('run.respondPermission: connection closed');
    expect(errorCopy('').headline).toBe('Could not send your answer. Try again.');
  });

  it('reason-hidden-until-deny-with-reason', () => {
    expect(showReasonField({ hasReject: true, expanded: false })).toBe(false);
    expect(showReasonField({ hasReject: false, expanded: true })).toBe(false);
    expect(showReasonField({ hasReject: false, expanded: false })).toBe(false);
    expect(showReasonField({ hasReject: true, expanded: true })).toBe(true);
  });

  it('reason-toggle-only-with-reject', () => {
    expect(showReasonToggle(true)).toBe(true);
    expect(showReasonToggle(false)).toBe(false);
  });
});

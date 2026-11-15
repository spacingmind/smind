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
    expect(resolvedCopy({ by: 'event' })).toBe('Resolved by the agent');
    expect(resolvedCopy({ by: 'event', optionKind: 'reject' })).toBe('Resolved by the agent');
    expect(resolvedCopy({})).toBe('Resolved by the agent');
    expect(resolvedCopy({ optionKind: 'reject' })).toBe('Resolved by the agent');
  });

  it('resolved-copy-never-shows-an-option-id', () => {
    const kinds = ['reject', 'reject_once', 'allow', 'allow_forever', 'ask_user', '', undefined];
    const ids = ['', 'deny-1', 'allow_once', 'reject-2', 'chosen:opt-9', undefined];
    for (const by of ['tap', 'event', undefined] as const) {
      for (const kind of kinds) {
        const text = resolvedCopy({ by, optionKind: kind });
        expect(text).not.toContain('?');
        expect(text).not.toContain('chose');
        for (const id of ids) {
          if (!id) continue; // '' is "contained" in every string; the copy is built from no id at all
          expect(text, `${text} vs ${id}`).not.toContain(id);
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

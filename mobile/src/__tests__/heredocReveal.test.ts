// heredocReveal.test.ts covers the toggle state machine of the mobile
// heredoc reveal (permission-heredoc-reveal plan design 7): the label flips,
// an open from idle fetches exactly once and never again (the redacted body
// is cached), a failure shows its message until Retry, and a loaded body
// survives being closed and reopened.

import { describe, expect, it } from 'vitest';
import type { HeredocReveal } from '../api';
import { heredocToggleLabel, initialRevealState, nextOnToggle, onFailed, onLoaded, onRetry } from '../transcript/heredocReveal';

const body: HeredocReveal = { index: 0, bytes: 42, text: 'TOKEN=***', redacted: true, truncated: false };

describe('heredocToggleLabel', () => {
  it('labels the toggle with the byte count and the direction', () => {
    expect(heredocToggleLabel(42, false)).toBe('Show heredoc (42 bytes)');
    expect(heredocToggleLabel(42, true)).toBe('Hide heredoc (42 bytes)');
  });
});

describe('nextOnToggle', () => {
  it('opening from idle fetches once', () => {
    const { state, fetch } = nextOnToggle(initialRevealState);
    expect(state).toEqual({ open: true, status: 'loading' });
    expect(fetch).toBe(true);
  });

  it('reopening a loaded heredoc never fetches again (the body is cached)', () => {
    const loaded = { open: false, status: 'loaded', body } as const;
    const { state, fetch } = nextOnToggle(loaded);
    expect(state).toEqual({ open: true, status: 'loaded', body });
    expect(fetch).toBe(false);
  });

  it('opening from error shows the error and does not fetch; Retry does', () => {
    const errored = onFailed(initialRevealState, 'not found');
    const { state, fetch } = nextOnToggle(errored);
    expect(state).toEqual({ open: true, status: 'error', error: 'not found' });
    expect(fetch).toBe(false);
    expect(onRetry(state)).toEqual({ ...state, status: 'loading' });
  });

  it('a loaded body keeps through close and reopen', () => {
    const open = onLoaded(nextOnToggle(initialRevealState).state, body);
    const closed = nextOnToggle(open);
    expect(closed.state).toEqual({ open: false, status: 'loaded', body });
    expect(closed.fetch).toBe(false);
    const reopened = nextOnToggle(closed.state);
    expect(reopened.state).toEqual({ open: true, status: 'loaded', body });
    expect(reopened.fetch).toBe(false);
  });

  it('onLoaded caches the body and clears the error', () => {
    const errored = onFailed({ open: true, status: 'loading' }, 'boom');
    expect(onLoaded(errored, body)).toEqual({ open: true, status: 'loaded', body });
  });
});

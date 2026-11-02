// heredocReveal.ts is the reveal-on-demand logic of the permission card and
// the tool-call row's "Show heredoc (N bytes)" toggle (permission-heredoc-
// reveal plan, design 6-7), kept pure so it can be unit-tested without
// react-native: a body is fetched at most once (the redacted body is cached
// in the state and re-shown as-is on reopen), a failure stays visible until
// Retry, and closing never drops what was loaded.

import type { HeredocReveal } from '../api';

/** "Show heredoc (42 bytes)" / "Hide heredoc (42 bytes)" -- the toggle's whole label. */
export function heredocToggleLabel(bytes: number, open: boolean): string {
  return `${open ? 'Hide' : 'Show'} heredoc (${bytes} bytes)`;
}

/** One heredoc row's state; `body` survives close and reopen. */
export type RevealState = {
  open: boolean;
  status: 'idle' | 'loading' | 'loaded' | 'error';
  body?: HeredocReveal;
  error?: string;
};

export const initialRevealState: RevealState = { open: false, status: 'idle' };

/**
 * The toggle's transition: closing keeps everything; opening from idle
 * fetches once; reopening a loaded or errored row never fetches (a cached
 * body re-shows as-is; an error re-shows with its Retry, which is the only
 * refetch path).
 */
export function nextOnToggle(state: RevealState): { state: RevealState; fetch: boolean } {
  if (state.open) return { state: { ...state, open: false }, fetch: false };
  if (state.status === 'idle') return { state: { ...state, open: true, status: 'loading' }, fetch: true };
  return { state: { ...state, open: true }, fetch: false };
}

/** The reveal landed: cache the body, clear any error. */
export function onLoaded(state: RevealState, body: HeredocReveal): RevealState {
  return { ...state, status: 'loaded', body, error: undefined };
}

/** The reveal failed: show the message until Retry or close. */
export function onFailed(state: RevealState, message: string): RevealState {
  return { ...state, status: 'error', error: message };
}

/** Retry: loading again; the caller refetches (and the body, if any, stays until it lands). */
export function onRetry(state: RevealState): RevealState {
  return { ...state, status: 'loading' };
}

// historySource.test.ts covers ADR-0053's skeleton/rebuilt contract as the
// task screen consumes it: every table row's exact wording (the web UI's
// strings, kept identical), canLoadFull's gating, the null cases, and
// historyNoteView's three render states.

import { describe, expect, it } from 'vitest';
import { describeHistorySource, HistorySourceInfo, historyNoteView } from '../historySource';

describe('describeHistorySource', () => {
  const CASES: Array<{ reason: string; tone: 'neutral' | 'warning'; message: string; canLoad: boolean }> = [
    { reason: 'rebuild_not_requested', tone: 'neutral', message: 'Showing a summary: large tool output was trimmed.', canLoad: true },
    { reason: 'provider_cannot_rebuild', tone: 'neutral', message: 'Showing a summary: this provider does not keep a history smind can read.', canLoad: false },
    { reason: 'native_session_missing', tone: 'warning', message: 'Showing a summary: the provider’s session file is no longer there.', canLoad: false },
    { reason: 'native_session_in_use', tone: 'neutral', message: 'Showing a summary: this chat is running. Try again when it ends.', canLoad: true },
    { reason: 'slice_not_found', tone: 'warning', message: 'Showing a summary: this turn could not be matched in the provider’s session.', canLoad: false },
    { reason: 'format_drift', tone: 'warning', message: 'Showing a summary: the provider’s session format was not recognised.', canLoad: false },
    { reason: 'rebuild_failed', tone: 'warning', message: 'Showing a summary: the full history could not be loaded (it may be too large or took too long).', canLoad: true },
    { reason: 'rebuild_busy', tone: 'neutral', message: 'Showing a summary: another history is loading. Try again in a moment.', canLoad: true },
  ];

  it('renders each skeleton reason with the web UI’s exact string, tone and loadability (canLoadFull true)', () => {
    for (const c of CASES) {
      expect(describeHistorySource({ source: 'skeleton', reason: c.reason, canLoadFull: true })).toEqual({
        tone: c.tone,
        message: c.message,
        canLoad: c.canLoad,
      });
    }
  });

  it('canLoadFull false (or absent) means canLoad false for every reason, loadable or not', () => {
    for (const c of CASES) {
      expect(describeHistorySource({ source: 'skeleton', reason: c.reason, canLoadFull: false })?.canLoad, c.reason).toBe(false);
      expect(describeHistorySource({ source: 'skeleton', reason: c.reason })?.canLoad, c.reason).toBe(false);
    }
  });

  it('an unknown or absent reason falls back to the generic summary sentence, never loadable', () => {
    expect(describeHistorySource({ source: 'skeleton', reason: 'something_new', canLoadFull: true })).toEqual({
      tone: 'neutral',
      message: 'Showing a summary of this run.',
      canLoad: false,
    });
    expect(describeHistorySource({ source: 'skeleton', canLoadFull: true })).toEqual({
      tone: 'neutral',
      message: 'Showing a summary of this run.',
      canLoad: false,
    });
  });

  it('rebuilt reads as the full history, with nothing left to load', () => {
    expect(describeHistorySource({ source: 'rebuilt', canLoadFull: true })).toEqual({
      tone: 'neutral',
      message: 'Full history, rebuilt from the provider’s session.',
      canLoad: false,
    });
  });

  it('a full, absent or unknown source says nothing at all', () => {
    expect(describeHistorySource({ source: 'full' })).toBeNull();
    expect(describeHistorySource({})).toBeNull();
    expect(describeHistorySource({ source: 'something_else', reason: 'rebuild_not_requested' })).toBeNull();
  });
});

describe('historyNoteView', () => {
  const loadable: HistorySourceInfo = { source: 'skeleton', reason: 'rebuild_not_requested', canLoadFull: true };
  const notLoadable: HistorySourceInfo = { source: 'skeleton', reason: 'provider_cannot_rebuild', canLoadFull: true };

  it('a loadable note idle shows the button, not the loading text', () => {
    expect(historyNoteView(loadable, false)).toEqual({
      message: 'Showing a summary: large tool output was trimmed.',
      tone: 'neutral',
      showButton: true,
      showLoading: false,
    });
  });

  it('a loadable note in flight swaps the button for the loading text', () => {
    expect(historyNoteView(loadable, true)).toEqual({
      message: 'Showing a summary: large tool output was trimmed.',
      tone: 'neutral',
      showButton: false,
      showLoading: true,
    });
  });

  it('a note with nothing to load shows neither, whatever loading says', () => {
    for (const loading of [false, true]) {
      expect(historyNoteView(notLoadable, loading)).toEqual({
        message: 'Showing a summary: this provider does not keep a history smind can read.',
        tone: 'neutral',
        showButton: false,
        showLoading: false,
      });
    }
  });

  it('renders nothing when describeHistorySource says nothing', () => {
    expect(historyNoteView({ source: 'full' }, false)).toBeNull();
    expect(historyNoteView({}, true)).toBeNull();
  });
});

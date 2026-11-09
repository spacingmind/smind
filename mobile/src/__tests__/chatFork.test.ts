// chatFork.test.ts covers the fork feature's pure decisions (ADR-0057 PR
// 1b): the wording (confirm, banner, divider, reason labels), the
// inherited-turns slicing over run.list's newest-first order, and the
// fork_unavailable refusal's three outcomes.

import { describe, expect, it } from 'vitest';
import {
  canForkRun,
  classifyForkError,
  earlierTurnsLabel,
  forkBannerText,
  forkConfirmText,
  forkDividerLabel,
  forkReasonLabel,
  forkTurnNumber,
  inheritedUpTo,
  isTransientForkReason,
} from '../chatFork';
import type { ChatForkInfo } from '../api';
import { DaemonError } from '../relay/RelayConnection';

const fork: ChatForkInfo = { parentChatId: 5, forkedAtRunId: 'r2', kind: 'fork', mode: 'native', lossy: false };
/** run.list's newest-first order: r3 is the latest turn, r1 the oldest. */
const runs = [
  { ID: 'r3', Prompt: 'third' },
  { ID: 'r2', Prompt: 'second' },
  { ID: 'r1', Prompt: 'first' },
];

describe('forkReasonLabel', () => {
  it("labels every daemon reason, 'unavailable' for the rest", () => {
    expect(forkReasonLabel('fork_unsupported')).toBe('this agent has no session fork');
    expect(forkReasonLabel('native_session_missing')).toBe('its session file is gone');
    expect(forkReasonLabel('native_session_in_use')).toBe('the chat is still running');
    expect(forkReasonLabel('slice_not_found')).toBe('this turn cannot be told apart in the session');
    expect(forkReasonLabel('format_drift')).toBe('the session format was not recognised');
    expect(forkReasonLabel('provider_cannot_rebuild')).toBe('no session was recorded for this turn');
    expect(forkReasonLabel('rebuild_busy')).toBe('the daemon is busy, try again');
    expect(forkReasonLabel('something_new')).toBe('unavailable');
    expect(forkReasonLabel(undefined)).toBe('unavailable');
  });
});

describe('isTransientForkReason', () => {
  it('is true only for the passing refusals', () => {
    expect(isTransientForkReason('native_session_in_use')).toBe(true);
    expect(isTransientForkReason('rebuild_busy')).toBe(true);
    expect(isTransientForkReason('fork_unsupported')).toBe(false);
    expect(isTransientForkReason(undefined)).toBe(false);
  });
});

describe('inheritedUpTo', () => {
  it('returns the runs up to and including runId in oldest-first order; empty when absent; the first run alone', () => {
    expect(inheritedUpTo(runs, 'r2')).toEqual([
      { ID: 'r1', Prompt: 'first' },
      { ID: 'r2', Prompt: 'second' },
    ]);
    expect(inheritedUpTo(runs, 'r3')).toEqual(runs.slice().reverse());
    expect(inheritedUpTo(runs, 'gone')).toEqual([]);
    expect(inheritedUpTo(runs, 'r1')).toEqual([{ ID: 'r1', Prompt: 'first' }]);
  });
});

describe('forkTurnNumber', () => {
  it('is the 1-based position in the oldest-first list; null when absent', () => {
    expect(forkTurnNumber(runs, 'r1')).toBe(1);
    expect(forkTurnNumber(runs, 'r2')).toBe(2);
    expect(forkTurnNumber(runs, 'r3')).toBe(3);
    expect(forkTurnNumber(runs, 'gone')).toBeNull();
  });
});

describe('canForkRun', () => {
  it('is true only for a done run', () => {
    expect(canForkRun({ Status: 'done' })).toBe(true);
    for (const status of ['running', 'error', 'stopped', 'interrupted']) {
      expect(canForkRun({ Status: status })).toBe(false);
    }
    expect(canForkRun(null)).toBe(false);
  });
});

describe('forkConfirmText', () => {
  it('native: names the chat and turn, files are not changed', () => {
    expect(forkConfirmText('Fix crash', 2, false)).toEqual({
      title: 'From "Fix crash", turn 2',
      message: 'Continue in a new chat from this turn. The agent keeps its memory of the conversation up to here. Files are not changed.',
      confirmLabel: 'Fork',
    });
  });
  it('fallback: says why and what the summary costs, labelled "Fork with a summary"', () => {
    expect(forkConfirmText('Fix crash', 2, true, 'fork_unsupported')).toEqual({
      title: 'From "Fix crash", turn 2',
      message:
        "The agent's own session cannot be branched here (this agent has no session fork). You can start a new chat that carries a short account of the earlier turns instead: the agent will not remember tool calls or its reasoning.",
      confirmLabel: 'Fork with a summary',
    });
  });
  it('without a turn the title is just the chat title (native and fallback)', () => {
    expect(forkConfirmText('Fix crash', null, false).title).toBe('Fix crash');
    expect(forkConfirmText('Fix crash', null, true).title).toBe('Fix crash');
  });
});

describe('classifyForkError', () => {
  it('fork_unavailable with seededAvailable offers the seeded fork, carrying the reason', () => {
    expect(classifyForkError(new DaemonError('cannot fork', 'fork_unavailable', { reason: 'fork_unsupported', seededAvailable: true }))).toEqual({
      kind: 'offer-seeded',
      reason: 'fork_unsupported',
    });
  });
  it('a transient reason stays a plain error that says to try again', () => {
    expect(classifyForkError(new DaemonError('cannot fork', 'fork_unavailable', { reason: 'native_session_in_use', seededAvailable: true }))).toEqual({
      kind: 'error',
      message: 'The session cannot be forked right now: the chat is still running.',
    });
    expect(classifyForkError(new DaemonError('cannot fork', 'fork_unavailable', { reason: 'rebuild_busy' }))).toEqual({
      kind: 'error',
      message: 'The session cannot be forked right now: the daemon is busy, try again.',
    });
  });
  it('seededAvailable false is an error with the daemon message', () => {
    expect(classifyForkError(new DaemonError('that run is not finished', 'fork_unavailable', { reason: 'bad_run', seededAvailable: false }))).toEqual({
      kind: 'error',
      message: 'that run is not finished',
    });
  });
  it('another code and a plain Error keep their own message; a non-Error is stringified', () => {
    expect(classifyForkError(new DaemonError('push is off', 'push_disabled'))).toEqual({ kind: 'error', message: 'push is off' });
    expect(classifyForkError(new Error('network went away'))).toEqual({ kind: 'error', message: 'network went away' });
    expect(classifyForkError('boom')).toEqual({ kind: 'error', message: 'boom' });
  });
});

describe('forkBannerText', () => {
  it('fork and retry name the parent and the turn', () => {
    expect(forkBannerText(fork, 'Fix crash', 2)).toBe('Forked from Fix crash at turn 2.');
    expect(forkBannerText({ ...fork, kind: 'retry' }, 'Fix crash', 2)).toBe('Retry from Fix crash after turn 2.');
  });
  it('a missing (or archived) parent is "another chat"; no turn drops the tail', () => {
    expect(forkBannerText(fork, null, 2)).toBe('Forked from another chat at turn 2.');
    expect(forkBannerText(fork, 'Fix crash', null)).toBe('Forked from Fix crash.');
    expect(forkBannerText({ ...fork, kind: 'retry' }, null, null)).toBe('Retry from another chat.');
  });
});

describe('forkDividerLabel', () => {
  it('separates by fork kind', () => {
    expect(forkDividerLabel(fork)).toBe('Forked here');
    expect(forkDividerLabel({ ...fork, kind: 'retry' })).toBe('Retry starts here');
  });
});

describe('earlierTurnsLabel', () => {
  it('is singular for one turn only', () => {
    expect(earlierTurnsLabel(1)).toBe('1 earlier turn');
    expect(earlierTurnsLabel(3)).toBe('3 earlier turns');
  });
});

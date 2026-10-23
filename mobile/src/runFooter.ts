// runFooter.ts is the pure view model for the task screen's not-finished
// footer (a port of web PR #373 / ADR-0027 D13): when a run ended "done"
// but the daemon says it was cut off at a turn/token/budget cap, the
// screen shows the same warning sentence the web run footer does plus a
// Continue button. Pure and free of react-native imports so it is
// testable without mounting anything.

import { RunSummary } from './api';

/** The prompt the Continue button sends on the run's own provider (web task-detail.tsx sends the same word). */
export const CONTINUE_PROMPT = 'Continue';

export interface RunFooterView {
  sentence: string;
  tone: 'warning';
  action: 'continue';
}

/** runs.DispositionNeedsContinue. */
const NEEDS_CONTINUE = 'needs-continue';

/** How each cap a daemon reports is worded; an unlisted cap reads "Stopped at a limit". */
const CAP: Record<string, string> = {
  max_tokens: 'Stopped at the output limit',
  max_turn_requests: 'Hit the tool-turn limit',
  budget_exceeded: 'Stopped at the budget limit',
};

/** runs.FailureKindRateLimited (ADR-0041). */
const RATE_LIMITED = 'rate_limited';

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

/**
 * The rate-limit sentence, worded like the web run footer: "Rate limited (5h
 * window) · resets 17:35" (local time, "Mon 17:35" on another day), "· reset
 * time unknown" with no usable reset, "· limit has reset" once it passed. The
 * window part is omitted when unknown.
 */
function rateLimitSentence(window: string | undefined, resetAt: string | null | undefined, now: Date): string {
  const head = `Rate limited${window ? ` (${window} window)` : ''}`;
  const reset = resetAt ? new Date(resetAt) : null;
  if (reset === null || Number.isNaN(reset.getTime())) return `${head} · reset time unknown`;
  if (reset.getTime() <= now.getTime()) return `${head} · limit has reset`;
  const hhmm = `${String(reset.getHours()).padStart(2, '0')}:${String(reset.getMinutes()).padStart(2, '0')}`;
  const sameDay =
    reset.getFullYear() === now.getFullYear() && reset.getMonth() === now.getMonth() && reset.getDate() === now.getDate();
  return `${head} · resets ${sameDay ? hhmm : `${WEEKDAYS[reset.getDay()]} ${hhmm}`}`;
}

/**
 * The footer for one run, or null when it needs none (not finished-yet,
 * stopped, a plain failure, or a normal done). A run that failed on a
 * provider quota (ADR-0041) gets the warning sentence; `now` is injectable
 * for tests. The disposition, not a list of stop
 * reasons, decides, so a cap this client has never heard of still reads
 * as needing a continue; an older daemon sends no disposition and the
 * stop-reason cases cover the two caps it can report.
 */
export function describeRunFooter(
  run: Pick<RunSummary, 'Status' | 'StopReason' | 'Disposition' | 'FailureKind' | 'FailureWindow' | 'FailureResetAt'>,
  now: Date = new Date(),
): RunFooterView | null {
  if (run.Status === 'error' && run.FailureKind === RATE_LIMITED) {
    // The Continue button re-submits on the run's own provider (TaskDetailScreen's handleContinue), which is the recovery path once the limit lifts.
    return { sentence: rateLimitSentence(run.FailureWindow || undefined, run.FailureResetAt, now), tone: 'warning', action: 'continue' };
  }
  if (run.Status !== 'done') return null;
  if (run.Disposition === NEEDS_CONTINUE) {
    return {
      sentence: `${CAP[run.StopReason ?? ''] ?? 'Stopped at a limit'} · not finished`,
      tone: 'warning',
      action: 'continue',
    };
  }
  switch (run.StopReason) {
    case 'max_tokens':
      return { sentence: 'Stopped at the output limit', tone: 'warning', action: 'continue' };
    case 'max_turn_requests':
      return { sentence: 'Hit the tool-turn limit', tone: 'warning', action: 'continue' };
    default:
      return null;
  }
}

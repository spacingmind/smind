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
  /** A second muted line: "Queued · starts after 17:35" while a held retry exists, "continued on GLM in chat #3" once a failover successor exists. */
  note?: string;
  /** True while a held retry is queued: the UI labels or hides Continue (the daemon already has the retry). */
  waiting?: boolean;
}

/** What the screen knows beyond the run itself (ADR-0041 failover); every part optional. */
export interface RunFooterContext {
  /** When the held retry for this run starts (its queue item's NotBefore). */
  heldUntil?: Date | null;
  /** The failover successor's provider and chat. */
  continuedOn?: { provider: string; chatId: number } | null;
  continuesRunId?: string | null;
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
function clock(at: Date, now: Date): string {
  const hhmm = `${String(at.getHours()).padStart(2, '0')}:${String(at.getMinutes()).padStart(2, '0')}`;
  const sameDay = at.getFullYear() === now.getFullYear() && at.getMonth() === now.getMonth() && at.getDate() === now.getDate();
  return sameDay ? hhmm : `${WEEKDAYS[at.getDay()]} ${hhmm}`;
}

/** "continues run <first 8 chars>" for a failover successor, or null when the run continues nothing. */
export function describeContinues(run: Pick<RunSummary, 'FailoverOf'>): string | null {
  return run.FailoverOf ? `continues run ${run.FailoverOf.slice(0, 8)}` : null;
}

function rateLimitSentence(window: string | undefined, resetAt: string | null | undefined, now: Date): string {
  const head = `Rate limited${window ? ` (${window} window)` : ''}`;
  const reset = resetAt ? new Date(resetAt) : null;
  if (reset === null || Number.isNaN(reset.getTime())) return `${head} · reset time unknown`;
  if (reset.getTime() <= now.getTime()) return `${head} · limit has reset`;
  return `${head} · resets ${clock(reset, now)}`;
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
  ctx?: RunFooterContext,
): RunFooterView | null {
  if (run.Status === 'error' && run.FailureKind === RATE_LIMITED) {
    // The Continue button re-submits on the run's own provider (TaskDetailScreen's handleContinue), which is the recovery path once the limit lifts.
    const view: RunFooterView = {
      sentence: rateLimitSentence(run.FailureWindow || undefined, run.FailureResetAt, now),
      tone: 'warning',
      action: 'continue',
    };
    const held = ctx?.heldUntil;
    if (held && !Number.isNaN(held.getTime())) {
      view.note = `Queued · starts after ${clock(held, now)}`;
      view.waiting = true;
    } else if (ctx?.continuedOn) {
      view.note = `continued on ${ctx.continuedOn.provider} in chat #${ctx.continuedOn.chatId}`;
    }
    return view;
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

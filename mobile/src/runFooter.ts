// runFooter.ts is the pure view model for the task screen's not-finished
// footer (a port of web PR #373 / ADR-0027 D13): when a run ended "done"
// but the daemon says it was cut off at a turn/token/budget cap, the
// screen shows the same warning sentence the web run footer does plus a
// Continue button. Pure and free of react-native imports so it is
// testable without mounting anything.

import { RunSummary } from './api';

/** The prompt the Continue button sends on the run's own provider (web task-detail.tsx sends the same word). */
export const CONTINUE_PROMPT = 'Continue';

/**
 * The prompt Continue sends for `run`: the plain word for a capped run, one
 * line naming the failure for an errored one (J1; the same text as the web
 * footer's continuePrompt in run-footer-model.ts).
 */
export function continuePrompt(run: Pick<RunSummary, 'Status' | 'FailureKind'>): string {
  if (run.Status !== 'error') return CONTINUE_PROMPT;
  const label = FAILURE_LABELS[run.FailureKind ?? ''] ?? 'an unexpected error';
  return `Continue from where you left off (the previous run ended unexpectedly: ${label}).`;
}

const FAILURE_LABELS: Record<string, string> = {
  cli_exited: 'the agent CLI exited without a result',
  rate_limited: 'a provider rate limit, which has since reset',
};

export interface RunFooterView {
  sentence: string;
  tone: 'warning';
  /** null: the footer explains the state but offers nothing (a rate limit that has not reset yet). */
  action: 'continue' | null;
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
function clock(at: Date, now: Date, inferred = false): string {
  const hhmm = `${inferred ? '≈' : ''}${String(at.getHours()).padStart(2, '0')}:${String(at.getMinutes()).padStart(2, '0')}`;
  const sameDay = at.getFullYear() === now.getFullYear() && at.getMonth() === now.getMonth() && at.getDate() === now.getDate();
  return sameDay ? hhmm : `${WEEKDAYS[at.getDay()]} ${hhmm}`;
}

/** "continues run <first 8 chars>" for a failover successor, or null when the run continues nothing. */
export function describeContinues(run: Pick<RunSummary, 'FailoverOf'>): string | null {
  return run.FailoverOf ? `continues run ${run.FailoverOf.slice(0, 8)}` : null;
}

function rateLimitSentence(window: string | undefined, resetAt: string | null | undefined, now: Date, inferred = false): string {
  const head = `Rate limited${window ? ` (${window} window)` : ''}`;
  const reset = resetAt ? new Date(resetAt) : null;
  if (reset === null || Number.isNaN(reset.getTime())) return `${head} · reset time unknown`;
  if (reset.getTime() <= now.getTime()) return `${head} · limit has reset`;
  return `${head} · resets ${clock(reset, now, inferred)}`;
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
  run: Pick<RunSummary, 'Status' | 'StopReason' | 'Disposition' | 'FailureKind' | 'FailureWindow' | 'FailureResetAt' | 'FailureResetInferred'>,
  now: Date = new Date(),
  ctx?: RunFooterContext,
): RunFooterView | null {
  if (run.Status === 'error' && run.FailureKind === RATE_LIMITED) {
    // Continue (TaskDetailScreen's handleContinue) resumes on the run's own provider. The daemon decides when that is offered
    // (J1: its quota has reset and the chat has a stored session), so before the reset the banner only explains.
    const view: RunFooterView = {
      sentence: rateLimitSentence(run.FailureWindow || undefined, run.FailureResetAt, now, run.FailureResetInferred),
      tone: 'warning',
      action: run.Disposition === NEEDS_CONTINUE ? 'continue' : null,
    };
    const held = ctx?.heldUntil;
    if (held && !Number.isNaN(held.getTime())) {
      view.note = `Queued · starts after ${clock(held, now, run.FailureResetInferred)}`;
      view.waiting = true;
    } else if (ctx?.continuedOn) {
      view.note = `continued on ${ctx.continuedOn.provider} in chat #${ctx.continuedOn.chatId}`;
    }
    return view;
  }
  // J1: a CLI that died without a result is resumable when the daemon says so (it has a stored session).
  if (run.Status === 'error' && run.FailureKind === 'cli_exited' && run.Disposition === NEEDS_CONTINUE) {
    return { sentence: 'Failed · the agent CLI exited without a result', tone: 'warning', action: 'continue' };
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

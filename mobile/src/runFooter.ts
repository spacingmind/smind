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

/**
 * The footer for one run, or null when it needs none (not finished-yet,
 * failed, stopped, or a normal done). The disposition, not a list of stop
 * reasons, decides, so a cap this client has never heard of still reads
 * as needing a continue; an older daemon sends no disposition and the
 * stop-reason cases cover the two caps it can report.
 */
export function describeRunFooter(
  run: Pick<RunSummary, 'Status' | 'StopReason' | 'Disposition'>,
): RunFooterView | null {
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

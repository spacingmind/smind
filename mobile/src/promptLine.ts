// promptLine.ts: a run started by run.start does not carry its own prompt
// in run.logs, so the finished-run load path must show the user's line from
// the run row's Prompt when the logs have none. Pure, per providerCatalog's
// discipline.

import { TimelineLine } from './runTimeline';

/**
 * Lines with the run's own prompt prepended as a user line, when the logs
 * carry no user line at all (a run.start-initiated run) and the prompt is
 * non-empty. The `p` key prefix is the scheme the adoption path uses, so
 * isEchoOfOptimisticLine still dedupes an echo against it. Returns the
 * input array (same reference) when nothing is prepended; never mutates.
 */
export function withRunPrompt(lines: readonly TimelineLine[], prompt: string): TimelineLine[] {
  if (prompt === '' || lines.some((l) => l.role === 'user')) return lines as TimelineLine[];
  return [{ key: `p${prompt}`, role: 'user', text: prompt } as TimelineLine, ...lines];
}

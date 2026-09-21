// followUpPrompt.ts is Milestone 3 Item 1's send-a-follow-up-prompt logic,
// kept out of the component so it can be unit-tested against a fake or
// harness-backed connection (the same discipline as api.ts). Sending is:
// optimistically append the typed text as a user line (before any server
// round trip), then run.start with the most recent run's provider, then
// run.attach on the returned runId over the same attach/detach path the
// screen uses for viewing a run -- deliberately NOT task.prompt, whose
// request-scoped cancellation would stop the run. A run.start failure
// rolls the optimistic line back so the caller can re-show the draft for
// a retry.

import { startRun } from './api';
import { RelayConnection } from './relay/RelayConnection';
import { lineFromAttachEvent, TimelineLine } from './runTimeline';

/** The live tail of one run: append events, and cancel() to detach. */
export interface RunTail {
  cancel(): void;
}

export interface SendFollowUpDeps {
  /** Appends lines to the on-screen timeline (optimistic user line + streamed events). */
  appendLines: (lines: TimelineLine[]) => void;
  /** Removes lines from the timeline (rollback of the optimistic user line on failure). */
  removeLines: (lines: TimelineLine[]) => void;
  /** Registers the new run's attach so navigating away cancels it, exactly like the initial run's. */
  trackTail: (tail: RunTail) => void;
}

/**
 * Sends one follow-up prompt on a task whose most recent run's provider
 * is reused. The typed text renders as a user line immediately; the new
 * run's events then append live to the same timeline. On failure the
 * optimistic line is removed and the error re-thrown for the screen to
 * show next to the compose box (with the draft restored for retry).
 */
export async function sendFollowUpPrompt(
  conn: RelayConnection,
  deps: SendFollowUpDeps,
  taskId: number,
  provider: string,
  prompt: string,
  nextSeq: () => number,
): Promise<void> {
  const userLine = { key: `f${nextSeq()}`, role: 'user', text: prompt } as TimelineLine;
  deps.appendLines([userLine]);
  let runId: string;
  try {
    runId = await startRun(conn, taskId, provider, prompt);
  } catch (err) {
    deps.removeLines([userLine]);
    throw err;
  }
  const attach = conn.call('run.attach', { runId }, {
    onEvent: (event, params) => {
      const newLines = lineFromAttachEvent(event, params, nextSeq());
      if (newLines.length > 0) deps.appendLines(newLines);
    },
  });
  deps.trackTail(attach);
  attach.catch(() => {
    // Detach (navigate-away cancel) or the connection ending: the new
    // run's streamed events simply stop, same as the initial run's tail.
  });
}

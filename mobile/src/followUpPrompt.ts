// followUpPrompt.ts is Milestone 3 Item 1's send-a-follow-up-prompt logic,
// kept out of the component so it can be unit-tested against a fake or
// harness-backed connection (the same discipline as api.ts). Sending is:
// optimistically append the typed text as a user line (before any server
// round trip), then run.start with the most recent run's provider, then
// run.attach on the returned runId over the same attach/detach path the
// screen uses for viewing a run -- deliberately NOT task.prompt, whose
// request-scoped cancellation would stop the run. A run.start failure
// rolls the optimistic line back so the caller can re-show the draft for
// a retry. With opts.whenBusy (ADR-0021 §9) a busy chat may park the
// prompt in the daemon's queue instead: the optimistic line is removed
// (the queue UI shows the prompt) and nothing attaches.

import { RunSummary, RunStartResult, startRun, startRunWhenBusy, WhenBusy } from './api';
import { attachmentPayloads } from './attachSend';
import { PendingAttachment } from './attachments';
import { lineAttachmentsFromPending } from './lineAttachments';
import { continuePrompt } from './runFooter';
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
  /** Sees every raw attach event, with the run's real id, before line rendering (permission events route to PermissionBoard here; they render as no lines). */
  onEvent?: (event: string, params: unknown, runId: string) => void;
  /** Return false when another path (a run.status adoption) already attached this run. */
  shouldAttach?: (runId: string) => boolean;
}

/** Optional send options: the busy-chat delivery mode and its target chat. */
export interface SendFollowUpOpts {
  whenBusy?: WhenBusy;
  chatId?: number;
  /** ADR-0038: the composer's pending attachments; the optimistic user line shows them and run.start carries their payloads. */
  attachments?: PendingAttachment[];
}

/**
 * Sends one follow-up prompt on a task whose most recent run's provider
 * is reused. The typed text renders as a user line immediately; the new
 * run's events then append live to the same timeline. On failure the
 * optimistic line is removed and the error re-thrown for the screen to
 * show next to the compose box (with the draft restored for retry).
 * With whenBusy set, a queued result instead removes the optimistic line
 * (the queue UI owns showing that prompt) and returns without attaching.
 */
export async function sendFollowUpPrompt(
  conn: RelayConnection,
  deps: SendFollowUpDeps,
  taskId: number,
  provider: string,
  prompt: string,
  nextSeq: () => number,
  opts?: SendFollowUpOpts,
): Promise<RunStartResult> {
  const userLine = { key: `f${nextSeq()}`, role: 'user', text: prompt } as TimelineLine;
  const pending = opts?.attachments ?? [];
  const lineAttachments = lineAttachmentsFromPending(pending);
  if (lineAttachments) userLine.attachments = lineAttachments;
  const payloads = attachmentPayloads(pending);
  deps.appendLines([userLine]);
  let result: RunStartResult;
  try {
    result = opts?.whenBusy
      ? await startRunWhenBusy(conn, { taskId, chatId: opts.chatId, provider, prompt, whenBusy: opts.whenBusy, attachments: payloads })
      : { kind: 'started', runId: await startRun(conn, taskId, provider, prompt, payloads) };
  } catch (err) {
    deps.removeLines([userLine]);
    throw err;
  }
  if (result.kind === 'queued') {
    // The daemon parked the prompt in the chat's queue; its own UI row
    // shows it, so the optimistic line stands down and nothing attaches.
    deps.removeLines([userLine]);
    return result;
  }
  const runId = result.runId;
  if (deps.shouldAttach && !deps.shouldAttach(runId)) {
    // Another path (a run.status adoption that beat this response) already
    // attached this run. The optimistic line STAYS: the daemon does not
    // synthesise the prompt as an event, so an agent that does not echo it
    // would leave the transcript without the user's own line. A stream that
    // does echo it is deduped against this line (isEchoOfOptimisticLine).
    return result;
  }
  // run.attach backfills the new run's whole recorded history before the
  // live tail (internal/runs Registry.Subscribe) -- which includes the
  // prompt's own user_message. The optimistic line already rendered it,
  // so the backfilled echo is skipped exactly once.
  let skippedPromptEcho = false;
  const attach = conn.call('run.attach', { runId }, {
    onEvent: (event, params) => {
      if (
        event === 'user_message' &&
        !skippedPromptEcho &&
        (params as { text?: string }).text === prompt
      ) {
        skippedPromptEcho = true;
        return;
      }
      deps.onEvent?.(event, params, runId);
      const newLines = lineFromAttachEvent(event, params, nextSeq());
      if (newLines.length > 0) deps.appendLines(newLines);
    },
  });
  deps.trackTail(attach);
  attach.catch(() => {
    // Detach (navigate-away cancel) or the connection ending: the new
    // run's streamed events simply stop, same as the initial run's tail.
  });
  return result;
}

/**
 * Whether a streamed event is the agent's echo of a prompt this client
 * already rendered itself (a user_message whose text equals a client-
 * rendered user line: those keyed `f<seq>` by sendFollowUpPrompt, or
 * `p<runId>` for the prompt line the screen adds for an adopted run):
 * the screen skips it so the prompt shows once, whichever of the
 * run.start response and the run.status adoption attached the run first.
 */
export function isEchoOfOptimisticLine(lines: readonly TimelineLine[], event: string, params: unknown): boolean {
  if (event !== 'user_message') return false;
  const text = (params as { text?: string } | undefined)?.text;
  return (
    text !== undefined &&
    lines.some((l) => l.role === 'user' && /^[fp]/.test(l.key) && l.text === text)
  );
}

/**
 * The not-finished footer's Continue action (ADR-0027 D13): a follow-up
 * run with the prompt "Continue" on the capped run's own provider. An
 * idle chat starts at once; a busy one queues -- the same as the web's
 * Continue.
 */
export function continueRun(
  conn: RelayConnection,
  deps: SendFollowUpDeps,
  taskId: number,
  run: Pick<RunSummary, 'Provider' | 'Status' | 'FailureKind'>,
  nextSeq: () => number,
  chatId?: number,
): Promise<RunStartResult> {
  return sendFollowUpPrompt(conn, deps, taskId, run.Provider, continuePrompt(run), nextSeq, { whenBusy: 'queue', chatId });
}

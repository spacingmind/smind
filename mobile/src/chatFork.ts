// chatFork.ts is the fork feature's one table of decisions (ADR-0057 PR
// 1b): the refusal reasons as words, which runs a native fork still
// remembers, the confirm texts and the banner/divider wording -- shared
// with the web UI so the two never drift. Pure -- no react-native
// import -- per theme.ts/icons.ts's discipline for vitest's plain
// transform; the screens' components stay thin views over it.

import type { ChatForkInfo } from './api';
import { DaemonError } from './relay/RelayConnection';

/** The daemon's refusal reasons (internal/history/native Reason values) as words -- the web's forkReasonLabel verbatim. */
export function forkReasonLabel(reason?: string): string {
  switch (reason) {
    case 'fork_unsupported':
      return 'this agent has no session fork';
    case 'native_session_missing':
      return 'its session file is gone';
    case 'native_session_in_use':
      return 'the chat is still running';
    case 'slice_not_found':
      return 'this turn cannot be told apart in the session';
    case 'format_drift':
      return 'the session format was not recognised';
    case 'provider_cannot_rebuild':
      return 'no session was recorded for this turn';
    case 'rebuild_busy':
      return 'the daemon is busy, try again';
    default:
      return 'unavailable';
  }
}

/** Refusals that pass (AC19): the chat is still running or the daemon is busy, so no lossy offer -- try again later. */
export function isTransientForkReason(reason?: string): boolean {
  return reason === 'native_session_in_use' || reason === 'rebuild_busy';
}

/**
 * The parent's runs up to and including runId -- the turns a native
 * fork's agent still remembers. run.list answers newest first, so the
 * runs are reversed to oldest-first here; empty when runId is not among
 * them (the parent's history no longer reaches the fork point).
 */
export function inheritedUpTo<T extends { ID: string }>(runs: T[], runId: string): T[] {
  const oldest = [...runs].reverse();
  const index = oldest.findIndex((run) => run.ID === runId);
  return index === -1 ? [] : oldest.slice(0, index + 1);
}

/** The fork point's 1-based position in the chat's oldest-first runs -- the number the confirm and banner name; null when absent. */
export function forkTurnNumber(runs: Array<{ ID: string }>, runId: string): number | null {
  const inherited = inheritedUpTo(runs, runId);
  return inherited.length > 0 ? inherited.length : null;
}

/** "Fork from here" sits on a finished latest run only (AC19): none while live, none on error/stopped/interrupted. */
export function canForkRun(run: { Status: string } | null): boolean {
  return run !== null && run.Status === 'done';
}

/**
 * The confirm dialog's texts (the platform confirmNative): the native
 * attempt first; `fallback` is the seeded offer a fork_unavailable
 * refusal with seededAvailable produced (same words as the web dialog).
 */
export function forkConfirmText(
  chatTitle: string,
  turn: number | null,
  fallback: boolean,
  reason?: string,
): { title: string; message: string; confirmLabel: string } {
  const title = turn !== null ? `From "${chatTitle}", turn ${turn}` : chatTitle;
  if (!fallback) {
    return {
      title,
      message: 'Continue in a new chat from this turn. The agent keeps its memory of the conversation up to here. Files are not changed.',
      confirmLabel: 'Fork',
    };
  }
  return {
    title,
    message: `The agent's own session cannot be branched here (${forkReasonLabel(reason)}). You can start a new chat that carries a short account of the earlier turns instead: the agent will not remember tool calls or its reasoning.`,
    confirmLabel: 'Fork with a summary',
  };
}

/** A failed native attempt either offers the seeded fork (with the refusal's reason) or stays an inline error (AC19). */
export type ForkErrorView = { kind: 'offer-seeded'; reason?: string } | { kind: 'error'; message: string };

/** chat.fork's refusal, as the screen acts on it: transient stays a plain error, seededAvailable opens the lossy offer, everything else is the error's own text. */
export function classifyForkError(err: unknown): ForkErrorView {
  if (err instanceof DaemonError && err.code === 'fork_unavailable') {
    const data = (err.data ?? {}) as { reason?: string; seededAvailable?: boolean };
    if (isTransientForkReason(data.reason)) {
      return { kind: 'error', message: `The session cannot be forked right now: ${forkReasonLabel(data.reason)}.` };
    }
    if (data.seededAvailable === true) return { kind: 'offer-seeded', reason: data.reason };
  }
  return { kind: 'error', message: err instanceof Error ? err.message : String(err) };
}

/** The banner above a forked chat's transcript (AC20): "another chat" for an archived or missing parent, and no turn number when the fork point is no longer among the runs. */
export function forkBannerText(fork: ChatForkInfo, parentTitle: string | null, turn: number | null): string {
  const parent = parentTitle ?? 'another chat';
  if (fork.kind === 'retry') return turn !== null ? `Retry from ${parent} after turn ${turn}.` : `Retry from ${parent}.`;
  return turn !== null ? `Forked from ${parent} at turn ${turn}.` : `Forked from ${parent}.`;
}

/** The divider between the inherited turns and the chat's own (AC21). */
export function forkDividerLabel(fork: Pick<ChatForkInfo, 'kind'>): string {
  return fork.kind === 'retry' ? 'Retry starts here' : 'Forked here';
}

/** The inherited block's collapsed header (AC21). */
export function earlierTurnsLabel(n: number): string {
  return n === 1 ? '1 earlier turn' : `${n} earlier turns`;
}

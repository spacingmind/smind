// historySource.ts maps run.logs' history-provenance fields (ADR-0053's
// skeleton/rebuilt contract) to the note the task screen shows above a
// finished run's transcript: one table of exact strings, shared wording
// with the web UI, so the two never drift. Pure -- no react-native
// import -- per theme.ts/icons.ts's discipline for vitest's plain
// transform; the screen's rebuilt-history merge lives here for the same
// reason.

import { withRunPrompt } from './promptLine';
import { LineAttachment, TimelineLine, timelineFromLogs } from './runTimeline';

/** Where a run.logs result's events came from, plus whether the full history can be asked for. */
export interface HistorySourceInfo {
  source?: string;
  reason?: string;
  canLoadFull?: boolean;
  /** Loading the full history starts the agent (an ACP run): ask the human first. */
  loadStartsAgent?: boolean;
}

/** run.logs' result shape as this screen reads it: the events plus ADR-0053's provenance fields. */
export interface RunLogsResult {
  runId?: string;
  events?: unknown[];
  historySource?: 'full' | 'skeleton' | 'rebuilt';
  skeletonReason?: string;
  canLoadFull?: boolean;
  loadStartsAgent?: boolean;
}

export interface HistorySourceNote {
  tone: 'neutral' | 'warning';
  message: string;
  canLoad: boolean;
}

/**
 * The one table of skeleton texts. `loadable` marks the reasons a full
 * history may still be fetched for (rebuild_not_requested and the
 * retryable reasons); the daemon's canLoadFull gates it either way.
 * Every other reason states why the summary is all there is.
 */
const SKELETON: Record<string, { tone: 'neutral' | 'warning'; message: string; loadable: boolean }> = {
  rebuild_not_requested: { tone: 'neutral', message: 'Showing a summary: large tool output was trimmed.', loadable: true },
  provider_cannot_rebuild: { tone: 'neutral', message: 'Showing a summary: this provider does not keep a history smind can read.', loadable: false },
  native_session_missing: { tone: 'warning', message: 'Showing a summary: the provider’s session file is no longer there.', loadable: false },
  native_session_in_use: { tone: 'neutral', message: 'Showing a summary: this chat is running. Try again when it ends.', loadable: true },
  slice_not_found: { tone: 'warning', message: 'Showing a summary: this turn could not be matched in the provider’s session.', loadable: false },
  format_drift: { tone: 'warning', message: 'Showing a summary: the provider’s session format was not recognised.', loadable: false },
  rebuild_failed: { tone: 'warning', message: 'Showing a summary: the full history could not be loaded (it may be too large or took too long).', loadable: true },
  confirm_required: { tone: 'neutral', message: 'Showing a summary: loading this history needs your confirmation.', loadable: true },
  rebuild_busy: { tone: 'neutral', message: 'Showing a summary: another history is loading. Try again in a moment.', loadable: true },
  excerpt_pruned: { tone: 'neutral', message: 'Showing a summary: large tool output was removed after the retention period.', loadable: true },
};

const SKELETON_FALLBACK = { tone: 'neutral', message: 'Showing a summary of this run.', loadable: false } as const;

/**
 * The note a transcript shows for a run.logs result's provenance: null
 * when there is nothing to say (a full history, an absent or unknown
 * source -- an older daemon), else the table's tone/message and whether
 * the full history can be loaded.
 */
export function describeHistorySource(info: HistorySourceInfo): HistorySourceNote | null {
  if (info.source === 'rebuilt') {
    return { tone: 'neutral', message: 'Full history, rebuilt from the provider’s session.', canLoad: false };
  }
  if (info.source !== 'skeleton') return null;
  const row = SKELETON[info.reason ?? ''] ?? SKELETON_FALLBACK;
  return { tone: row.tone, message: row.message, canLoad: row.loadable && info.canLoadFull === true };
}

/** The provenance fields of a run.logs result, as component state keeps them. */
export function historySourceInfo(result: RunLogsResult): HistorySourceInfo {
  return {
    source: result.historySource,
    reason: result.skeletonReason,
    canLoadFull: result.canLoadFull,
    ...(result.loadStartsAgent ? { loadStartsAgent: true } : {}),
  };
}

/** The question asked before a load that starts the agent (same words as the web UI). "30 seconds" is native.DefaultACPTimeout in the daemon (internal/history/native/rebuild.go): keep them in step. */
export const LOAD_STARTS_AGENT_CONFIRM = {
  title: 'Start the agent to load this history?',
  message: 'This starts the agent to read this run’s history from its own session. It can take up to 30 seconds. No prompt is sent.',
  confirmLabel: 'Start agent',
} as const;

/** run.logs params for "Load full history": confirmStart only when the load starts the agent (the human has just confirmed). */
export function fullHistoryParams(runId: string, info: HistorySourceInfo | null | undefined): { runId: string; detail: 'full'; confirmStart?: true } {
  return { runId, detail: 'full', ...(info?.loadStartsAgent ? { confirmStart: true as const } : {}) };
}

export interface HistoryNoteView {
  message: string;
  tone: 'neutral' | 'warning';
  showButton: boolean;
  showLoading: boolean;
}

/**
 * HistorySourceNote's three render states, kept here so the component is
 * a pure view: null renders nothing; otherwise the message with either a
 * "Load full history" button (loadable, idle), a "Loading…" text
 * (loadable, in flight) or neither (the summary is all there is).
 */
export function historyNoteView(info: HistorySourceInfo, loading: boolean): HistoryNoteView | null {
  const note = describeHistorySource(info);
  if (note === null) return null;
  return { message: note.message, tone: note.tone, showButton: note.canLoad && !loading, showLoading: note.canLoad && loading };
}

/**
 * The transcript replacement for a successful detail:'full' run.logs
 * call: the rebuilt events as lines, with the initial load's own run-
 * prompt treatment (a run.start run's logs carry no user message), and
 * any lines appended since the load (an optimistic prompt) kept after.
 */
export function applyFullHistoryResult(
  prev: readonly TimelineLine[],
  result: { runId?: string; events?: unknown[] },
  prompt: string,
  attachments?: LineAttachment[],
): TimelineLine[] {
  const rebuilt = timelineFromLogs({ runId: result.runId, events: (result.events ?? []) as never[] });
  if (prev.some((l) => l.role === 'user' && l.text === prompt)) return [...rebuilt, ...prev];
  return [...withRunPrompt(rebuilt, prompt, attachments), ...prev];
}

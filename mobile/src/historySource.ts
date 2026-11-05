// historySource.ts maps run.logs' history-provenance fields (ADR-0053's
// skeleton/rebuilt contract) to the note the task screen shows above a
// finished run's transcript: one table of exact strings, shared wording
// with the web UI, so the two never drift. Pure -- no react-native
// import -- per theme.ts/icons.ts's discipline for vitest's plain
// transform.

/** Where a run.logs result's events came from, plus whether the full history can be asked for. */
export interface HistorySourceInfo {
  source?: string;
  reason?: string;
  canLoadFull?: boolean;
}

/** run.logs' result shape as this screen reads it: the events plus ADR-0053's provenance fields. */
export interface RunLogsResult {
  runId?: string;
  events?: unknown[];
  historySource?: 'full' | 'skeleton' | 'rebuilt';
  skeletonReason?: string;
  canLoadFull?: boolean;
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
  rebuild_busy: { tone: 'neutral', message: 'Showing a summary: another history is loading. Try again in a moment.', loadable: true },
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
  return { source: result.historySource, reason: result.skeletonReason, canLoadFull: result.canLoadFull };
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

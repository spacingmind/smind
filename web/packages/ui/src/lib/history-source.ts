import type { RunHistorySource } from "@/lib/types";

/** What a run's `history` carries (run.attach/run.logs historySource, skeletonReason, canLoadFull) -- the shape RunEntry.history stores. */
export interface HistorySourceInfo {
  source?: RunHistorySource;
  reason?: string;
  canLoadFull?: boolean;
}

/** The note HistorySourceNote renders: which tone of box, the exact sentence, and whether the "Load full history" button may show. */
export interface HistorySourceNote {
  tone: "info" | "warning";
  message: string;
  canLoad: boolean;
}

interface SkeletonRow {
  tone: "info" | "warning";
  message: string;
  /** The reasons a human can act on offer the rebuild button (and only when canLoadFull says the daemon can): the first ask, and a retry after a failure that may pass (busy, running chat, too slow). */
  loadable?: boolean;
}

/**
 * One row per skeletonReason the daemon sends (ADR-0053), plus the fallback
 * for a reason this build has never heard of. Every sentence is exact copy --
 * wording here is product copy, not an implementation detail.
 */
const SKELETON_ROWS: Record<string, SkeletonRow> = {
  rebuild_not_requested: { tone: "info", message: "Showing a summary: large tool output was trimmed.", loadable: true },
  provider_cannot_rebuild: { tone: "info", message: "Showing a summary: this provider does not keep a history smind can read." },
  native_session_missing: { tone: "warning", message: "Showing a summary: the provider's session file is no longer there." },
  native_session_in_use: { tone: "info", message: "Showing a summary: this chat is running. Try again when it ends.", loadable: true },
  slice_not_found: { tone: "warning", message: "Showing a summary: this turn could not be matched in the provider's session." },
  format_drift: { tone: "warning", message: "Showing a summary: the provider's session format was not recognised." },
  rebuild_failed: { tone: "warning", message: "Showing a summary: the full history could not be loaded (it may be too large or took too long).", loadable: true },
  rebuild_busy: { tone: "info", message: "Showing a summary: another history is loading. Try again in a moment.", loadable: true },
};

const UNKNOWN_ROW: SkeletonRow = { tone: "info", message: "Showing a summary of this run." };

const REBUILT_NOTE: HistorySourceNote = { tone: "info", message: "Full history, rebuilt from the provider's session.", canLoad: false };

/**
 * The note a run's history state renders. Callers show no banner at all for
 * source "full" (or no history) -- this function describes the two states
 * that do get one: a trimmed summary (keyed by skeletonReason) and a
 * successful rebuild.
 */
export function describeHistorySource(info: HistorySourceInfo): HistorySourceNote {
  if (info.source === "rebuilt") return REBUILT_NOTE;
  const row = info.reason !== undefined ? SKELETON_ROWS[info.reason] : undefined;
  const chosen = row ?? UNKNOWN_ROW;
  return { tone: chosen.tone, message: chosen.message, canLoad: chosen.loadable === true && info.canLoadFull === true };
}

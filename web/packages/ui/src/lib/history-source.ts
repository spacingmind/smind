import type { TimelineItem } from "@/hooks/use-run-timeline";
import type { RunHistorySource } from "@/lib/types";

/** What a run's `history` carries (run.attach/run.logs historySource, skeletonReason, canLoadFull) -- the shape RunEntry.history stores. */
export interface HistorySourceInfo {
  source?: RunHistorySource;
  reason?: string;
  canLoadFull?: boolean;
  /** Loading the full history starts the agent (ACP run): the Load button asks the human first. */
  loadStartsAgent?: boolean;
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
  confirm_required: { tone: "info", message: "Showing a summary: loading this history needs your confirmation.", loadable: true },
  rebuild_busy: { tone: "info", message: "Showing a summary: another history is loading. Try again in a moment.", loadable: true },
  excerpt_pruned: { tone: "info", message: "Showing a summary: large tool output was removed after the retention period.", loadable: true },
};

const UNKNOWN_ROW: SkeletonRow = { tone: "info", message: "Showing a summary of this run." };

/** rebuild_not_requested's copy when the only thing the skeleton dropped is the reasoning text. */
const REASONING_NOT_KEPT = "Showing a summary: the agent's reasoning was not kept.";

const REBUILT_NOTE: HistorySourceNote = { tone: "info", message: "Full history, rebuilt from the provider's session.", canLoad: false };

/** What the daemon dropped from a skeleton's rows: a trimmed payload, or reasoning that was not kept at all. */
export type TrimKind = "output" | "thinking";

/** The exact marker the daemon writes inside a head+tail trim (internal/runs/skeleton.go), ellipsis U+2026. */
const OMITTED_BYTES = /…\[\d+ bytes omitted\]…/;

/** JSON of a tool payload, false when the value cannot be stringified (a circular object) -- one unreadable item must not sink the scan. */
function toolPayloadHasMarker(value: unknown): boolean {
  try {
    return OMITTED_BYTES.test(JSON.stringify(value ?? ""));
  } catch {
    return false;
  }
}

/**
 * Whether a skeleton's rows are missing anything, judged from the rows
 * themselves: a marker inside an answer, a user message, a tool input or a
 * tool result means a payload was trimmed ("output"); a thinking item with
 * no text at all means the reasoning was not kept ("thinking"). A marker
 * wins over reasoning: one trimmed payload names the larger loss.
 */
export function runWasTrimmed(items: readonly TimelineItem[] | undefined): TrimKind | null {
  if (!items) return null;
  let thinkingDropped = false;
  for (const item of items) {
    if (item.kind === "assistant" || item.kind === "user") {
      if (OMITTED_BYTES.test(item.text)) return "output";
    } else if (item.kind === "tool_call") {
      if (toolPayloadHasMarker(item.input) || toolPayloadHasMarker(item.result)) return "output";
    } else if (item.kind === "thinking" && item.text === "") {
      thinkingDropped = true;
    }
  }
  return thinkingDropped ? "thinking" : null;
}

/**
 * The note a run's history state renders. Callers show no banner at all for
 * source "full" (or no history) -- this function describes the two states
 * that do get one: a trimmed summary (keyed by skeletonReason) and a
 * successful rebuild. `trim` (runWasTrimmed's answer for the same run)
 * switches the rebuild_not_requested copy: that reason alone only means
 * nobody asked for the full history, so what the note names is what the
 * rows actually lost.
 */
export function describeHistorySource(info: HistorySourceInfo, trim?: TrimKind | null): HistorySourceNote {
  if (info.source === "rebuilt") return REBUILT_NOTE;
  const row = info.reason !== undefined ? SKELETON_ROWS[info.reason] : undefined;
  const chosen = row ?? UNKNOWN_ROW;
  const message = info.reason === "rebuild_not_requested" && trim === "thinking" ? REASONING_NOT_KEPT : chosen.message;
  return { tone: chosen.tone, message, canLoad: chosen.loadable === true && info.canLoadFull === true };
}

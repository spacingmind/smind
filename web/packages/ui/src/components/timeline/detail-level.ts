import { useCallback, useState } from "react";

import type { TimelineItem, TimelineToolCallItem } from "@/hooks/use-run-timeline";

/**
 * How much of a tool call the transcript shows, matching Paseo's
 * `toolCallDetailLevel` (`audit-workbench.md` §2). `overview` collapses a run
 * of consecutive tool calls into one row so a long agentic stretch reads
 * as "it did twelve things" rather than twelve cards of scrollback.
 * `auto` (the default) does that only for a FINISHED run with three or more
 * consecutive tool calls: a live run keeps showing each tool as it happens.
 * `detailed` always shows every row.
 */
export type DetailLevel = "auto" | "detailed" | "overview";

/** The header control cycles auto, detailed, overview. */
export const NEXT_DETAIL_LEVEL: Record<DetailLevel, DetailLevel> = { auto: "detailed", detailed: "overview", overview: "auto" };
export const DETAIL_LEVEL_LABELS: Record<DetailLevel, string> = { auto: "Auto", detailed: "Detailed", overview: "Overview" };
export const DETAIL_LEVEL_TITLES: Record<DetailLevel, string> = {
  auto: "Finished runs fold three or more tool calls in a row; click to show every tool call",
  detailed: "Every tool call is shown; click to collapse runs of tool calls",
  overview: "Runs of tool calls are collapsed; click to fold only finished runs",
};

const STORAGE_KEY = "smind:tool-call-detail-level";

function readStored(): DetailLevel {
  if (typeof window === "undefined") return "auto";
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    return stored === "overview" || stored === "detailed" ? stored : "auto";
  } catch {
    return "auto";
  }
}

/**
 * The detail-level preference, persisted like the other client-side
 * preferences in this app (`smind:` prefix, localStorage). Item 13's
 * settings screen is its eventual home; until then the transcript's own
 * header owns it.
 */
export function useDetailLevel(): [DetailLevel, (next: DetailLevel) => void] {
  const [level, setLevelState] = useState<DetailLevel>(() => readStored());

  const setLevel = useCallback((next: DetailLevel) => {
    setLevelState(next);
    if (typeof window === "undefined") return;
    try {
      window.localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // Best-effort persistence; the choice still applies this session.
    }
  }, []);

  return [level, setLevel];
}

/** One rendered row: either a single item, or a collapsed run of tool calls. */
export type TimelineGroup =
  | { kind: "item"; id: string; item: TimelineItem }
  | { kind: "tool-group"; id: string; items: TimelineToolCallItem[] };

/** Overview: runs shorter than this stay as individual cards -- collapsing a single call into a "1 tool call" row hides it behind a click for nothing. */
const MIN_GROUP = 2;

/** Auto: a finished run's tool rows collapse from this many in a row (two stay readable inline). */
export const MIN_AUTO_GROUP = 3;

export interface GroupOptions {
  /** The run has ended. `auto` never collapses a live run. */
  finished?: boolean;
}

/**
 * Groups a transcript for rendering at the given detail level. Pure, and
 * identity-preserving for the `detailed` case's items, so the memoized
 * rows keep bailing out (see TimelineRow).
 */
export function groupTimeline(items: TimelineItem[], level: DetailLevel, options: GroupOptions = {}): TimelineGroup[] {
  if (level === "detailed" || (level === "auto" && options.finished !== true)) {
    return items.map((item) => ({ kind: "item", id: item.id, item }));
  }
  const minGroup = level === "auto" ? MIN_AUTO_GROUP : MIN_GROUP;

  const groups: TimelineGroup[] = [];
  let run: TimelineToolCallItem[] = [];

  const flush = () => {
    if (run.length === 0) return;
    if (run.length < minGroup) {
      for (const item of run) groups.push({ kind: "item", id: item.id, item });
    } else {
      groups.push({ kind: "tool-group", id: `group-${run[0]!.id}`, items: run });
    }
    run = [];
  };

  for (const item of items) {
    // Overview is the quiet view: events this build can't classify (a raw ACP kind, an unknown
    // wire type) are debugging aids, shown in Detailed only.
    if (item.kind === "unknown" && level === "overview") continue;
    if (item.kind === "tool_call") {
      run.push(item);
      continue;
    }
    flush();
    groups.push({ kind: "item", id: item.id, item });
  }
  flush();
  return groups;
}

/**
 * Whether Overview would render anything differently from Detailed for these
 * runs (a run of two or more tool calls to collapse, or an unclassified event
 * to hide). When it would not -- an error-only run, a plain chat -- the
 * Detailed/Overview control has nothing to reveal and the header hides it.
 */
export function detailLevelChangesView(runs: ReadonlyArray<{ items: TimelineItem[] }>): boolean {
  return runs.some((run) => groupTimeline(run.items, "overview").length !== run.items.length);
}

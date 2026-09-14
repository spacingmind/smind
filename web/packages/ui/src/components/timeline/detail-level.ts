import { useCallback, useState } from "react";

import type { TimelineItem, TimelineToolCallItem } from "@/hooks/use-run-timeline";

/**
 * How much of a tool call the transcript shows, matching Paseo's
 * `toolCallDetailLevel` (`audit-paseo.md` §2). `overview` collapses a run
 * of consecutive tool calls into one row so a long agentic stretch reads
 * as "it did twelve things" rather than twelve cards of scrollback.
 */
export type DetailLevel = "detailed" | "overview";

const STORAGE_KEY = "smind:tool-call-detail-level";

function readStored(): DetailLevel {
  if (typeof window === "undefined") return "detailed";
  try {
    return window.localStorage.getItem(STORAGE_KEY) === "overview" ? "overview" : "detailed";
  } catch {
    return "detailed";
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

/** Runs shorter than this stay as individual cards -- collapsing a single call into a "1 tool call" row hides it behind a click for nothing. */
const MIN_GROUP = 2;

/**
 * Groups a transcript for rendering at the given detail level. Pure, and
 * identity-preserving for the `detailed` case's items, so the memoized
 * rows keep bailing out (see TimelineRow).
 */
export function groupTimeline(items: TimelineItem[], level: DetailLevel): TimelineGroup[] {
  if (level === "detailed") return items.map((item) => ({ kind: "item", id: item.id, item }));

  const groups: TimelineGroup[] = [];
  let run: TimelineToolCallItem[] = [];

  const flush = () => {
    if (run.length === 0) return;
    if (run.length < MIN_GROUP) {
      for (const item of run) groups.push({ kind: "item", id: item.id, item });
    } else {
      groups.push({ kind: "tool-group", id: `group-${run[0]!.id}`, items: run });
    }
    run = [];
  };

  for (const item of items) {
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

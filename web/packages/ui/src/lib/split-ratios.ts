import { MIN_SPLIT_SIZE, type SplitRatios } from "@/lib/split-tree";
import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";

/**
 * D3 of docs/plans/active/desktop-tabs.md: the last ratio a person set for a two-pane split in a workspace becomes the starting ratio
 * of the next split created in that workspace (the per-task layout stays authoritative once it exists). One small map in
 * localStorage keyed by `w<workspaceId>` (a non-numeric key keeps insertion order, which the oldest-first cap relies on), `{ w1: { horizontal?, vertical? } }`, each value the FIRST child's fraction.
 * Read and write follow lib/storage.ts: a bad value never throws and reads as "no remembered ratio"; a failed write is dropped.
 * No daemon or desktop-shell change.
 */

/** Workspaces remembered at most; the oldest entry is dropped first, so the map cannot grow without bound. */
export const MAX_REMEMBERED_WORKSPACES = 200;

/** A first-child fraction a split may really have: finite and at least MIN_SPLIT_SIZE from either edge. */
export function isValidSplitFraction(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= MIN_SPLIT_SIZE && value <= 1 - MIN_SPLIT_SIZE;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** Keeps only the entries that are well-formed (a stale or hand-edited payload loses just its bad entries, not the whole map). */
function sanitize(raw: Record<string, unknown>): Record<string, SplitRatios> {
  const out: Record<string, SplitRatios> = {};
  for (const [id, entry] of Object.entries(raw)) {
    if (!isRecord(entry)) continue;
    const ratios: SplitRatios = {};
    if (isValidSplitFraction(entry.horizontal)) ratios.horizontal = entry.horizontal;
    if (isValidSplitFraction(entry.vertical)) ratios.vertical = entry.vertical;
    if (ratios.horizontal !== undefined || ratios.vertical !== undefined) out[id] = ratios;
  }
  return out;
}

function keyOf(workspaceId: number): string {
  return `w${workspaceId}`;
}

function readAll(): Record<string, SplitRatios> {
  return sanitize(readStored<Record<string, unknown>>(STORAGE_KEYS.splitRatios, {}, isRecord));
}

/** The workspace's remembered starting ratios, or undefined when it has none. */
export function readSplitRatios(workspaceId: number): SplitRatios | undefined {
  return readAll()[keyOf(workspaceId)];
}

/** Remembers `firstFraction` as the workspace's starting ratio for `direction`. An invalid fraction is ignored. */
export function rememberSplitRatio(workspaceId: number, direction: "horizontal" | "vertical", firstFraction: number): void {
  if (!isValidSplitFraction(firstFraction)) return;
  const all = readAll();
  const key = keyOf(workspaceId);
  const merged: SplitRatios = { ...all[key], [direction]: firstFraction };
  delete all[key]; // re-inserting moves it to the newest position
  all[key] = merged;
  const ids = Object.keys(all);
  for (const id of ids.slice(0, Math.max(0, ids.length - MAX_REMEMBERED_WORKSPACES))) delete all[id];
  writeStored(STORAGE_KEYS.splitRatios, all);
}

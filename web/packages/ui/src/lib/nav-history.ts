/**
 * Pure model for in-app back/forward navigation history (stage 5 of the
 * ZCode parity plan). The app has no router -- the hash only carries the
 * selected task -- so back/forward walk this list of navigation entries
 * instead of the browser history. All functions are pure: they never
 * mutate the history they are given.
 */

export type NavView = "workspace" | "settings" | "inbox" | "automations";

export interface NavEntry {
  view: NavView;
  taskId: number | null;
}

export interface NavHistory {
  entries: readonly NavEntry[];
  /** Cursor into `entries`; -1 when the history is empty. */
  index: number;
}

/** Oldest entries beyond this many are dropped on push. */
export const NAV_HISTORY_CAP = 50;

export function emptyHistory(): NavHistory {
  return { entries: [], index: -1 };
}

export function sameEntry(a: NavEntry, b: NavEntry): boolean {
  return a.view === b.view && a.taskId === b.taskId;
}

/**
 * Records a navigation: an entry equal to the one under the cursor changes
 * nothing, anything else truncates the forward tail and lands on the new
 * entry, dropping the oldest past `NAV_HISTORY_CAP`.
 */
export function pushEntry(history: NavHistory, entry: NavEntry): NavHistory {
  const cursor = history.entries[history.index];
  if (cursor && sameEntry(cursor, entry)) return history;
  const entries = [...history.entries.slice(0, history.index + 1), entry];
  const overflow = entries.length - NAV_HISTORY_CAP;
  const kept = overflow > 0 ? entries.slice(overflow) : entries;
  return { entries: kept, index: kept.length - 1 };
}

export function canBack(history: NavHistory): boolean {
  return history.index > 0;
}

export function canForward(history: NavHistory): boolean {
  return history.index < history.entries.length - 1;
}

/**
 * Removes an entry (a task that no longer exists): the cursor follows the
 * entry it was on -- it only moves when the removal is at or below it, and
 * never below 0 while entries remain.
 */
export function dropEntry(history: NavHistory, i: number): NavHistory {
  const entries = history.entries.filter((_, j) => j !== i);
  const index =
    i > history.index ? history.index : entries.length === 0 ? -1 : Math.max(0, history.index - 1);
  return { entries, index };
}

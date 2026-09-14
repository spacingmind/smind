/**
 * What the command palette shows and how a query narrows it.
 *
 * A command is plain data plus a `run` closure. Nothing here knows about
 * React or about the palette component -- which is the point: sources
 * register commands (`palette/palette-provider.tsx`), the palette renders
 * whatever is registered, and neither has to edit the other.
 *
 * Shape is `refs/paseo/packages/app/src/command-center/contributions.ts`
 * reduced to the one presentation smind needs (Paseo also has a "choice"
 * kind for its settings-style toggles, which smind has no surface for).
 */

import type { ActionId } from "@/keyboard/actions";

export interface Command {
  /** Unique within its source; the registry namespaces it with the source id. */
  id: string;
  /** Display group heading ("Tasks", "Actions", ...). Commands are grouped by it in the list. */
  group: string;
  title: string;
  subtitle?: string;
  /**
   * Extra terms a query may match that aren't in the visible text -- a
   * branch name, a file's full path, an alias ("dark mode" for "Cycle
   * theme").
   */
  keywords?: readonly string[];
  /**
   * A keyboard action this command is equivalent to. Purely presentational:
   * the palette renders that action's current shortcut on the row, so a
   * rebound shortcut shows its new keys with no work here.
   */
  action?: ActionId;
  run: () => void;
}

export interface CommandSource {
  /** Stable id of the registering surface. Registering again under the same id replaces the previous set. */
  id: string;
  /** Groups sort by this first, so "Tasks" can outrank "Files" regardless of alphabetical order. */
  groupRank: number;
  commands: readonly Command[];
}

/** A command with its source's rank attached, which is what ordering needs. */
export interface RankedCommand extends Command {
  groupRank: number;
  /** `${sourceId}:${command.id}` -- unique across sources, and the React key. */
  key: string;
}

/** Flattens sources into one ranked list, ordered by group then registration order. */
export function flattenSources(sources: readonly CommandSource[]): RankedCommand[] {
  const flat: RankedCommand[] = [];
  const seen = new Set<string>();
  for (const source of sources) {
    for (const command of source.commands) {
      const key = `${source.id}:${command.id}`;
      // Two commands colliding on a key would make the palette's React
      // keys ambiguous and the highlight jump around. Dropping the later
      // one keeps the list usable; the alternative (throwing, as Paseo
      // does) would take the whole app down over a duplicate menu entry.
      if (seen.has(key)) continue;
      seen.add(key);
      flat.push({ ...command, groupRank: source.groupRank, key });
    }
  }
  return flat.sort((a, b) => a.groupRank - b.groupRank || a.group.localeCompare(b.group));
}

/**
 * Whether `query`'s characters appear in `text` in order, and how well.
 *
 * Subsequence matching (not substring) is what makes "tsk" find "Open
 * task" and "apsb" find "app-sidebar.tsx" -- the thing that makes a
 * palette feel fast to type into. Higher is better; `null` is no match.
 */
export function fuzzyScore(text: string, query: string): number | null {
  if (query === "") return 0;
  const haystack = text.toLowerCase();
  const needle = query.toLowerCase();

  let score = 0;
  let from = 0;
  for (const char of needle) {
    const at = haystack.indexOf(char, from);
    if (at === -1) return null;
    // A character right after the previous one, or at a word boundary,
    // scores higher than one found several characters later -- so a
    // contiguous "task" beats a scattered t..a..s..k.
    if (at === from && from > 0) score += 8;
    else if (at === 0 || " -_/.:".includes(haystack[at - 1] ?? "")) score += 5;
    else score += 1;
    from = at + 1;
  }
  // Shorter matches win ties: "Diff" should outrank "Diff viewer settings"
  // for the query "diff".
  return score - haystack.length * 0.01;
}

/**
 * The commands matching `query`, best first.
 *
 * Matching is per-field rather than against one concatenated blob: a query
 * that straddles a title and a keyword ("ta" from "Task" + "sk" from a
 * keyword) is a coincidence, not an intent, and blob matching produces a
 * lot of those. Title matches are weighted above subtitle above keywords.
 */
export function filterCommands(
  commands: readonly RankedCommand[],
  query: string,
): RankedCommand[] {
  const trimmed = query.trim();
  if (trimmed === "") return [...commands];

  const scored: { command: RankedCommand; score: number }[] = [];
  for (const command of commands) {
    const candidates = [
      { text: command.title, weight: 3 },
      { text: command.subtitle ?? "", weight: 1.5 },
      ...(command.keywords ?? []).map((k) => ({ text: k, weight: 1 })),
    ];

    let best: number | null = null;
    for (const { text, weight } of candidates) {
      if (text === "") continue;
      const score = fuzzyScore(text, trimmed);
      if (score === null) continue;
      const weighted = score * weight;
      if (best === null || weighted > best) best = weighted;
    }
    if (best !== null) scored.push({ command, score: best });
  }

  // Results stay *grouped* while being ordered by relevance: a flat
  // score sort would interleave a task, a file and an action on every
  // keystroke, and the group headings would then alternate one per row.
  // So groups are ordered by their best member's score (ties broken by
  // groupRank, the same order an empty query uses), and members are
  // ordered by score within their group.
  const bestByGroup = new Map<string, number>();
  const rankByGroup = new Map<string, number>();
  for (const { command, score } of scored) {
    const currentBest = bestByGroup.get(command.group);
    if (currentBest === undefined || score > currentBest) bestByGroup.set(command.group, score);
    const currentRank = rankByGroup.get(command.group);
    if (currentRank === undefined || command.groupRank < currentRank) {
      rankByGroup.set(command.group, command.groupRank);
    }
  }

  // sort() is stable in every engine this targets, so equal-scoring
  // entries keep their registration order rather than shuffling as the
  // user types.
  return scored
    .sort((a, b) => {
      if (a.command.group !== b.command.group) {
        const groupDelta = bestByGroup.get(b.command.group)! - bestByGroup.get(a.command.group)!;
        if (groupDelta !== 0) return groupDelta;
        const rankDelta = rankByGroup.get(a.command.group)! - rankByGroup.get(b.command.group)!;
        if (rankDelta !== 0) return rankDelta;
        return a.command.group.localeCompare(b.command.group);
      }
      return b.score - a.score;
    })
    .map((s) => s.command);
}

/**
 * Rows to render: each command, with `groupStart` set on the first of each
 * run so the list can draw a heading above it.
 *
 * Headings are a property of a command row rather than rows of their own,
 * which is what makes "arrow navigation skips group headers" true by
 * construction instead of by a skip-loop that has to be kept correct.
 */
export interface CommandRow {
  command: RankedCommand;
  /** The group heading to draw above this row, or null when it continues the previous group. */
  groupStart: string | null;
}

export function toRows(commands: readonly RankedCommand[]): CommandRow[] {
  let previous: string | null = null;
  return commands.map((command) => {
    const groupStart = command.group === previous ? null : command.group;
    previous = command.group;
    return { command, groupStart };
  });
}

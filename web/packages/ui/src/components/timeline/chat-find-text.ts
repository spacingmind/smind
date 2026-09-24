/**
 * The chat-find match model: pure string matching, no DOM. Ported from
 * Paseo's `agent-stream/chat-find/ranges.web.ts` word-joining pattern --
 * splitting the query on whitespace and rejoining with `\s+` means a query
 * typed with a single space still matches text that wrapped across a line
 * break -- minus Paseo's host round-trip, since smind's transcript is
 * already fully loaded on the client
 * (`docs/plans/active/web-find.md`'s Decisions).
 */

export interface TextMatch {
  start: number;
  end: number;
}

/**
 * Builds a case-insensitive, whitespace-tolerant regex for `query`, or
 * `null` for a blank one -- the empty-query-gives-zero-matches case is
 * handled by callers simply having no pattern to search with.
 */
export function buildFindPattern(query: string): RegExp | null {
  const trimmed = query.trim();
  if (!trimmed) return null;
  const pattern = trimmed
    .split(/\s+/)
    .map((part) => part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
    .join("\\s+");
  return new RegExp(pattern, "giu");
}

/** Every match's `[start, end)` offset in `text`, in order. */
export function findMatches(text: string, pattern: RegExp): TextMatch[] {
  const matches: TextMatch[] = [];
  for (const match of text.matchAll(pattern)) {
    matches.push({ start: match.index, end: match.index + match[0].length });
  }
  return matches;
}

export function countMatches(text: string, pattern: RegExp): number {
  return findMatches(text, pattern).length;
}

/** The next match index, wrapping from the last back to the first. `count` zero always yields 0. */
export function nextMatchIndex(current: number, count: number): number {
  if (count === 0) return 0;
  return (current + 1) % count;
}

/** The previous match index, wrapping from the first back to the last. `count` zero always yields 0. */
export function previousMatchIndex(current: number, count: number): number {
  if (count === 0) return 0;
  return (current - 1 + count) % count;
}

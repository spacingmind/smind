import { fuzzyMatch } from "@/lib/fuzzy-match";

/** An active `@query` token ending at the caret: `text.slice(start, end)` is `@` plus the query. */
export interface FileMentionRange {
  start: number;
  end: number;
  query: string;
}

/** Rows the popover shows at most. */
export const MENTION_LIMIT = 30;

const INVALID_QUERY_CHARS = /[\s"'<>]/;

/**
 * The `@` token the caret is in (ported from Paseo's rules): the `@` must start the text or follow whitespace, so an
 * email (`a@b`) or a word never triggers, and the query holds no whitespace or quote.
 */
export function findActiveFileMention({ text, cursorIndex }: { text: string; cursorIndex: number }): FileMentionRange | null {
  const cursor = Math.max(0, Math.min(cursorIndex, text.length));
  const before = text.slice(0, cursor);
  for (let at = before.lastIndexOf("@"); at >= 0; at = at === 0 ? -1 : before.lastIndexOf("@", at - 1)) {
    if (at > 0 && !/\s/.test(before[at - 1]!)) continue;
    const query = before.slice(at + 1);
    if (INVALID_QUERY_CHARS.test(query)) continue;
    return { start: at, end: cursor, query };
  }
  return null;
}

/** `@path`, or `@"path"` when the path has whitespace. */
export function formatMention(path: string): string {
  return /\s/.test(path) ? `@"${path}"` : `@${path}`;
}

/** Replaces the mention with the picked path plus one space; `cursor` is the caret position after that space. */
export function applyFileMentionReplacement({ text, mention, path }: { text: string; mention: FileMentionRange; path: string }): { text: string; cursor: number } {
  const insert = `${formatMention(path)} `;
  const before = text.slice(0, mention.start);
  // The caret may sit before an existing space: do not double it.
  const after = text.slice(mention.end).replace(/^ /, "");
  return { text: `${before}${insert}${after}`, cursor: before.length + insert.length };
}

/** The remembered paths whose `@path` / `@"path"` is still a token in the prompt (the daemon dedupes against the text). */
export function mentionedPaths(text: string, remembered: readonly string[]): string[] {
  const out: string[] = [];
  for (const path of remembered) {
    if (out.includes(path)) continue;
    if (hasToken(text, formatMention(path))) out.push(path);
  }
  return out;
}

function hasToken(text: string, token: string): boolean {
  for (let from = text.indexOf(token); from !== -1; from = text.indexOf(token, from + 1)) {
    const prev = from === 0 ? "" : text[from - 1]!;
    const next = text[from + token.length] ?? "";
    // Not `@src/a.go.bak` for `src/a.go`, and not `x@src/a.go`.
    if ((prev === "" || /\s/.test(prev)) && (next === "" || /[\s,;:!?)\]]/.test(next))) return true;
  }
  return false;
}

export interface RankedPath {
  path: string;
  indices: number[];
}

/** Best matches first (score desc, then shorter path), at most `limit`; an empty query keeps the index's own order. */
export function rankPaths(paths: readonly string[], query: string, limit = MENTION_LIMIT): RankedPath[] {
  if (!query) return paths.slice(0, limit).map((path) => ({ path, indices: [] }));
  const hits: { path: string; score: number; indices: number[] }[] = [];
  for (const path of paths) {
    const m = fuzzyMatch(query, path);
    if (m) hits.push(m);
  }
  hits.sort((a, b) => b.score - a.score || a.path.length - b.path.length);
  return hits.slice(0, limit).map(({ path, indices }) => ({ path, indices }));
}

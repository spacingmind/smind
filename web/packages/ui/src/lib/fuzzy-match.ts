/** One path's fuzzy-match result: its score (higher ranks first) and which character indices matched, for highlighting. */
export interface FuzzyMatch {
  path: string;
  score: number;
  indices: number[];
}

function basenameOf(path: string): string {
  const at = path.lastIndexOf("/");
  return at === -1 ? path : path.slice(at + 1);
}

/**
 * Scores `path` against `query` for the quick-open list (ui-redesign-
 * parity plan, Item 18). Two tiers, chosen for predictability over
 * cleverness:
 *
 * 1. A **substring match on the filename** -- what someone typing a name
 *    they remember gets -- always outranks tier 2, with a bonus for
 *    matching right at the start of the name and a preference for a
 *    shorter name among equal-position matches (both read as "more
 *    exact").
 * 2. A **subsequence match anywhere in the path** -- what someone typing
 *    initials gets (`cmpne` for `components/pane.tsx`) -- with bonuses
 *    for a contiguous run and for starting right at a path segment
 *    boundary (`/` or the start of the string).
 *
 * Returns null when query's characters don't all appear, in order,
 * somewhere in path -- not a match at all, not just a low-scoring one.
 */
export function fuzzyMatch(query: string, path: string): FuzzyMatch | null {
  const q = query.trim().toLowerCase();
  if (!q) return { path, score: 0, indices: [] };

  const base = basenameOf(path);
  const baseLower = base.toLowerCase();
  const baseStart = path.length - base.length;

  const substringAt = baseLower.indexOf(q);
  if (substringAt !== -1) {
    const indices = Array.from({ length: q.length }, (_, i) => baseStart + substringAt + i);
    const score = 1000 + (substringAt === 0 ? 100 : 0) - base.length;
    return { path, score, indices };
  }

  const p = path.toLowerCase();
  let qi = 0;
  const indices: number[] = [];
  let score = 0;
  let prevIndex = -1;
  for (let pi = 0; pi < p.length && qi < q.length; pi++) {
    if (p[pi] !== q[qi]) continue;
    indices.push(pi);
    if (prevIndex === pi - 1) score += 3;
    if (pi === 0 || p[pi - 1] === "/") score += 5;
    prevIndex = pi;
    qi++;
  }
  if (qi < q.length) return null;
  return { path, score: score - path.length * 0.01, indices };
}

/** Every path in `paths` that matches `query`, ranked best first, capped at `limit`. An empty query matches everything, in the index's own order, so an empty quick-open shows a browsable list rather than nothing. */
export function fuzzyFilter(query: string, paths: string[], limit = 50): FuzzyMatch[] {
  if (!query.trim()) {
    return paths.slice(0, limit).map((path) => ({ path, score: 0, indices: [] }));
  }
  const matches: FuzzyMatch[] = [];
  for (const path of paths) {
    const m = fuzzyMatch(query, path);
    if (m) matches.push(m);
  }
  matches.sort((a, b) => b.score - a.score || a.path.localeCompare(b.path));
  return matches.slice(0, limit);
}

// Ranked text matching for the app's pickers, ported from Paseo's
// protocol/src/search/text-match.ts minus the fuzzy/typo tier, match ranges and
// path matching (not needed here). A match is a tier plus the offset it was
// found at; lower is better on both, so callers sort ascending and never have
// to invent a scale.

export interface MatchScore {
  tier: number;
  offset: number;
  spread?: number;
}

export interface MatchOptions {
  /**
   * Match characters in order within one whitespace-delimited word, so `pasbab`
   * finds `paseo-babysit`, but `labdes` cannot join "Label as Design". Defaults to on.
   */
  subsequence?: boolean;
}

/** Exact tiers, best to worst. */
const TIER_EXACT = 0;
const TIER_WHOLE_WORD = 1;
const TIER_PREFIX = 2;
const TIER_WORD_START = 3;
const TIER_SUBSTRING = 4;
const TIER_SUBSEQUENCE = 5;

function isWordBoundaryChar(ch: string | undefined): boolean {
  if (ch === undefined) return true;
  return !/[a-z0-9]/.test(ch);
}

function scoreSubstringMatch(query: string, text: string): MatchScore | null {
  let best: MatchScore | null = null;
  let pos = 0;
  while (pos <= text.length - query.length) {
    const found = text.indexOf(query, pos);
    if (found === -1) break;
    const before = found > 0 ? text[found - 1] : undefined;
    const after = text[found + query.length];
    const startsAtBoundary = found === 0 || isWordBoundaryChar(before);
    const endsAtBoundary = after === undefined || isWordBoundaryChar(after);
    let tier: number;
    if (startsAtBoundary && endsAtBoundary) {
      tier = TIER_WHOLE_WORD;
    } else if (found === 0) {
      tier = TIER_PREFIX;
    } else if (startsAtBoundary) {
      tier = TIER_WORD_START;
    } else {
      tier = TIER_SUBSTRING;
    }
    if (!best || tier < best.tier || (tier === best.tier && found < best.offset)) {
      best = { tier, offset: found };
    }
    pos = found + 1;
  }
  return best;
}

function scoreSubsequenceMatch(query: string, text: string): MatchScore | null {
  let queryIndex = 0;
  let firstIndex = -1;
  let lastIndex = -1;
  for (let textIndex = 0; textIndex < text.length && queryIndex < query.length; textIndex += 1) {
    if (/\s/u.test(text[textIndex])) {
      queryIndex = 0;
      firstIndex = -1;
      lastIndex = -1;
      continue;
    }
    if (text[textIndex] !== query[queryIndex]) continue;
    if (firstIndex === -1) firstIndex = textIndex;
    lastIndex = textIndex;
    queryIndex += 1;
  }

  if (queryIndex !== query.length || firstIndex === -1) return null;
  return { tier: TIER_SUBSEQUENCE, offset: firstIndex, spread: lastIndex - firstIndex + 1 };
}

export function scoreMatch(
  query: string,
  text: string,
  options: MatchOptions = {},
): MatchScore | null {
  if (!query) return { tier: TIER_EXACT, offset: 0 };
  const q = query.toLowerCase();
  const t = text.toLowerCase();
  if (t === q) return { tier: TIER_EXACT, offset: 0 };

  const substring = scoreSubstringMatch(q, t);
  return substring ?? (options.subsequence === false ? null : scoreSubsequenceMatch(q, t));
}

export function compareMatchScores(a: MatchScore, b: MatchScore): number {
  if (a.tier !== b.tier) return a.tier - b.tier;
  if (a.offset !== b.offset) return a.offset - b.offset;
  return (a.spread ?? 0) - (b.spread ?? 0);
}

export function tokenizeQuery(query: string): string[] {
  return query
    .trim()
    .toLowerCase()
    .split(/\s+/)
    .filter((token) => token.length > 0);
}

export interface TextFieldsOptions {
  /**
   * See `MatchOptions.subsequence`. Applied per token, so turning it off means
   * every token has to appear as a run of adjacent characters in some field —
   * `lab des` still matches "Label as Design", `labdes` no longer does.
   */
  subsequence?: boolean;
}

export function scoreTextFields(
  query: string,
  fields: string[],
  options: TextFieldsOptions = {},
): MatchScore | null {
  const tokens = tokenizeQuery(query);
  if (tokens.length === 0) return { tier: TIER_EXACT, offset: 0, spread: 0 };

  const aggregate: MatchScore = { tier: TIER_EXACT, offset: 0, spread: 0 };
  for (const token of tokens) {
    let best: MatchScore | null = null;
    for (const field of fields) {
      const score = scoreMatch(token, field, { subsequence: options.subsequence });
      if (score && (!best || compareMatchScores(score, best) < 0)) {
        best = score;
      }
    }
    if (!best) return null;
    aggregate.tier += best.tier;
    aggregate.offset += best.offset;
    aggregate.spread = (aggregate.spread ?? 0) + (best.spread ?? token.length);
  }
  return aggregate;
}

import { buildFindPattern, findMatches } from "@/components/timeline/chat-find-text";

/**
 * Chat Find's DOM half: locating and highlighting matches in the already-
 * rendered transcript. `data-chat-find-text="true"` marks every container
 * whose text is searchable (`timeline-row.tsx`'s user/thinking bodies,
 * `timeline-markdown.tsx`'s rendered markdown, `tool-call-card.tsx`'s name
 * and summary) -- Paseo's `ranges.web.ts` walks similarly marked
 * Markdown-block containers.
 *
 * Highlighting wraps each match in a real `<mark>` element rather than
 * using the CSS Custom Highlight API Paseo's web build uses
 * (`CSS.highlights`/`Highlight`): that API isn't implemented in jsdom, and
 * a `<mark>` is exactly as "highlight in place" as a non-invasive overlay
 * for this codebase's component tests to assert against, at the cost of
 * needing to unwrap it again before the next search or on close (see
 * {@link clearChatHighlights}).
 */

const SEARCHABLE_SELECTOR = '[data-chat-find-text="true"]';
const MATCH_ATTR = "data-chat-find-match";
const MATCH_CLASS = "rounded-sm bg-status-warning/35";
const ACTIVE_CLASS = "rounded-sm bg-status-warning text-surface-0";

interface DomMatch {
  node: Text;
  start: number;
  end: number;
}

/**
 * Every match of `query` under `root`, in document order. Each match is
 * entirely inside one Text node -- a query can't span two nodes (e.g.
 * across a bold/plain boundary) -- which is what keeps
 * {@link applyChatHighlights} safe to implement with `Range.surroundContents`
 * (it throws on a range that only partially selects a non-Text node).
 */
export function findChatMatches(root: HTMLElement | null, query: string): DomMatch[] {
  const pattern = buildFindPattern(query);
  if (!root || !pattern) return [];

  const matches: DomMatch[] = [];
  for (const container of root.querySelectorAll<HTMLElement>(SEARCHABLE_SELECTOR)) {
    const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
    let node: Node | null;
    while ((node = walker.nextNode())) {
      if (!(node instanceof Text)) continue;
      for (const match of findMatches(node.data, pattern)) {
        matches.push({ node, start: match.start, end: match.end });
      }
    }
  }
  return matches;
}

/**
 * Wraps every match in a `<mark>`, styling `activeIndex`'s distinctly, and
 * returns the marks in the same order as `matches` so a caller can restyle
 * or scroll to one by index without re-walking the DOM.
 *
 * Multiple matches inside the *same* Text node are wrapped back-to-front
 * (highest `start` first): `surroundContents` mutates the node it wraps
 * into (splitting it and inserting the `<mark>` between the pieces), which
 * would invalidate every other match's offset into that same node computed
 * before any mutation happened -- processing right-to-left means a wrap
 * never disturbs the character offsets of a match still to come.
 */
export function applyChatHighlights(matches: DomMatch[], activeIndex: number): HTMLElement[] {
  const marks: HTMLElement[] = new Array(matches.length);

  const indicesByNode = new Map<Text, number[]>();
  matches.forEach((match, index) => {
    const indices = indicesByNode.get(match.node) ?? [];
    indices.push(index);
    indicesByNode.set(match.node, indices);
  });

  for (const indices of indicesByNode.values()) {
    indices.sort((a, b) => matches[b]!.start - matches[a]!.start);
    for (const index of indices) {
      const match = matches[index]!;
      const range = document.createRange();
      range.setStart(match.node, match.start);
      range.setEnd(match.node, match.end);
      const mark = document.createElement("mark");
      mark.setAttribute(MATCH_ATTR, "true");
      mark.className = index === activeIndex ? ACTIVE_CLASS : MATCH_CLASS;
      range.surroundContents(mark);
      marks[index] = mark;
    }
  }
  return marks;
}

/** Re-styles already-applied marks for a new active index, without re-walking or re-wrapping anything. */
export function restyleChatHighlights(marks: readonly HTMLElement[], activeIndex: number): void {
  marks.forEach((mark, index) => {
    mark.className = index === activeIndex ? ACTIVE_CLASS : MATCH_CLASS;
  });
}

/** Unwraps every `<mark>` {@link applyChatHighlights} inserted under `root`, merging text nodes back to their pre-search shape. */
export function clearChatHighlights(root: HTMLElement | null): void {
  if (!root) return;
  for (const mark of Array.from(root.querySelectorAll(`mark[${MATCH_ATTR}]`))) {
    const parent = mark.parentNode;
    if (!parent) continue;
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark);
    parent.removeChild(mark);
    parent.normalize();
  }
}

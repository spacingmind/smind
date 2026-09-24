import { buildFindPattern, findMatches } from "@/components/timeline/chat-find-text";

/**
 * Chat Find's DOM half: locating matches in the already-rendered
 * transcript and painting them via the CSS Custom Highlight API
 * (`CSS.highlights`/`Highlight`, styled in `index.css`) -- the same
 * technique Paseo's web build uses
 * (`agent-stream/chat-find/viewport.web.ts`).
 *
 * A `Highlight` is a pure paint overlay registered against live `Range`
 * objects; registering one never mutates the DOM. That matters here
 * specifically because this module used to highlight by wrapping each
 * match in a `<mark>` via `Range.surroundContents` -- which splits and
 * truncates the very Text nodes React's own fibers reference for the
 * transcript it renders. React was found to hit a detached or
 * wrong-shaped node the next time it updated or removed that text (a
 * streamed chunk landing while Find was open), either silently keeping
 * stale text or throwing "Failed to execute 'removeChild' on 'Node': The
 * node to be removed is not a child of this node." (see
 * `chat-find-react-safety.test.tsx`, the regression coverage for exactly
 * this). The Highlight API sidesteps the whole bug class by never
 * touching the DOM tree at all.
 *
 * Feature-detected: jsdom doesn't implement `CSS.highlights`/`Highlight`,
 * so this file's own tests stub both (see `chat-find-dom.test.ts`) to
 * exercise the real paint path against a fake registry. A real browser
 * without the API degrades to counting matches and scrolling the active
 * one into view with nothing painted, rather than falling back to any
 * DOM-mutating technique.
 */

const HIGHLIGHT_ALL = "smind-chat-find";
const HIGHLIGHT_ACTIVE = "smind-chat-find-active";
const SEARCHABLE_SELECTOR = '[data-chat-find-text="true"]';

interface DomMatch {
  node: Text;
  start: number;
  end: number;
}

/**
 * Every match of `query` under `root`, in document order. Each match is
 * scoped to a single Text node -- a query can't span two nodes (e.g.
 * across a bold/plain boundary) -- a deliberate scope trim (not, unlike
 * before, a technical requirement of how highlighting is applied: a
 * `Range` can freely span multiple nodes).
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

/** True when this runtime implements the CSS Custom Highlight API. */
function highlightApiAvailable(): boolean {
  return typeof Highlight !== "undefined" && typeof CSS !== "undefined" && Boolean(CSS.highlights);
}

function toRange(match: DomMatch): Range {
  const range = document.createRange();
  range.setStart(match.node, match.start);
  range.setEnd(match.node, match.end);
  return range;
}

function paint(ranges: readonly Range[], activeIndex: number): void {
  if (!highlightApiAvailable()) return;
  CSS.highlights.set(HIGHLIGHT_ALL, new Highlight(...ranges));
  const active = ranges[activeIndex];
  if (active) CSS.highlights.set(HIGHLIGHT_ACTIVE, new Highlight(active));
  else CSS.highlights.delete(HIGHLIGHT_ACTIVE);
}

/**
 * Builds a live Range for every match and paints them, styling
 * `activeIndex`'s distinctly. Returns the ranges in the same order as
 * `matches` so a caller can restyle or scroll to one by index without
 * re-walking the DOM. A no-op paint (ranges are still returned) when the
 * Highlight API isn't available.
 */
export function applyChatHighlights(matches: DomMatch[], activeIndex: number): Range[] {
  const ranges = matches.map(toRange);
  paint(ranges, activeIndex);
  return ranges;
}

/** Re-paints already-built ranges for a new active index, without re-walking or rebuilding anything. */
export function restyleChatHighlights(ranges: readonly Range[], activeIndex: number): void {
  paint(ranges, activeIndex);
}

/** Un-registers both highlights. A no-op if the API isn't available -- nothing was ever painted. */
export function clearChatHighlights(): void {
  if (!highlightApiAvailable()) return;
  CSS.highlights.delete(HIGHLIGHT_ALL);
  CSS.highlights.delete(HIGHLIGHT_ACTIVE);
}

/**
 * Scrolls a match's rendered position into view. Independent of whether
 * anything is actually painted, so the degraded no-Highlight-API path
 * still reveals the active match -- counting and navigation don't depend
 * on the paint succeeding.
 */
export function scrollChatMatchIntoView(range: Range | undefined): void {
  if (!range) return;
  const { startContainer } = range;
  const element = startContainer instanceof Element ? startContainer : startContainer.parentElement;
  element?.scrollIntoView({ block: "center" });
}

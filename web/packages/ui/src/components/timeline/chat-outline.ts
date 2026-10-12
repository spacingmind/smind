/** Distance below the viewport top at which a run counts as "being read". */
export const ACTIVE_ANCHOR_PX = 24;

const PREVIEW_MAX_CHARS = 120;
/** Shown for a prompt with no text (attachment-only), so a tick never has a blank label. */
export const EMPTY_PROMPT_PREVIEW = "(no text)";

export interface OutlinePrompt {
  id: string;
  preview: string;
}

/** First non-empty line of the prompt, trimmed and truncated for the tick preview. */
export function promptPreview(prompt: string): string {
  const line = prompt.split(/\r?\n/).find((candidate) => candidate.trim() !== "") ?? "";
  const trimmed = line.trim();
  if (trimmed === "") return EMPTY_PROMPT_PREVIEW;
  if (trimmed.length <= PREVIEW_MAX_CHARS) return trimmed;
  return `${trimmed.slice(0, PREVIEW_MAX_CHARS - 1).trimEnd()}…`;
}

export function buildOutlinePrompts(
  runs: readonly { id: string; prompt: string }[],
): OutlinePrompt[] {
  return runs.map((run) => ({ id: run.id, preview: promptPreview(run.prompt) }));
}

/**
 * Streaming creates a new `runs` array on every chunk; the outline only cares about ids and
 * previews, so hand back the previous array while those are unchanged.
 */
export function stabilizeOutlinePrompts(
  previous: readonly OutlinePrompt[] | null,
  next: OutlinePrompt[],
): OutlinePrompt[] {
  if (
    previous &&
    previous.length === next.length &&
    previous.every((prompt, i) => prompt.id === next[i].id && prompt.preview === next[i].preview)
  ) {
    return previous as OutlinePrompt[];
  }
  return next;
}

/**
 * The run the reader is inside: the last run whose top (relative to the scroll container's
 * content) is at or above the anchor; the last run when scrolled to the bottom; the first
 * run above the first one. `tops` must be ascending. Returns -1 when there are no runs.
 */
export function resolveActiveRunIndex(
  tops: readonly number[],
  anchor: number,
  atBottom = false,
): number {
  if (tops.length === 0) return -1;
  if (atBottom) return tops.length - 1;
  let low = 0;
  let high = tops.length - 1;
  let found = 0;
  while (low <= high) {
    const mid = (low + high) >> 1;
    if (tops[mid] <= anchor) {
      found = mid;
      low = mid + 1;
    } else {
      high = mid - 1;
    }
  }
  return found;
}

/**
 * Slots further than this from the pointer keep their resting size, so a long rail
 * magnifies a local band instead of swelling the whole column.
 */
export const OUTLINE_MAGNIFY_RADIUS = 3;

/** Dock-style raised-cosine falloff: 1 under the pointer, 0 at the radius. */
export function promptTickMagnification(slotDistance: number): number {
  const distance = Math.abs(slotDistance);
  if (!Number.isFinite(distance) || distance >= OUTLINE_MAGNIFY_RADIUS) return 0;
  return (1 + Math.cos((Math.PI * distance) / OUTLINE_MAGNIFY_RADIUS)) / 2;
}

export interface ActivePromptSource {
  subscribe: (listener: () => void) => () => void;
  getActiveId: () => string | null;
}

export interface ActivePromptPublisher extends ActivePromptSource {
  publish: (id: string | null) => void;
}

/**
 * The scroll handler reports the reading position every frame, far more often than React
 * should render. Keeping the active run outside React lets only the rail subscribe.
 */
export function createActivePromptPublisher(): ActivePromptPublisher {
  const listeners = new Set<() => void>();
  let activeId: string | null = null;
  return {
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    getActiveId: () => activeId,
    publish(id) {
      if (id === activeId) return;
      activeId = id;
      for (const listener of listeners) listener();
    },
  };
}

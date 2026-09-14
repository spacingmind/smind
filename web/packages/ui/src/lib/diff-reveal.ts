/**
 * A one-shot "show me this path in the diff" signal, from the explorer
 * tree's context menu (ui-redesign-parity plan, Item 17's "reveal in
 * diff") to DiffViewerPane.
 *
 * Same reasoning as lib/dirty-buffers.ts for why this is a module-level
 * bus and not React state: the explorer and the diff pane are sibling tab
 * contents, and the only thing they need to agree on is a (taskId, path)
 * pair. App.tsx's part is just activating the diff tab -- it doesn't have
 * to carry the payload.
 *
 * The request is *latched*, not merely broadcast: the diff pane is
 * usually unmounted at the moment the explorer asks (its tab hasn't been
 * activated yet), so a live-only event would be missed every time. The
 * pane reads the pending request on mount via takeDiffReveal, which
 * consumes it -- so re-mounting the tab later doesn't re-scroll to a file
 * the user has moved on from.
 */
export interface DiffRevealRequest {
  taskId: number;
  path: string;
}

let pending: DiffRevealRequest | null = null;
const listeners = new Set<(request: DiffRevealRequest) => void>();

export function requestDiffReveal(taskId: number, path: string): void {
  pending = { taskId, path };
  for (const listener of listeners) listener(pending);
}

/** Returns and clears the pending request if it's for taskId, else null (a request aimed at a different task is left alone, not swallowed). */
export function takeDiffReveal(taskId: number): DiffRevealRequest | null {
  if (!pending || pending.taskId !== taskId) return null;
  const request = pending;
  pending = null;
  return request;
}

/** Subscribes to requests raised while already mounted (the diff tab is open and the user reveals from a split/side explorer). */
export function subscribeDiffReveal(listener: (request: DiffRevealRequest) => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Test-only reset -- a latched request from one test must not leak into the next. */
export function resetDiffReveal(): void {
  pending = null;
}

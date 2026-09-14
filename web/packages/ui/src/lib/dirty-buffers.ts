import { useSyncExternalStore } from "react";

/**
 * Which open file tabs have unsaved edits, keyed by tab key
 * (`${taskId}:file:${path}` -- see tab-registry.tsx's fileTabKey).
 *
 * This is a module-level store rather than React state because the two
 * ends live in different subtrees: FileEditorPane (mounted inside one
 * tab's content) is what knows a buffer is dirty, and the tab *strip*
 * (App.tsx) is what has to show the marker -- and the strip is a sibling
 * of the content, not an ancestor of it. Lifting the state into App.tsx
 * would mean threading a setter down through the tab renderer into every
 * pane; a context provider would mean App.tsx owning a provider Track C
 * doesn't own. A store keyed by the tab key that both ends already have
 * keeps the coupling to exactly the key string (ui-redesign-parity plan,
 * Item 17: "a dirty indicator on the file tab itself, not only inside the
 * editor").
 *
 * Entries are removed, never set false, when a tab's editor unmounts, so
 * the store can't grow without bound across a long session.
 */
const dirtyKeys = new Set<string>();
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
}

/** Records (or clears) the unsaved-edits flag for one file tab. A no-op when the flag already matches, so an editor may call it on every render. */
export function setBufferDirty(key: string, dirty: boolean): void {
  if (dirty === dirtyKeys.has(key)) return;
  if (dirty) dirtyKeys.add(key);
  else dirtyKeys.delete(key);
  emit();
}

/** Drops a tab's entry entirely -- called when its editor unmounts (tab closed, task switched). */
export function forgetBuffer(key: string): void {
  if (dirtyKeys.delete(key)) emit();
}

export function isBufferDirty(key: string): boolean {
  return dirtyKeys.has(key);
}

/** Test-only reset, so one test's leftover dirty tab can't leak into the next. */
export function resetDirtyBuffers(): void {
  if (dirtyKeys.size === 0) return;
  dirtyKeys.clear();
  emit();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/**
 * Subscribes one tab's marker to the store. The snapshot is a boolean --
 * a primitive, so useSyncExternalStore's identity check is exactly the
 * right comparison and a change to some *other* tab's flag re-renders
 * nothing here.
 */
export function useBufferDirty(key: string): boolean {
  return useSyncExternalStore(
    subscribe,
    () => dirtyKeys.has(key),
    () => false,
  );
}

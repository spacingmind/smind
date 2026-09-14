import { useSyncExternalStore } from "react";

/**
 * What a terminal *tab* knows about its terminal *session*
 * (ui-redesign-parity plan, Item 20): which server-side session it owns,
 * and whether that session has produced output the user hasn't looked at.
 *
 * Both are keyed by tab key (`${taskId}:terminal`, `${taskId}:terminal:2`,
 * ...), and both live outside React for the same reason
 * lib/dirty-buffers.ts does: the tab strip that renders the activity dot
 * is a sibling of the pane that knows about the data, not an ancestor.
 *
 * The binding is deliberately **not** persisted to localStorage. A
 * reload's sessions may or may not have survived on the daemon side, and
 * a stale binding would put a freshly-loaded page straight into the
 * "session closed" state instead of giving it a working shell; within one
 * page load, though, it's what lets a second terminal tab avoid stealing
 * the first one's session.
 */
const bindings = new Map<string, string>();
const activeTabs = new Set<string>();
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
}

/** The session this tab owns, or null if it hasn't claimed one yet. */
export function boundTerminalId(tabKey: string): string | null {
  return bindings.get(tabKey) ?? null;
}

export function bindTerminal(tabKey: string, terminalId: string): void {
  if (bindings.get(tabKey) === terminalId) return;
  bindings.set(tabKey, terminalId);
  emit();
}

/** Forgets a tab's session -- what an explicit "Close terminal" does, so the next mount creates a fresh one rather than getting stuck on a closed id. */
export function unbindTerminal(tabKey: string): void {
  if (bindings.delete(tabKey)) emit();
}

/**
 * Session ids already owned by *other* tabs. A tab with no binding yet is
 * allowed to adopt an already-running session (that's what makes
 * reconnect resync work without spawning a duplicate shell -- see
 * terminal-pane.tsx), but it must never adopt one another tab is driving,
 * or two terminal tabs would render the same shell.
 */
export function terminalIdsBoundElsewhere(exceptTabKey: string): Set<string> {
  const out = new Set<string>();
  for (const [key, id] of bindings) {
    if (key !== exceptTabKey) out.add(id);
  }
  return out;
}

export function markTerminalActivity(tabKey: string): void {
  if (activeTabs.has(tabKey)) return;
  activeTabs.add(tabKey);
  emit();
}

export function clearTerminalActivity(tabKey: string): void {
  if (activeTabs.delete(tabKey)) emit();
}

export function hasTerminalActivity(tabKey: string): boolean {
  return activeTabs.has(tabKey);
}

/** Test-only reset of both maps -- a binding or an activity flag left by one test would change the next one's session selection. */
export function resetTerminalSessions(): void {
  if (bindings.size === 0 && activeTabs.size === 0) return;
  bindings.clear();
  activeTabs.clear();
  emit();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

/** Subscribes one terminal tab's activity dot. The snapshot is a boolean, so an unrelated tab's change re-renders nothing here. */
export function useTerminalActivity(tabKey: string): boolean {
  return useSyncExternalStore(
    subscribe,
    () => activeTabs.has(tabKey),
    () => false,
  );
}

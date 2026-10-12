import { useSyncExternalStore } from "react";

/**
 * Open state of the plan-usage modal, as a module store so any surface (the
 * command palette's "Usage: open plan usage", the chat hover card's "Open
 * Usage" later, the sidebar footer item) can open it with `openUsageModal()`
 * without a prop chain through the shell. The modal itself is mounted once,
 * by `UsageModalRoot`, which reads this.
 */
let open = false;
const listeners = new Set<() => void>();

function set(next: boolean): void {
  if (next === open) return;
  open = next;
  for (const l of listeners) l();
}

export function openUsageModal(): void {
  set(true);
}

export function closeUsageModal(): void {
  set(false);
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useUsageModalOpen(): boolean {
  return useSyncExternalStore(subscribe, () => open);
}

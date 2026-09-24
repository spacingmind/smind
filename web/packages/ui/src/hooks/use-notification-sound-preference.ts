import { useCallback, useSyncExternalStore } from "react";

import { readNotificationSoundEnabled, writeNotificationSoundEnabled } from "@/lib/sidebar-preferences";

/**
 * Module-level store, same shape as use-notification-permission.ts's, and
 * for the same reason: this mounts both in the Notifications settings
 * section (where the toggle lives) and in AppSidebar (which reads it every
 * time useAttentionNotifications is about to fire) -- and AppSidebar stays
 * mounted for the app's whole lifetime (App.tsx never unmounts the
 * sidebar to show Settings), so a plain per-mount `useState(() =>
 * readNotificationSoundEnabled())` would never see a toggle flipped after
 * the sidebar's first mount. `current` re-syncs from storage whenever
 * there are no active subscribers, the same test-friendliness escape
 * hatch use-notification-permission.ts documents.
 */
let current: boolean | null = null;
const listeners = new Set<() => void>();

function getSnapshot(): boolean {
  if (listeners.size === 0 || current === null) current = readNotificationSoundEnabled();
  return current;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function setEnabled(next: boolean): void {
  if (next === current) return;
  current = next;
  writeNotificationSoundEnabled(next);
  for (const l of listeners) l();
}

/** The Notifications settings section's sound toggle (AC3), read live by useAttentionNotifications' caller. */
export function useNotificationSoundPreference(): {
  enabled: boolean;
  setEnabled: (enabled: boolean) => void;
} {
  const enabled = useSyncExternalStore(subscribe, getSnapshot);
  const set = useCallback((next: boolean) => setEnabled(next), []);
  return { enabled, setEnabled: set };
}

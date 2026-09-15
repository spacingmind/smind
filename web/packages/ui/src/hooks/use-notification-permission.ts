import { useCallback, useSyncExternalStore } from "react";

/** Notification.permission plus "unsupported" for a browser without the API at all -- kept distinct from "denied" so the UI can explain *why* notifications aren't available instead of implying the user said no. */
export type NotificationPermissionState = NotificationPermission | "unsupported";

function isSupported(): boolean {
  return typeof window !== "undefined" && "Notification" in window;
}

/**
 * Module-level store, not component state: this hook now mounts in two
 * places at once (the sidebar's gating read for
 * useAttentionNotifications, and the settings screen's General section,
 * which is where Item 13 moves the user-facing control to) and both must
 * observe the same "granted" transition the moment it happens -- a
 * settings-screen click must not leave the sidebar's copy stale until
 * some unrelated re-render happens to catch up. useSyncExternalStore is
 * what makes every mount subscribe to the one shared value instead of
 * each hook call tracking its own.
 *
 * `current` is re-synced from the live `Notification.permission` whenever
 * there are no active subscribers -- i.e. every consumer has unmounted.
 * A real app never hits that path after its first mount (something is
 * always subscribed), so this is invisible there; it's what lets a test
 * swap in a fresh `Notification` global between cases and have the very
 * next mount pick it up, the same way the old per-component `useState(()
 * => Notification.permission)` initializer did.
 */
let current: NotificationPermissionState | null = null;
const listeners = new Set<() => void>();

function liveSnapshot(): NotificationPermissionState {
  return isSupported() ? Notification.permission : "unsupported";
}

function setPermission(next: NotificationPermissionState): void {
  if (next === current) return;
  current = next;
  for (const l of listeners) l();
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot(): NotificationPermissionState {
  if (listeners.size === 0 || current === null) current = liveSnapshot();
  return current;
}

/**
 * Tracks the browser's Notification permission and exposes a
 * requestPermission callback meant to be wired to an explicit user action
 * (a button/toggle click) -- never called on mount or any other
 * unprompted path, per this project's own policy of not surprising users
 * with permission popups.
 *
 * Never re-prompts: if permission is already anything other than
 * "default" (i.e. the user already answered "granted" or "denied", in
 * this session or a previous one -- browsers persist the choice),
 * requestPermission just re-syncs state from Notification.permission
 * instead of calling the native API again, so repeated clicks (or a
 * denied user clicking again) never spam the browser's own prompt.
 * Notification.requestPermission() rejecting for any reason (some
 * browsers throw if it's not called from a user gesture) is caught and
 * treated as "denied" rather than left to crash the click handler.
 */
export function useNotificationPermission(): {
  permission: NotificationPermissionState;
  requestPermission: () => void;
} {
  const permission = useSyncExternalStore(subscribe, getSnapshot);

  const requestPermission = useCallback(() => {
    if (!isSupported()) return;
    // Gate on the shared tracked state, not a fresh Notification.permission
    // read: once this has been set to "granted"/"denied", that *is* the
    // decided value -- no need to ask the browser again, and no need to
    // trust that some external mutable global reflects it synchronously
    // (real browsers do; naive test doubles don't have to).
    if (current !== "default") return;
    Notification.requestPermission()
      .then((result) => setPermission(result))
      .catch(() => setPermission("denied"));
  }, []);

  return { permission, requestPermission };
}

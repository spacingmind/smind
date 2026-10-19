import { useCallback, useEffect } from "react";

import type { NotificationPermissionState } from "@/stores/notification-permission-store";

export type { NotificationPermissionState };
import { useNotificationPermissionStore } from "@/stores/notification-permission-store";

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
  const permission = useNotificationPermissionStore((s) => s.permission);

  useEffect(() => {
    useNotificationPermissionStore.getState().sync();
  }, []);

  const requestPermission = useCallback(() => {
    const store = useNotificationPermissionStore.getState();
    // Gate on the shared tracked state, not a fresh Notification.permission
    // read: once this has been set to "granted"/"denied", that *is* the
    // decided value -- no need to ask the browser again, and no need to
    // trust that some external mutable global reflects it synchronously
    // (real browsers do; naive test doubles don't have to).
    if (store.permission !== "default") return;
    if (typeof window === "undefined" || !("Notification" in window)) return;
    Notification.requestPermission()
      .then((result) => useNotificationPermissionStore.getState().setPermission(result))
      .catch(() => useNotificationPermissionStore.getState().setPermission("denied"));
  }, []);

  return { permission, requestPermission };
}

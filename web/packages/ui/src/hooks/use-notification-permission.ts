import { useCallback, useState } from "react";

/** Notification.permission plus "unsupported" for a browser without the API at all -- kept distinct from "denied" so the UI can explain *why* notifications aren't available instead of implying the user said no. */
export type NotificationPermissionState = NotificationPermission | "unsupported";

function isSupported(): boolean {
  return typeof window !== "undefined" && "Notification" in window;
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
  const [permission, setPermission] = useState<NotificationPermissionState>(() =>
    isSupported() ? Notification.permission : "unsupported",
  );

  const requestPermission = useCallback(() => {
    if (!isSupported()) return;
    // Gate on our own tracked state, not a fresh Notification.permission
    // read: once this hook has set state to "granted"/"denied", that *is*
    // the decided value -- no need to ask the browser again, and no need
    // to trust that some external mutable global reflects it synchronously
    // (real browsers do; naive test doubles don't have to).
    if (permission !== "default") return;
    Notification.requestPermission()
      .then((result) => setPermission(result))
      .catch(() => setPermission("denied"));
  }, [permission]);

  return { permission, requestPermission };
}

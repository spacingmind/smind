import { create } from "zustand";

/** Notification.permission plus "unsupported" for a browser without the API at all -- kept distinct from "denied" so the UI can explain *why* notifications aren't available instead of implying the user said no. */
export type NotificationPermissionState = NotificationPermission | "unsupported";

function liveSnapshot(): NotificationPermissionState {
  return typeof window !== "undefined" && "Notification" in window ? Notification.permission : "unsupported";
}

/**
 * The browser's notification permission as one shared copy (not persisted) for the sidebar's gating read
 * and the settings screen's control, replacing the old hand-rolled module store. `sync()` re-reads the
 * live `Notification.permission`; the hook calls it on every mount, which is what lets a test (or a fresh
 * mount) pick up a permission that changed while nobody was subscribed.
 */
export const useNotificationPermissionStore = create<{
  permission: NotificationPermissionState;
  setPermission: (permission: NotificationPermissionState) => void;
  sync: () => void;
}>()((set) => ({
  permission: liveSnapshot(),
  setPermission: (permission) => set({ permission }),
  sync: () => set({ permission: liveSnapshot() }),
}));

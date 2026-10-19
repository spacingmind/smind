import { create } from "zustand";

import { desktopWindow, type WindowState } from "@/lib/platform";

/**
 * One shared copy of the main window's maximized/fullscreen state (not persisted), read by the caption
 * buttons, the header inset and the sidebar's top strip. The subscription to the desktop window is
 * ref-counted by `initWindowStateSubscription`, exactly as the old hand-rolled module store did, so the
 * shared IPC listener lives only while some consumer is mounted.
 */
const DEFAULT_STATE: WindowState = { maximized: false, fullscreen: false };

export const useWindowStateStore = create<{ state: WindowState; setState: (state: WindowState) => void }>()(
  (set) => ({
    state: DEFAULT_STATE,
    setState: (state) => set({ state }),
  }),
);

let subscribers = 0;
let stop: (() => void) | null = null;

/** Starts the shared desktopWindow subscription on the first consumer and stops it after the last one leaves. */
export function initWindowStateSubscription(): () => void {
  subscribers += 1;
  if (!stop) {
    stop = desktopWindow.onStateChange((next) => {
      const { state } = useWindowStateStore.getState();
      if (next.maximized === state.maximized && next.fullscreen === state.fullscreen) return;
      useWindowStateStore.getState().setState(next);
    });
  }
  return () => {
    subscribers -= 1;
    if (subscribers === 0) {
      stop?.();
      stop = null;
    }
  };
}

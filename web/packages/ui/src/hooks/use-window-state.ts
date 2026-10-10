import { useSyncExternalStore } from "react";

import { desktopWindow, type WindowState } from "@/lib/platform";

/**
 * One shared subscription to the main window's maximized/fullscreen state,
 * ref-counted across consumers (the caption buttons, the header inset and
 * the sidebar's top strip all read it) so a drag-resize costs one IPC read
 * per frame no matter how many components care. Inert in a browser build:
 * the platform stub's `onStateChange` never fires.
 */
const DEFAULT_STATE: WindowState = { maximized: false, fullscreen: false };

let current: WindowState = DEFAULT_STATE;
let stop: (() => void) | null = null;
const listeners = new Set<() => void>();

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  if (!stop) {
    stop = desktopWindow.onStateChange((next) => {
      if (next.maximized === current.maximized && next.fullscreen === current.fullscreen) return;
      current = next;
      listeners.forEach((l) => l());
    });
  }
  return () => {
    listeners.delete(listener);
    if (listeners.size === 0) {
      stop?.();
      stop = null;
    }
  };
}

export function useWindowState(): WindowState {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => DEFAULT_STATE,
  );
}

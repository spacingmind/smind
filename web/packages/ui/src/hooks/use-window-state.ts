import { useEffect } from "react";

import type { WindowState } from "@/lib/platform";
import { initWindowStateSubscription, useWindowStateStore } from "@/stores/window-state-store";

/**
 * One shared subscription to the main window's maximized/fullscreen state
 * (the caption buttons, the header inset and the sidebar's top strip all
 * read it) so a drag-resize costs one IPC read per frame no matter how many
 * components care. Inert in a browser build: the platform stub's
 * `onStateChange` never fires.
 */
export function useWindowState(): WindowState {
  const state = useWindowStateStore((s) => s.state);
  useEffect(() => initWindowStateSubscription(), []);
  return state;
}

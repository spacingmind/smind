import { useCallback } from "react";

import { SIDEBAR_MAX_WIDTH, SIDEBAR_MIN_WIDTH, useSidebarWidthStore } from "@/stores/sidebar-width-store";

export {
  SIDEBAR_DEFAULT_WIDTH,
  SIDEBAR_ICON_WIDTH,
  SIDEBAR_MAX_WIDTH,
  SIDEBAR_MIN_WIDTH,
} from "@/stores/sidebar-width-store";

/**
 * The sidebar's user-resized width in pixels, persisted across reloads
 * (`lib/storage.ts` -- the plan's Item 3 "one storage mechanism" criterion)
 * -- a fresh mount (a real reload, or a test simulating one by mounting a
 * second time against the same localStorage) reads back whatever was last
 * set, defaulting to SIDEBAR_DEFAULT_WIDTH the first time ever.
 *
 * This is the committed state the sidebar drag handle updates on release
 * (hooks/use-sidebar-resize.ts, wired up in App.tsx) -- clamping and
 * persistence both happen in the store, in one place, whatever the drag
 * math hands back.
 */
export function useSidebarWidth(): [number, (next: number) => void] {
  const width = useSidebarWidthStore((s) => s.value);
  const setWidth = useCallback((next: number) => {
    const clamped = Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, next));
    const { value, set } = useSidebarWidthStore.getState();
    if (value === clamped) return;
    set(clamped);
  }, []);
  return [width, setWidth];
}

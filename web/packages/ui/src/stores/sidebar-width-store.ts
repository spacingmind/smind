import { createPreferenceStore } from "@/stores/create-preference-store";
import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";

/** Sane drag bounds for the sidebar, in pixels -- 12rem/32rem at the usual 16px/rem. */
export const SIDEBAR_MIN_WIDTH = 192;
export const SIDEBAR_MAX_WIDTH = 512;
/** Matches the prior fixed SIDEBAR_WIDTH ("16rem") in components/ui/sidebar.tsx, now just the initial value instead of a constant. */
export const SIDEBAR_DEFAULT_WIDTH = 256;
/** Matches `SIDEBAR_WIDTH_ICON` ("3rem") in components/ui/sidebar.tsx -- the icon-collapsed rail's width, at the usual 16px/rem. The sidebar panel collapses to exactly this many pixels rather than leaving its last dragged width as dead space beside the icon rail. */
export const SIDEBAR_ICON_WIDTH = 48;

/** Clamps to [SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH] -- dragging past either bound settles at the bound, never collapses to 0 or grows off-screen. */
function clamp(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, width));
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

/**
 * The sidebar's user-resized width in pixels as one shared copy, persisted across reloads via the same
 * storage key and shape (`lib/storage.ts`, the plan's Item 3 "one storage mechanism" criterion).
 */
export const useSidebarWidthStore = createPreferenceStore<number>(
  () => clamp(readStored(STORAGE_KEYS.sidebarWidth, SIDEBAR_DEFAULT_WIDTH, isFiniteNumber)),
  (width) => writeStored(STORAGE_KEYS.sidebarWidth, clamp(width)),
);

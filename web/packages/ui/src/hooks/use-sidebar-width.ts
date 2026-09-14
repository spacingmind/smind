import { useCallback, useState } from "react";

import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";

/** Sane drag bounds for the sidebar, in pixels -- 12rem/32rem at the usual 16px/rem. */
export const SIDEBAR_MIN_WIDTH = 192;
export const SIDEBAR_MAX_WIDTH = 512;
/** Matches the prior fixed SIDEBAR_WIDTH ("16rem") in components/ui/sidebar.tsx, now just the initial value instead of a constant. */
export const SIDEBAR_DEFAULT_WIDTH = 256;

/** Clamps to [SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH] -- dragging past either bound settles at the bound, never collapses to 0 or grows off-screen. */
function clamp(width: number): number {
  return Math.min(SIDEBAR_MAX_WIDTH, Math.max(SIDEBAR_MIN_WIDTH, width));
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function readPersistedWidth(): number {
  const raw = readStored(STORAGE_KEYS.sidebarWidth, SIDEBAR_DEFAULT_WIDTH, isFiniteNumber);
  return clamp(raw);
}

/**
 * The sidebar's user-resized width in pixels, persisted across reloads
 * (`lib/storage.ts` -- the plan's Item 3 "one storage mechanism" criterion)
 * -- a fresh mount (a real reload, or a test simulating one by mounting a
 * second time against the same localStorage) reads back whatever was last
 * set, defaulting to SIDEBAR_DEFAULT_WIDTH the first time ever.
 *
 * This is the state the drag handle (react-resizable-panels' own
 * ResizableHandle, wired up in App.tsx) ultimately updates via its
 * onResize callback -- clamping and persistence both happen here, in one
 * place, regardless of what the underlying drag math (percentage of the
 * panel group's measured width) hands back.
 */
export function useSidebarWidth(): [number, (next: number) => void] {
  const [width, setWidthState] = useState<number>(() => readPersistedWidth());

  const setWidth = useCallback((next: number) => {
    const clamped = clamp(next);
    setWidthState((prev) => (prev === clamped ? prev : clamped));
    writeStored(STORAGE_KEYS.sidebarWidth, clamped);
  }, []);

  return [width, setWidth];
}

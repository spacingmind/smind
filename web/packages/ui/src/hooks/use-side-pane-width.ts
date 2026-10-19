import { useCallback } from "react";

import { useSidePaneWidthStore } from "@/stores/side-pane-width-store";

/** Sane drag bounds for the side pane, in pixels -- narrower floor than the sidebar's, since it's splitting an already-narrower content area. */
export const SIDE_PANE_MIN_WIDTH = 280;
export const SIDE_PANE_MAX_WIDTH = 800;
export const SIDE_PANE_DEFAULT_WIDTH = 420;

function clamp(width: number): number {
  return Math.min(SIDE_PANE_MAX_WIDTH, Math.max(SIDE_PANE_MIN_WIDTH, width));
}

/**
 * The side pane's width in pixels, **persisted per task** (Item 6's
 * criterion) rather than as one global preference -- a task with a wide
 * diff and a task with a narrow file both keep the split they left, the
 * same way each task keeps its own open tabs.
 */
export function useSidePaneWidth(taskId: number | null): [number, (next: number) => void] {
  const widths = useSidePaneWidthStore((s) => s.value);

  const width = taskId === null ? SIDE_PANE_DEFAULT_WIDTH : clamp(widths[String(taskId)] ?? SIDE_PANE_DEFAULT_WIDTH);

  const setWidth = useCallback(
    (next: number) => {
      if (taskId === null) return;
      const clamped = clamp(next);
      const { value: prev, set } = useSidePaneWidthStore.getState();
      if (prev[String(taskId)] === clamped) return;
      set({ ...prev, [String(taskId)]: clamped });
    },
    [taskId],
  );

  return [width, setWidth];
}

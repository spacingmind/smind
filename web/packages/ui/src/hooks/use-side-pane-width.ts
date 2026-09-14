import { useCallback, useState } from "react";

import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";

/** Sane drag bounds for the side pane, in pixels -- narrower floor than the sidebar's, since it's splitting an already-narrower content area. */
export const SIDE_PANE_MIN_WIDTH = 280;
export const SIDE_PANE_MAX_WIDTH = 800;
export const SIDE_PANE_DEFAULT_WIDTH = 420;

function clamp(width: number): number {
  return Math.min(SIDE_PANE_MAX_WIDTH, Math.max(SIDE_PANE_MIN_WIDTH, width));
}

type PersistedWidths = Record<string, number>;

function isPersistedWidths(value: unknown): value is PersistedWidths {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  return Object.values(value).every((v) => typeof v === "number" && Number.isFinite(v));
}

/**
 * The side pane's width in pixels, **persisted per task** (Item 6's
 * criterion) rather than as one global preference -- a task with a wide
 * diff and a task with a narrow file both keep the split they left, the
 * same way each task keeps its own open tabs.
 */
export function useSidePaneWidth(taskId: number | null): [number, (next: number) => void] {
  const [widths, setWidths] = useState<PersistedWidths>(() => readStored(STORAGE_KEYS.sidePaneWidth, {}, isPersistedWidths));

  const width = taskId === null ? SIDE_PANE_DEFAULT_WIDTH : clamp(widths[String(taskId)] ?? SIDE_PANE_DEFAULT_WIDTH);

  const setWidth = useCallback(
    (next: number) => {
      if (taskId === null) return;
      const clamped = clamp(next);
      setWidths((prev) => {
        if (prev[String(taskId)] === clamped) return prev;
        const updated = { ...prev, [String(taskId)]: clamped };
        writeStored(STORAGE_KEYS.sidePaneWidth, updated);
        return updated;
      });
    },
    [taskId],
  );

  return [width, setWidth];
}

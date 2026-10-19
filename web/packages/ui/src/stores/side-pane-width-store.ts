import { createPreferenceStore } from "@/stores/create-preference-store";
import { readStored, STORAGE_KEYS, writeStored } from "@/lib/storage";

type PersistedWidths = Record<string, number>;

function isPersistedWidths(value: unknown): value is PersistedWidths {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  return Object.values(value).every((v) => typeof v === "number" && Number.isFinite(v));
}

/**
 * The side pane's per-task widths (`Record<taskId, width>`) as one shared copy, persisted under the same
 * storage key and shape -- a task with a wide diff and a task with a narrow file both keep the split they
 * left, the same way each task keeps its own open tabs.
 */
export const useSidePaneWidthStore = createPreferenceStore<PersistedWidths>(
  () => readStored(STORAGE_KEYS.sidePaneWidth, {}, isPersistedWidths),
  (widths) => writeStored(STORAGE_KEYS.sidePaneWidth, widths),
);

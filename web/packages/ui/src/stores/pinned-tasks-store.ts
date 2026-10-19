import { createPreferenceStore } from "@/stores/create-preference-store";
import { readPinnedTasks, writePinnedTasks } from "@/lib/sidebar-preferences";

/**
 * The sidebar's pinned-task id set as one shared copy (AC4), persisted under the same storage key and
 * shape (lib/sidebar-preferences.ts).
 */
export const usePinnedTasksStore = createPreferenceStore<Set<number>>(readPinnedTasks, writePinnedTasks);

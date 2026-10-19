import { createPreferenceStore } from "@/stores/create-preference-store";
import { readCollapsedTasks, writeCollapsedTasks } from "@/lib/sidebar-preferences";

/**
 * The sidebar tree view's collapsed-parent task id set (PR-U1) as one shared copy, persisted under the same
 * storage key and shape (lib/sidebar-preferences.ts).
 */
export const useCollapsedTasksStore = createPreferenceStore<Set<number>>(readCollapsedTasks, writeCollapsedTasks);

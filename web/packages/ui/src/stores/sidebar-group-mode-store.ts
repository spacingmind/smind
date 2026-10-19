import { createPreferenceStore } from "@/stores/create-preference-store";
import { readSidebarGroupMode, writeSidebarGroupMode } from "@/lib/sidebar-preferences";

/**
 * The sidebar's tree-vs-status view toggle (AC6), as one shared copy instead of a per-mount `useState`
 * so every reader sees the mode change live.
 */
export const useSidebarGroupModeStore = createPreferenceStore(readSidebarGroupMode, writeSidebarGroupMode);

import { createPreferenceStore } from "@/stores/create-preference-store";
import { readSidebarAutomationFilter, writeSidebarAutomationFilter } from "@/lib/sidebar-preferences";

/** The sidebar's "Started by automation" filter, one shared copy so every reader sees the toggle live. */
export const useSidebarAutomationFilterStore = createPreferenceStore(readSidebarAutomationFilter, writeSidebarAutomationFilter);

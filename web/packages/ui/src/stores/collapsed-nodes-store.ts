import { createPreferenceStore } from "@/stores/create-preference-store";
import { readCollapsedNodes, writeCollapsedNodes } from "@/lib/sidebar-preferences";

/**
 * The sidebar's persisted expand/collapse container ids (sidebar-polish S8) as one shared copy, persisted
 * under the same storage key and shape (lib/sidebar-preferences.ts).
 */
export const useCollapsedNodesStore = createPreferenceStore<Set<string>>(readCollapsedNodes, writeCollapsedNodes);

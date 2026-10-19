import { createPreferenceStore } from "@/stores/create-preference-store";
import { readStoredDefaultAgentId, writeStoredDefaultAgentId } from "@/lib/settings-preferences";

/**
 * The run-config IA plan's ★ default agent: Settings -> Agents sets it and every surface that seeds a task's
 * run config reads it. A shared store, so Settings and the toolbar can no longer disagree about it.
 * (A toolbar that has already seeded keeps its own state: it reads `getState()` when it seeds, it does not
 * subscribe.)
 */
export const useDefaultAgentStore = createPreferenceStore<string | null>(readStoredDefaultAgentId, writeStoredDefaultAgentId);

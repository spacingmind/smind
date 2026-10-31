import { createPreferenceStore } from "@/stores/create-preference-store";

const HOME_PROJECT_STORAGE_KEY = "smind:home-project";

function readHomeProject(): number | null {
  if (typeof window === "undefined") return null;
  try {
    const id = Number.parseInt(window.localStorage.getItem(HOME_PROJECT_STORAGE_KEY) ?? "", 10);
    return Number.isFinite(id) ? id : null;
  } catch {
    return null;
  }
}

function writeHomeProject(id: number | null): void {
  if (typeof window === "undefined") return;
  try {
    if (id === null) window.localStorage.removeItem(HOME_PROJECT_STORAGE_KEY);
    else window.localStorage.setItem(HOME_PROJECT_STORAGE_KEY, String(id));
  } catch {
    // Best-effort only.
  }
}

/** The project the home landing composer targets: the last one the user picked there. */
export const useHomeProjectStore = createPreferenceStore<number | null>(readHomeProject, writeHomeProject);

import { createPreferenceStore } from "@/stores/create-preference-store";

const GET_STARTED_STORAGE_KEY = "smind:get-started-dismissed";

function readDismissed(): boolean {
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(GET_STARTED_STORAGE_KEY) === "1";
  } catch {
    return false;
  }
}

function writeDismissed(value: boolean): void {
  if (typeof window === "undefined") return;
  try {
    if (value) window.localStorage.setItem(GET_STARTED_STORAGE_KEY, "1");
    else window.localStorage.removeItem(GET_STARTED_STORAGE_KEY);
  } catch {
    // Best-effort only.
  }
}

/** Whether the home landing's Get started card was dismissed (a browser preference, E5-A D4). */
export const useGetStartedDismissedStore = createPreferenceStore<boolean>(readDismissed, writeDismissed);

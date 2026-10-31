import { parseRunConfigState } from "@/components/composer/run-config-preference";
import type { RunConfigState } from "@/components/composer/run-config-toolbar";
import { createPreferenceStore } from "@/stores/create-preference-store";

const HOME_RUN_CONFIG_STORAGE_KEY = "smind:home-run-config";

function readHomeRunConfig(): RunConfigState | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(HOME_RUN_CONFIG_STORAGE_KEY);
    return raw === null ? null : parseRunConfigState(JSON.parse(raw));
  } catch {
    return null;
  }
}

function writeHomeRunConfig(config: RunConfigState | null): void {
  if (typeof window === "undefined") return;
  try {
    if (config === null) window.localStorage.removeItem(HOME_RUN_CONFIG_STORAGE_KEY);
    else window.localStorage.setItem(HOME_RUN_CONFIG_STORAGE_KEY, JSON.stringify(config));
  } catch {
    // Best-effort only.
  }
}

/** The run config the home landing composer last used. */
export const useHomeRunConfigStore = createPreferenceStore<RunConfigState | null>(readHomeRunConfig, writeHomeRunConfig);

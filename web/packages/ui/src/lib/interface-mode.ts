import { STORAGE_KEYS } from "@/lib/storage";

/** ADR-0049: how much of the developer surface the UI shows. Client-only; it is never sent to the daemon. */
export type InterfaceMode = "coding" | "office";

/**
 * The stored mode. The value is the bare string "office" (not JSON); anything else
 * (missing, "coding", corrupt, throwing storage) is the default, coding.
 */
export function readInterfaceMode(): InterfaceMode {
  try {
    return window.localStorage.getItem(STORAGE_KEYS.interfaceMode) === "office" ? "office" : "coding";
  } catch {
    return "coding";
  }
}

/** Persists the mode; a storage failure (quota, private mode) is silent, the in-memory store still holds it. */
export function writeInterfaceMode(mode: InterfaceMode): void {
  try {
    window.localStorage.setItem(STORAGE_KEYS.interfaceMode, mode);
  } catch {
    // The preference is a convenience.
  }
}

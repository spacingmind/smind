import { useCallback } from "react";

import type { UsageDisplayAs } from "@/lib/account-usage/format";
import {
  toggleUsagePin,
  type PinnedWindow,
  setUsageAccount,
  setUsageDisplayAs,
  setUsageInSidebar,
  type UsagePreferences,
} from "@/lib/account-usage/preferences";
import { useUsagePreferencesStore } from "@/stores/usage-preferences-store";

/** Applies a pure preference update, persists it and notifies every surface. */
export function updateUsagePreferences(update: (prefs: UsagePreferences) => UsagePreferences): void {
  const { value, set } = useUsagePreferencesStore.getState();
  set(update(value));
}

export function useUsagePreferences(): {
  preferences: UsagePreferences;
  displayAs: UsageDisplayAs;
  accountKey: string | null;
  inSidebar: boolean;
  setDisplayAs: (displayAs: UsageDisplayAs) => void;
  setAccountKey: (accountKey: string | null) => void;
  setInSidebar: (inSidebar: boolean) => void;
  /** Pins or unpins one window; the first toggle snapshots `defaults` (what the surface shows pinned) before editing. */
  togglePin: (defaults: readonly PinnedWindow[], pin: PinnedWindow) => void;
} {
  const preferences = useUsagePreferencesStore((s) => s.value);
  const setDisplayAs = useCallback(
    (displayAs: UsageDisplayAs) => updateUsagePreferences((p) => setUsageDisplayAs(p, displayAs)),
    [],
  );
  const setAccountKey = useCallback(
    (accountKey: string | null) => updateUsagePreferences((p) => setUsageAccount(p, accountKey)),
    [],
  );
  const setInSidebar = useCallback(
    (inSidebar: boolean) => updateUsagePreferences((p) => setUsageInSidebar(p, inSidebar)),
    [],
  );
  const togglePin = useCallback(
    (defaults: readonly PinnedWindow[], pin: PinnedWindow) =>
      updateUsagePreferences((p) => toggleUsagePin(p, defaults, pin)),
    [],
  );
  return {
    preferences,
    displayAs: preferences.displayAs,
    accountKey: preferences.accountKey,
    inSidebar: preferences.inSidebar,
    setDisplayAs,
    setAccountKey,
    setInSidebar,
    togglePin,
  };
}

import { useCallback, useSyncExternalStore } from "react";

import type { UsageDisplayAs } from "@/lib/account-usage/format";
import {
  readUsagePreferences,
  setUsageAccount,
  setUsageDisplayAs,
  setUsageInSidebar,
  writeUsagePreferences,
  type UsagePreferences,
} from "@/lib/account-usage/preferences";

/**
 * The plan-usage preferences (Used/Remaining, account filter, "Show summary
 * in sidebar", pins) as one module store shared by every surface -- Settings
 * -> Usage, the modal, and later the footer item and chat hover card -- so a
 * change in one is live in the others. Same shape as
 * use-notification-sound-preference.ts: `current` re-syncs from storage
 * whenever nobody is subscribed (a test-friendly escape hatch, and how a
 * change made in another tab is picked up on the next mount).
 */
let current: UsagePreferences | null = null;
const listeners = new Set<() => void>();

function getSnapshot(): UsagePreferences {
  if (listeners.size === 0 || current === null) current = readUsagePreferences();
  return current;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Applies a pure preference update, persists it and notifies every surface. */
export function updateUsagePreferences(update: (prefs: UsagePreferences) => UsagePreferences): void {
  const next = update(getSnapshot());
  if (next === current) return;
  current = next;
  writeUsagePreferences(next);
  for (const l of listeners) l();
}

export function useUsagePreferences(): {
  preferences: UsagePreferences;
  displayAs: UsageDisplayAs;
  accountKey: string | null;
  inSidebar: boolean;
  setDisplayAs: (displayAs: UsageDisplayAs) => void;
  setAccountKey: (accountKey: string | null) => void;
  setInSidebar: (inSidebar: boolean) => void;
} {
  const preferences = useSyncExternalStore(subscribe, getSnapshot);
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
  return {
    preferences,
    displayAs: preferences.displayAs,
    accountKey: preferences.accountKey,
    inSidebar: preferences.inSidebar,
    setDisplayAs,
    setAccountKey,
    setInSidebar,
  };
}

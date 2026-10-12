import { useCallback, useMemo } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { useAccountUsage, type UsageSnapshot, type UsageStore } from "@/hooks/use-account-usage";
import { useUsagePreferences } from "@/hooks/use-usage-preferences";
import type { UsageDisplayAs } from "@/lib/account-usage/format";
import { resolveAccountFilter, resolveUsageView } from "@/lib/account-usage/model";
import type { AccountUsage, UsageView } from "@/lib/account-usage/types";
import type { WsClient } from "@/lib/ws-client";

export interface UsageAccountOption {
  key: string;
  displayName: string;
}

/** Everything a usage surface (the modal, Settings -> Usage) renders from, in one place so both stay identical. */
export interface UsageSurface {
  view: UsageView;
  snapshot: UsageSnapshot;
  displayAs: UsageDisplayAs;
  /** The account the surface is filtered to, or null for All accounts. */
  filterKey: string | null;
  accounts: UsageAccountOption[];
  setFilterKey: (key: string | null) => void;
  setDisplayAs: (displayAs: UsageDisplayAs) => void;
  inSidebar: boolean;
  setInSidebar: (on: boolean) => void;
  /** Refresh all: one `refresh` for every account. Never called except from a button. */
  refreshAll: () => void;
  refreshOne: (accountId: number) => void;
  /** Error "Try again": re-lists as `cached`. */
  retry: () => void;
  /** Whether Refresh all has anything to act on. */
  canRefresh: boolean;
  busy: boolean;
}

/**
 * The shared state of a usage surface: report store + preferences + the
 * account filter. `touch` is true while the surface is open (the modal when
 * it is open, Settings -> Usage while it is mounted); see useAccountUsage
 * for the call discipline.
 */
export function useUsageSurface(
  client: WsClient | null,
  events: DaemonEvents | null | undefined,
  { touch }: { touch: boolean },
): UsageSurface {
  const { store, snapshot } = useAccountUsage(client, events, { touch });
  const prefs = useUsagePreferences();

  const filterKey = useMemo(() => resolveAccountFilter(prefs.accountKey, snapshot.reports), [prefs.accountKey, snapshot.reports]);
  const reports = useMemo<AccountUsage[]>(
    () => (filterKey ? snapshot.reports.filter((r) => r.key === filterKey) : [...snapshot.reports]),
    [filterKey, snapshot.reports],
  );
  const view = useMemo(
    () =>
      resolveUsageView({
        supportsUsage: snapshot.supportsUsage,
        enabled: snapshot.enabled,
        phase: client ? snapshot.phase : "loading",
        reports,
        error: snapshot.error,
      }),
    [client, reports, snapshot.enabled, snapshot.error, snapshot.phase, snapshot.supportsUsage],
  );
  const accounts = useMemo<UsageAccountOption[]>(
    () => snapshot.reports.map((r) => ({ key: r.key, displayName: r.displayName })),
    [snapshot.reports],
  );

  const refreshAll = useCallback(() => void store?.refreshAll(), [store]);
  const refreshOne = useCallback((id: number) => void store?.refreshOne(id), [store]);
  const retry = useCallback(() => void retryList(store), [store]);

  return {
    view,
    snapshot,
    displayAs: prefs.displayAs,
    filterKey,
    accounts,
    setFilterKey: prefs.setAccountKey,
    setDisplayAs: prefs.setDisplayAs,
    inSidebar: prefs.inSidebar,
    setInSidebar: prefs.setInSidebar,
    refreshAll,
    refreshOne,
    retry,
    canRefresh: view.kind === "ready" && snapshot.reports.length > 0,
    busy: snapshot.refreshingAll || view.kind === "loading",
  };
}

async function retryList(store: UsageStore | null): Promise<void> {
  store?.relist();
}

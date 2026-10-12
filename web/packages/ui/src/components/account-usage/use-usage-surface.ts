import { useCallback, useMemo } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { useAccountUsage, type UsageSnapshot, type UsageStore } from "@/hooks/use-account-usage";
import { useUsagePreferences } from "@/hooks/use-usage-preferences";
import type { UsageDisplayAs } from "@/lib/account-usage/format";
import { resolveAccountFilter, resolveUsageView } from "@/lib/account-usage/model";
import { effectiveUsagePins } from "@/lib/account-usage/preferences";
import type { AccountUsage, UsageView } from "@/lib/account-usage/types";
import type { WsClient } from "@/lib/ws-client";

export interface UsageAccountOption {
  key: string;
  displayName: string;
}

/** Window pinning for a surface; null while "Show summary in sidebar" is off, when rows are plain bars. */
export interface UsagePinning {
  isPinned: (provider: string, windowId: string) => boolean;
  toggle: (provider: string, windowId: string) => void;
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
  /** Pinning is offered only while the sidebar summary is on (AC30). */
  pinning: UsagePinning | null;
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

  const pinning = useMemo<UsagePinning | null>(() => {
    if (!prefs.inSidebar) return null;
    // Pins are provider-wide, so the first toggle snapshots the defaults of every account, not
    // only the filtered one: filtering to Claude must not drop Codex's default pins.
    const pinned = new Set(
      effectiveUsagePins(prefs.preferences, reports).map((pin) => `${pin.provider}\u0000${pin.windowId}`),
    );
    const defaults = effectiveUsagePins({ ...prefs.preferences, pins: null }, snapshot.reports);
    return {
      isPinned: (provider, windowId) => pinned.has(`${provider}\u0000${windowId}`),
      toggle: (provider, windowId) => prefs.togglePin(defaults, { provider, windowId }),
    };
  }, [prefs.inSidebar, prefs.preferences, prefs.togglePin, reports, snapshot.reports]);

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
    pinning,
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

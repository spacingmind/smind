import { queryOptions } from "@tanstack/react-query";

import { queryKeys } from "@/lib/query-keys";
import { RpcError, type WsClient } from "@/lib/ws-client";
import { settleReports } from "./model";
import type { AccountUsage, AccountUsageParams, AccountUsageResult, UsageQueryData } from "./types";

/**
 * Mutation keys of the two explicit actions (ADR-0032). Their entries in the mutation cache are what
 * the touch cadence (`lastTouchAt`) and a card's refreshing / failed / cooldown state are read from,
 * so both are per QueryClient (per connection) and shared by every surface.
 */
export const usageMutationKeys = {
  touch: ["account.usage.touch"] as const,
  refresh: ["account.usage.refresh"] as const,
} as const;

/** Used until the first answer says what the daemon's TTL is. */
export const DEFAULT_TTL_SECONDS = 300;

export function isUsageReport(value: unknown): value is AccountUsage {
  return typeof value === "object" && value !== null && typeof (value as AccountUsage).accountId === "number";
}

export function isUnknownMethod(err: unknown): boolean {
  const message = err instanceof Error ? err.message : String(err);
  return err instanceof RpcError && /unknown method|method not found|not implemented/i.test(message);
}

/** Turns an `account.usage` answer into the query's data, keeping any copy already held that was fetched later (`settleReports`). */
export function usageDataFrom(result: AccountUsageResult | null | undefined, held: UsageQueryData | undefined): UsageQueryData {
  return {
    supportsUsage: true,
    enabled: result?.enabled !== false,
    ttlSeconds: result?.ttlSeconds && result.ttlSeconds > 0 ? result.ttlSeconds : (held?.ttlSeconds ?? DEFAULT_TTL_SECONDS),
    reports: settleReports(held?.reports, (result?.reports ?? []).filter(isUsageReport)),
  };
}

/**
 * The plan-usage query (ADR-0028 + ADR-0032): the daemon's *cached* reports
 * and nothing else. `mode: "cached"` never makes the daemon call a provider,
 * and the options below keep it from being asked again by anything but an
 * explicit action: no refetch on mount, focus or reconnect, no retry, and an
 * unknown-method answer is data (`supportsUsage: false`), not an error to
 * retry. Fresh numbers arrive through `account.usage` events and the explicit
 * touch/refresh mutations, never through this query re-running.
 */
export function usageQueryOptions(client: WsClient | null) {
  return queryOptions({
    queryKey: queryKeys.accountUsage(),
    enabled: client !== null,
    staleTime: Infinity,
    refetchOnMount: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    retry: false,
    queryFn: async ({ client: queryClient }): Promise<UsageQueryData> => {
      if (!client) throw new Error("no daemon connection");
      // A daemon that answered "unknown method" is never asked again, not even by an invalidation.
      const held = queryClient.getQueryData<UsageQueryData>(queryKeys.accountUsage());
      if (held && !held.supportsUsage) return held;
      const params: AccountUsageParams = { mode: "cached" };
      try {
        const result = await client.call<AccountUsageResult>("account.usage", params);
        return usageDataFrom(result, held);
      } catch (err) {
        if (isUnknownMethod(err)) {
          return { supportsUsage: false, enabled: true, ttlSeconds: DEFAULT_TTL_SECONDS, reports: [] };
        }
        throw err;
      }
    },
  });
}

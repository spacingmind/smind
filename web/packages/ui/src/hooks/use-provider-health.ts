import { useMemo } from "react";
import { useQueries, useQueryClient, skipToken, type QueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import type { ProviderHealth } from "@/lib/model-browser";
import { accountHealthTestKey } from "@/lib/provider-health";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { queryKeys } from "@/lib/query-keys";
import type { ProviderInfo, ProviderTestResult } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/**
 * Lazy per-provider health over the shared cache (ADR-0032 AC12):
 * `provider.test` is a local PATH / stored-credential check -- no network --
 * but it is still only run when something calls `ensure()` (the composer
 * picker's first open, the sidebar footer's once-per-connection count) or
 * when the user presses Test (`useRunProviderTest`) -- never on render,
 * focus or a retry loop.
 *
 * Results live under `queryKeys.providerTest(accountHealthTestKey(id,
 * providers))`, so the sidebar's "N healthy" footer and the Providers /
 * Agents dots read the SAME answers. `health[id]` never triggers a fetch
 * (skipToken cache read); `ensure()` fills the missing keys with
 * `fetchQuery` -- single-flight per key (a key already fetching or cached is
 * skipped), so two opens while the first round is in flight test each
 * provider exactly once. A failed RPC leaves the key untested (undefined =
 * enabled, never cached as failed) so a later open can try again. A new
 * daemon connection is a new QueryClient, so all of it is per-connection.
 */
export function useProviderHealth(): {
  health: Record<string, ProviderHealth | undefined>;
  ensure: () => void;
  summary: { healthy: number; total: number };
} {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const { providers } = useProviderCatalog();

  const tests = useQueries({
    queries: providers.map((info) => ({
      queryKey: queryKeys.providerTest(accountHealthTestKey(info.id, providers)),
      queryFn: skipToken,
      staleTime: Infinity,
      gcTime: Infinity,
    })),
  });

  const health = useMemo(() => {
    const map: Record<string, ProviderHealth | undefined> = {};
    providers.forEach((info, i) => {
      const result = tests[i].data as ProviderTestResult | undefined;
      map[info.id] = result ? { ok: result.ok, detail: result.detail } : undefined;
    });
    return map;
  }, [providers, tests]);

  const summary = useMemo(() => {
    let healthy = 0;
    for (const info of providers) {
      if (health[info.id]?.ok) healthy++;
    }
    return { healthy, total: providers.length };
  }, [providers, health]);

  function ensure(): void {
    if (!client) return;
    for (const info of providers) {
      runProviderTest(queryClient, client, info, providers);
    }
  }

  return { health, ensure, summary };
}

/**
 * Runs `provider.test` for one provider into the shared cache unless the key
 * already has an answer or an in-flight request. Never throws: a failure
 * leaves the key untested so a later `ensure()` (or a Test button) can retry.
 */
export function runProviderTest(
  queryClient: QueryClient,
  client: WsClient,
  info: ProviderInfo,
  providers: ProviderInfo[],
): void {
  const key = accountHealthTestKey(info.id, providers);
  const queryKey = queryKeys.providerTest(key);
  if (queryClient.getQueryData(queryKey) !== undefined) return;
  if (queryClient.getQueryState(queryKey)?.fetchStatus === "fetching") return;
  void queryClient
    .fetchQuery({
      queryKey,
      queryFn: async (): Promise<ProviderTestResult> => client.call<ProviderTestResult>("provider.test", { provider: key }),
      staleTime: Infinity,
      retry: false,
    })
    .catch(() => {
      // Untested, not unhealthy: allow a later open to try again.
    });
}

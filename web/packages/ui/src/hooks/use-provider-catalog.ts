import { queryOptions, useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { FALLBACK_PROVIDERS } from "@/lib/provider-catalog-fallback";
import { queryKeys } from "@/lib/query-keys";
import type { ProviderInfo, ProviderListResult } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/** The one `provider.list` query definition: the hook and one-off readers (`ensureQueryData`) share it, so the key has a single fetcher. */
export function providerListQueryOptions(client: WsClientLike | null) {
  return queryOptions({
    queryKey: queryKeys.providerList(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    // A failed list is not re-sent by every component that mounts afterwards (a reconnect/event invalidation re-reads it).
    retryOnMount: false,
    queryFn: async (): Promise<ProviderListResult> => {
      if (!client) throw new Error("no daemon connection");
      try {
        const result = await client.call<ProviderListResult>("provider.list");
        if (!result) throw new Error("provider.list returned nothing");
        if (result.providers.length === 0) console.error("provider.list returned no providers, using fallback provider list");
        return result;
      } catch (err) {
        // Logged once per failed fetch (not per render); consumers fall back to FALLBACK_PROVIDERS.
        console.error("provider.list failed, using fallback provider list", err);
        throw err;
      }
    },
  });
}

/**
 * The daemon's provider catalog, ONE cache entry per connection (ADR-0032
 * AC10): every surface that needs `provider.list` mounts this hook instead of
 * its own fetch, so composer, sidebar, task headers and Settings share the
 * same answer. The raw `ProviderListResult` is cached; `providers` is the
 * daemon's own list (empty while loading or after a failure -- a failed list
 * never throws to the UI), and `providersOrFallback` is what surfaces that
 * must stay usable render.
 */
/** A stable empty list: a fresh `[]` per render would re-run every effect/memo keyed on `providers`. */
const NO_PROVIDERS: ProviderInfo[] = [];

/** How long a picker waits on provider.list before it falls back to the built-in catalog. */
const PROVIDER_LIST_TIMEOUT_MS = 8000;

export function useProviderCatalog(): {
  providers: ProviderInfo[];
  providersOrFallback: ProviderInfo[];
  loaded: boolean;
  /** `loaded`, or the list failed, or it did not answer within PROVIDER_LIST_TIMEOUT_MS: a picker stops showing "Loading…". */
  settled: boolean;
  error: Error | null;
} {
  const { client } = useDaemonConnection();
  const query = useQuery(providerListQueryOptions(client));
  const providers = query.data?.providers ?? NO_PROVIDERS;
  // A daemon that never answers must not leave a picker on "Loading…" forever: the fallback catalog takes over.
  // Per connection: a new client waits (shows "Loading…") again before falling back.
  const [timedOutFor, setTimedOutFor] = useState<WsClientLike | null>(null);
  const timedOut = client !== null && timedOutFor === client;
  const waiting = client !== null && query.data === undefined && !query.error;
  useEffect(() => {
    if (!waiting) return;
    const giveUp = setTimeout(() => setTimedOutFor(client), PROVIDER_LIST_TIMEOUT_MS);
    return () => clearTimeout(giveUp);
  }, [waiting, client]);
  return {
    providers,
    providersOrFallback: providers.length > 0 ? providers : FALLBACK_PROVIDERS,
    loaded: query.data !== undefined,
    settled: query.data !== undefined || !!query.error || timedOut,
    error: query.error ?? null,
  };
}

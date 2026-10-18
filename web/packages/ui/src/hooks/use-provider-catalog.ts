import { useQuery } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { FALLBACK_PROVIDERS } from "@/lib/provider-catalog-fallback";
import { queryKeys } from "@/lib/query-keys";
import type { ProviderInfo, ProviderListResult } from "@/lib/types";

/**
 * The daemon's provider catalog, ONE cache entry per connection (ADR-0032
 * AC10): every surface that needs `provider.list` mounts this hook instead of
 * its own fetch, so composer, sidebar, task headers and Settings share the
 * same answer. The raw `ProviderListResult` is cached; `providers` is the
 * daemon's own list (empty while loading or after a failure -- a failed list
 * never throws to the UI), and `providersOrFallback` is what surfaces that
 * must stay usable render.
 */
export function useProviderCatalog(): {
  providers: ProviderInfo[];
  providersOrFallback: ProviderInfo[];
  loaded: boolean;
  error: Error | null;
} {
  const { client } = useDaemonConnection();
  const query = useQuery({
    queryKey: queryKeys.providerList(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    queryFn: async (): Promise<ProviderListResult> => {
      if (!client) throw new Error("no daemon connection");
      const result = await client.call<ProviderListResult>("provider.list");
      if (!result) throw new Error("provider.list returned nothing");
      if (result.providers.length === 0) console.error("provider.list returned no providers, using fallback provider list");
      return result;
    },
  });
  if (query.error && query.fetchStatus !== "fetching") {
    console.error("provider.list failed, using fallback provider list", query.error);
  }
  const providers = query.data?.providers ?? [];
  return {
    providers,
    providersOrFallback: providers.length > 0 ? providers : FALLBACK_PROVIDERS,
    loaded: query.data !== undefined,
    error: query.error ?? null,
  };
}

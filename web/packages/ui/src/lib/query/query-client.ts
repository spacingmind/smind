import { QueryClient } from "@tanstack/react-query";

/**
 * Defaults for every daemon query (ADR-0032), ported from Paseo's
 * `data/query-client.ts`: data is kept fresh by daemon events
 * (`event-bridge.ts`), not by refetching, so nothing refetches on mount, focus
 * or reconnect. A query that needs different behaviour says so itself.
 *
 * `retry` is one attempt: a wsapi error from a connected daemon is almost
 * always deterministic, and a dropped socket is handled by the reconnect loop
 * swapping the WsClient (and with it the whole QueryClient).
 *
 * Rate-limited keys (`account.usage`, ADR-0028) additionally set `retry: false`
 * in `usageQueryOptions`, so a failure never fans out into repeat requests.
 */
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: Infinity,
        gcTime: Infinity,
        refetchOnMount: false,
        refetchOnReconnect: false,
        refetchOnWindowFocus: false,
        retry: 1,
      },
    },
  });
}

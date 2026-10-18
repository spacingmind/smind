import { QueryClient } from "@tanstack/react-query";

/**
 * Defaults for every daemon query (ADR-0032), ported from Paseo's
 * `data/query-client.ts`: data is kept fresh by daemon events
 * (`event-bridge.ts`), not by refetching, so nothing refetches on focus or
 * reconnect, and a cached entry is used as is on mount -- unless it was
 * invalidated (a same-daemon reconnect invalidates everything, see
 * `useDaemonEvents`), in which case the next mount re-reads it. A query that
 * needs different behaviour says so itself.
 *
 * `networkMode: "always"`: queries and mutations are RPCs over an already
 * open socket to a (usually local) daemon, not HTTP. `navigator.onLine` says
 * nothing about it, and the default `"online"` would *pause* a mutation while
 * the browser reports offline and send it later, outside the window and TTL
 * gates that decide when a `touch` may go out (ADR-0028).
 *
 * `retry` is one attempt: a wsapi error from a connected daemon is almost
 * always deterministic, and a dropped socket is handled by the reconnect loop
 * handing the shell a new WsClient.
 *
 * Rate-limited keys (`account.usage`, ADR-0028) additionally set `retry: false`
 * in `usageQueryOptions`, so a failure never fans out into repeat requests.
 */
/** Re-read an entry on mount only if it was invalidated (an event, a same-daemon reconnect); a fresh one is used as is. */
export const refetchIfInvalidated = (query: { state: { isInvalidated: boolean } }): boolean => query.state.isInvalidated;

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: Infinity,
        gcTime: Infinity,
        networkMode: "always",
        refetchOnMount: refetchIfInvalidated,
        refetchOnReconnect: false,
        refetchOnWindowFocus: false,
        retry: 1,
      },
      mutations: { networkMode: "always" },
    },
  });
}

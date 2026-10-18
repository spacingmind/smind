import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { createContext, useContext, useMemo, useRef, type ReactNode } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { daemonWsBaseUrl, type WsClient } from "@/lib/ws-client";
import { createQueryClient } from "./query-client";

/**
 * One QueryClient per daemon *connection*, not per `WsClient` object (ADR-0032).
 *
 * - A transport reconnect to the same daemon (the reconnect loop hands the
 *   shell a new `WsClient`, same `identity`) keeps the cache, so nothing
 *   flashes back to loading. `useDaemonEvents` then invalidates every query
 *   once the new connection's `events.subscribe` has been answered (so no
 *   event can fall between the re-read and the subscription): active queries
 *   re-read in the background, inactive ones re-read on their next mount
 *   (`createQueryClient`'s `refetchOnMount`). The usage re-read is the daemon's
 *   *cached* list, so a reconnect sends no `touch` or `refresh` (ADR-0028).
 * - A different `identity` (another daemon) gets a new QueryClient and the old
 *   one is cleared, so one daemon's data is never shown for another.
 *
 * `identity` is the connection as the app knows it: the daemon's ws endpoint
 * (scheme + host, never the token). The web UI is same-origin, so a desktop
 * connection switch lands on another origin and therefore another identity.
 */
export function useDaemonQueryClient(identity: string = daemonWsBaseUrl()): QueryClient {
  const held = useRef<{ identity: string; queryClient: QueryClient } | null>(null);
  if (held.current === null || held.current.identity !== identity) {
    // Another daemon: drop everything the previous one told us. Cleared here, not in an effect
    // cleanup: StrictMode's simulated unmount would otherwise wipe the live cache on every dev mount.
    held.current?.queryClient.clear();
    held.current = { identity, queryClient: createQueryClient() };
  }
  const { queryClient } = held.current;
  return queryClient;
}

/** The connection a daemon query talks to; both are null until the first connect. */
export interface DaemonConnection {
  client: WsClient | null;
  events: DaemonEvents | null;
}

const DaemonConnectionContext = createContext<DaemonConnection>({ client: null, events: null });

/** Provides the QueryClient and the connection its hooks call through. */
export function DaemonQueryProvider({
  queryClient,
  client,
  events,
  children,
}: DaemonConnection & { queryClient: QueryClient; children: ReactNode }) {
  const value = useMemo(() => ({ client, events }), [client, events]);
  return (
    <QueryClientProvider client={queryClient}>
      <DaemonConnectionContext.Provider value={value}>{children}</DaemonConnectionContext.Provider>
    </QueryClientProvider>
  );
}

/** The current connection (client + event bus) for query hooks. */
export function useDaemonConnection(): DaemonConnection {
  return useContext(DaemonConnectionContext);
}

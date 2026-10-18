import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useMemo, useRef, type ReactNode } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import { daemonWsBaseUrl, type WsClient } from "@/lib/ws-client";
import { createQueryClient } from "./query-client";

/**
 * One QueryClient per daemon *connection*, not per `WsClient` object (ADR-0032).
 *
 * - A transport reconnect to the same daemon (the reconnect loop hands the
 *   shell a new `WsClient`, same `identity`) keeps the cache, so nothing
 *   flashes back to loading; every query is invalidated instead and
 *   re-reads in the background. Invalidation only re-runs active queries, and
 *   the usage query's re-read is the daemon's *cached* list, so a reconnect
 *   still sends no `touch` or `refresh` (ADR-0028).
 * - A different `identity` (another daemon) gets a new QueryClient and the old
 *   one is cleared, so one daemon's data is never shown for another.
 *
 * `identity` is the connection as the app knows it: the daemon's ws endpoint
 * (scheme + host, never the token). The web UI is same-origin, so a desktop
 * connection switch lands on another origin and therefore another identity.
 */
export function useDaemonQueryClient(
  client: WsClient | null,
  identity: string = daemonWsBaseUrl(),
): QueryClient {
  const held = useRef<{ identity: string; queryClient: QueryClient } | null>(null);
  if (held.current === null || held.current.identity !== identity) {
    held.current = { identity, queryClient: createQueryClient() };
  }
  const { queryClient } = held.current;
  useEffect(() => () => queryClient.clear(), [queryClient]);

  // Same daemon, new transport: refetch in the background. Runs after the children's effects, so
  // their queries already hold the new client when they re-read.
  const lastClient = useRef<WsClient | null>(null);
  useEffect(() => {
    const previous = lastClient.current;
    lastClient.current = client;
    if (previous !== null && client !== null && previous !== client) {
      void queryClient.invalidateQueries();
    }
  }, [client, queryClient]);
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

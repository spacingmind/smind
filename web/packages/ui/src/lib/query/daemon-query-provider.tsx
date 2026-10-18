import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useMemo, useRef, type ReactNode } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { WsClient } from "@/lib/ws-client";
import { createQueryClient } from "./query-client";

/**
 * One QueryClient per WsClient (ADR-0032): a reconnect or a switch to another
 * daemon yields a new WsClient, so the previous connection's data is dropped
 * with it and can never be shown for the new one. The old client is cleared
 * when it is replaced or the app unmounts.
 */
export function useDaemonQueryClient(client: WsClient | null): QueryClient {
  const held = useRef<{ client: WsClient | null; queryClient: QueryClient } | null>(null);
  if (held.current === null || held.current.client !== client) {
    held.current = { client, queryClient: createQueryClient() };
  }
  const { queryClient } = held.current;
  useEffect(() => () => queryClient.clear(), [queryClient]);
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

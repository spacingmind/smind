import { queryOptions, useQuery } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import { selectObservedModel } from "@/lib/observed-model";
import type { RunSummary } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/** The shared `run.list` entry (every run the registry holds); one cache entry serves every reader, refreshed at most every 30 s. */
export function runListQueryOptions(client: WsClient | null, enabled = true) {
  return queryOptions({
    queryKey: queryKeys.runList(),
    enabled: client !== null && enabled,
    staleTime: 30_000,
    refetchOnMount: true,
    retry: false,
    queryFn: async ({ signal }): Promise<RunSummary[]> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<RunSummary[] | null>("run.list", undefined, { signal })) ?? [];
    },
  });
}

/**
 * The model a "Default" model choice last resolved to for this chat (else for this provider), read from
 * the shared run list. Pass `enabled=false` when the chip does not need it (an explicit model is picked).
 */
export function useObservedModel(chatId: number | null, provider: string, enabled = true): string | undefined {
  const runs = useQuery(runListQueryOptions(useDaemonConnection().client, enabled)).data;
  return enabled ? selectObservedModel(runs, { chatId, provider }) : undefined;
}

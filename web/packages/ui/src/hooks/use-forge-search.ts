import { keepPreviousData, useQuery } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";

export interface ForgeSearchItem {
  number: number;
  title: string;
  state: string;
  author: string;
  url: string;
  updatedAt: string;
  headRef?: string;
}

export interface ForgeSearchResult {
  forge: "github";
  repo: string;
  items: ForgeSearchItem[];
}

/** `forge.search` for the PR/issue picker: 30 s fresh, and the previous query's rows stay while the next one loads. */
export function useForgeSearch({ taskId, kind, query, enabled }: { taskId: number | null; kind: "pr" | "issue"; query: string; enabled: boolean }) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.forgeSearch(taskId ?? -1, kind, query),
    enabled: enabled && client !== null && taskId !== null,
    staleTime: 30_000,
    retry: false,
    placeholderData: keepPreviousData,
    queryFn: async (): Promise<ForgeSearchResult> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<ForgeSearchResult>("forge.search", { forge: "github", taskId, kind, query, limit: 30 });
    },
  });
}

import { keepPreviousData, useQuery } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { WorkspaceBranches } from "@/lib/types";

/** `workspace.branches` for the New Task dialog: local, no network, so 30 s fresh; the previous query's rows stay while the next loads. */
export function useWorkspaceBranches({ workspaceId, query = "", limit = 30, enabled = true }: { workspaceId: number; query?: string; limit?: number; enabled?: boolean }) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.workspaceBranches(workspaceId, query, limit),
    enabled: enabled && client !== null,
    staleTime: 30_000,
    retry: false,
    placeholderData: keepPreviousData,
    queryFn: async (): Promise<WorkspaceBranches> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<WorkspaceBranches>("workspace.branches", { workspaceId, query, limit });
    },
  });
}

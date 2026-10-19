import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import { workspaceTreeQueryOptions } from "@/lib/workspace-tree-query";
import type { WorkspaceWithTree } from "@/lib/workspace-tree";

/**
 * The workspace tree (workspaces -> spaces -> tasks) from the shared query cache. `workspaces` is null while
 * the first fetch is in flight, `error` the first fetch's failure message. ADR 0009's lifecycle topics keep
 * it live (the event bridge splices them in, so a workspace/space/task created, archived or deleted anywhere
 * -- a second tab, the CLI -- appears with no refetch); a new connection and `event.dropped` re-read it.
 *
 * `refresh` is a background re-read: the acting client still calls it after its own successful mutation even
 * though the event covers that case too (events.subscribe is fire-and-forget, so a connection that failed to
 * subscribe must not silently stop showing this user's own new rows); the splice upserts by ID, so the two
 * paths converge instead of double-inserting.
 */
export function useWorkspaceTree(): { workspaces: WorkspaceWithTree[] | null; error: string | null; refresh: () => void } {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const query = useQuery(workspaceTreeQueryOptions(client));
  const refresh = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.workspaceTree() });
  }, [queryClient]);
  const error = query.data === undefined && query.error ? (query.error instanceof Error ? query.error.message : String(query.error)) : null;
  return { workspaces: query.data ?? null, error, refresh };
}

import { useQueries, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { TaskGit, TaskGitListResult } from "@/lib/types";

/**
 * Task git/PR state as shared cache entries (ADR-0039 §3, ADR-0032): one `task.gitList` per workspace and a
 * `task.git` per task, both kept live by the `task.git` event through the event bridge and re-read after a
 * reconnect. `staleTime: Infinity` -- the daemon pushes, the UI never polls.
 */

const NO_ENTRIES: ReadonlyMap<number, TaskGit> = new Map();

/** Merges the per-workspace lists by task id; a stable function so `combine` is memoised. */
function mergeLists(results: readonly { data?: TaskGitListResult }[]): ReadonlyMap<number, TaskGit> {
  const merged = new Map<number, TaskGit>();
  for (const r of results) for (const item of r.data?.items ?? []) merged.set(item.taskId, item);
  return merged.size === 0 ? NO_ENTRIES : merged;
}

/** Every task's git state across the given workspaces, merged by task id. */
export function useWorkspaceTaskGit(workspaceIds: readonly number[]): ReadonlyMap<number, TaskGit> {
  const { client } = useDaemonConnection();
  return useQueries({
    combine: mergeLists,
    queries: workspaceIds.map((workspaceId) => ({
      queryKey: queryKeys.workspaceTaskGit(workspaceId),
      enabled: client !== null,
      staleTime: Infinity,
      retry: false,
      retryOnMount: false,
      queryFn: async (): Promise<TaskGitListResult> => {
        if (!client) throw new Error("no daemon connection");
        return (await client.call<TaskGitListResult>("task.gitList", { workspaceId })) ?? { items: [] };
      },
    })),
  });
}

/** The task's entry from any cached workspace list, if one holds it. */
function fromWorkspaceLists(queryClient: QueryClient, taskId: number): TaskGit | undefined {
  for (const [, list] of queryClient.getQueriesData<TaskGitListResult>({ queryKey: queryKeys.workspaceTaskGitAll() })) {
    const found = list?.items.find((item) => item.taskId === taskId);
    if (found) return found;
  }
  return undefined;
}

/** One task's git state: the workspace list's entry when it is cached, else a `task.git` read. */
export function useTaskGit(taskId: number | null): TaskGit | undefined {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: queryKeys.taskGit(taskId ?? -1),
    enabled: client !== null && taskId !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    initialData: () => (taskId === null ? undefined : fromWorkspaceLists(queryClient, taskId)),
    queryFn: async (): Promise<TaskGit> => {
      if (!client || taskId === null) throw new Error("no daemon connection");
      return client.call<TaskGit>("task.git", { taskId });
    },
  });
  return data;
}

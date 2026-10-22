import { useQuery } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { TaskSearchIndexResult } from "@/lib/types";

const NONE: string[] = [];

/**
 * The task worktree's paths (`task.searchIndex`) for the composer's `@` mention. Fetched only while `enabled` (a
 * mention is active), once per task per 30 s; the event bridge invalidates it when a run ends.
 */
export function useTaskPaths(taskId: number | null, enabled: boolean) {
  const { client } = useDaemonConnection();
  const query = useQuery({
    queryKey: queryKeys.taskSearchIndex(taskId ?? -1),
    enabled: enabled && client !== null && taskId !== null,
    staleTime: 30_000,
    retry: false,
    queryFn: async (): Promise<string[]> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<TaskSearchIndexResult>("task.searchIndex", { taskId }))?.paths ?? NONE;
    },
  });
  return { paths: query.data ?? NONE, loading: query.isPending && query.fetchStatus !== "idle", error: query.isError };
}

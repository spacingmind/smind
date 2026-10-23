import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { ArchivePreview, Task, TaskArchiveParams } from "@/lib/types";

/** How long a preview is trusted; the dialog refetches on every open regardless. */
const PREVIEW_STALE_MS = 5_000;

/** `task.archivePreview` for the archive dialog: local git and cache only, so refetching on open is cheap. */
export function useArchivePreview(taskId: number | null, enabled: boolean) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.archivePreview(taskId ?? -1),
    enabled: enabled && taskId !== null && client !== null,
    staleTime: PREVIEW_STALE_MS,
    refetchOnMount: "always",
    retry: false,
    queryFn: async (): Promise<ArchivePreview> => {
      if (!client || taskId === null) throw new Error("no daemon connection");
      return client.call<ArchivePreview>("task.archivePreview", { taskId });
    },
  });
}

/** `task.archive`. The tree follows the `task.archived` event; the Archived view is invalidated here and by the bridge. */
export function useArchiveTask() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<Task, Error, TaskArchiveParams>({
    mutationFn: async (params) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<Task>("task.archive", params);
    },
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKeys.archivedTasksAll() }),
  });
}

/** `task.restore`: the restored task comes back into the tree through the daemon's lifecycle event. */
export function useRestoreTask() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<Task, Error, { taskId: number }>({
    mutationFn: async (params) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<Task>("task.restore", params);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.archivedTasksAll() });
      void queryClient.invalidateQueries({ queryKey: queryKeys.workspaceTree() });
    },
  });
}

/** A workspace's archived tasks, newest archive first. Fetched only once the workspace is shown. */
export function useArchivedTasks(workspaceId: number, enabled: boolean): Task[] | undefined {
  const { client } = useDaemonConnection();
  const { data } = useQuery({
    queryKey: queryKeys.archivedTasks(workspaceId),
    enabled: enabled && client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<Task[]> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<Task[]>("task.list", { workspaceId, archived: true })) ?? [];
    },
  });
  return data;
}

import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type {
  PrPreview,
  TaskCreatePrParams,
  TaskCreatePrResult,
  TaskUpdatePrResult,
} from "@/lib/types";

/** How long a preview is trusted; the dialog refetches on every open regardless. */
const PREVIEW_STALE_MS = 5_000;

/** `task.prPreview` for the Create PR dialog: local git and cache only, so refetching on open is cheap. */
export function usePrPreview(taskId: number, baseBranch: string, enabled: boolean) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.prPreview(taskId, baseBranch),
    enabled: enabled && client !== null,
    staleTime: PREVIEW_STALE_MS,
    refetchOnMount: "always",
    placeholderData: keepPreviousData,
    retry: false,
    queryFn: async (): Promise<PrPreview> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<PrPreview>("task.prPreview", baseBranch ? { taskId, baseBranch } : { taskId });
    },
  });
}

/**
 * `task.createPr`. Nothing is written to the `task.git` cache by hand: the daemon's `task.git` event carries the
 * new PR; the preview is the only entry that goes stale.
 */
export function useCreatePr() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<TaskCreatePrResult, Error, TaskCreatePrParams>({
    mutationFn: async (params) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<TaskCreatePrResult>("task.createPr", params);
    },
    // Not awaited: the mutation settles (and the dialog closes) without waiting for the stale preview to refetch.
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKeys.prPreviewAll() }),
  });
}

/** `task.updatePr`: a fast-forward push of the task's branch to its open PR. */
export function useUpdatePr() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useMutation<TaskUpdatePrResult, Error, { taskId: number }>({
    mutationFn: async (params) => {
      if (!client) throw new Error("no daemon connection");
      return client.call<TaskUpdatePrResult>("task.updatePr", params);
    },
    // Not awaited: the mutation settles (and the dialog closes) without waiting for the stale preview to refetch.
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: queryKeys.prPreviewAll() }),
  });
}

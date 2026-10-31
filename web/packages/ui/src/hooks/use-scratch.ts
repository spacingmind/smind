import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { useDaemonConnection } from "@/lib/query/daemon-query-provider";

/** `scratch.usage` (human-only, no params): sizes of the per-task scratch root, counts only, no paths. */
export interface ScratchUsage {
  totalBytes: number;
  totalFiles: number;
  /** Task folders under the scratch root. */
  dirs: number;
  /** Folders of closed or unknown tasks (a running task's folder is never reclaimable). */
  reclaimableDirs: number;
  reclaimableBytes: number;
}

/** `scratch.clean` (human-only, no params): what the sweep of closed tasks' folders removed. */
export interface ScratchCleanResult {
  files: number;
  bytes: number;
  dirs: number;
}

const scratchUsageKey = ["scratch.usage"] as const;

/**
 * The scratch root's size, read when the Settings card opens (a fresh read on every mount: there is no
 * daemon event for scratch changes, and a task finishing in the meantime changes what is reclaimable)
 * and again after a clean. A failure is returned as `error` with the daemon's text, as-is.
 */
export function useScratchUsage(): {
  usage: ScratchUsage | null;
  loading: boolean;
  error: string | null;
  refresh: () => void;
} {
  const { client } = useDaemonConnection();
  const query = useQuery({
    queryKey: scratchUsageKey,
    enabled: client !== null,
    staleTime: 0,
    refetchOnMount: "always",
    retry: false,
    queryFn: async (): Promise<ScratchUsage> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<ScratchUsage>("scratch.usage");
    },
  });
  return {
    usage: query.data ?? null,
    loading: query.isPending && query.fetchStatus !== "idle",
    error: query.isError ? query.error.message : null,
    refresh: () => void query.refetch(),
  };
}

/**
 * `scratch.clean`: sweep the closed tasks' folders. The daemon never touches a running task (its busy
 * guard keeps those), and any refusal it answers with is returned verbatim as `error`. A success
 * re-reads the usage so the card shows what is left.
 */
export function useScratchClean(): {
  clean: () => void;
  pending: boolean;
  result: ScratchCleanResult | null;
  error: string | null;
  reset: () => void;
} {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const mutation = useMutation<ScratchCleanResult, Error, void>({
    mutationFn: async () => {
      if (!client) throw new Error("no daemon connection");
      return client.call<ScratchCleanResult>("scratch.clean");
    },
    onSettled: () => {
      // Success or refusal: the folders may have changed either way.
      void queryClient.invalidateQueries({ queryKey: scratchUsageKey });
    },
  });
  const { mutate, reset } = mutation;
  return {
    clean: () => mutate(),
    pending: mutation.isPending,
    result: mutation.data ?? null,
    error: mutation.isError ? mutation.error.message : null,
    reset,
  };
}

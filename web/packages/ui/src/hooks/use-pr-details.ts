import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { describePrError, type PrErrorView } from "@/components/git/pr-error";
import { useTaskGit } from "@/hooks/use-task-git";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { CheckLogTail, PRDetails } from "@/lib/types";

/**
 * The PR panel's data (G9, docs/plans/active/pr-panel.md): a pull, never a push. The query is enabled only while the
 * PR tab is focused and the task has a PR, after the daemon has been told the tab is open (`task.prDetailsFocus`:
 * without it a read never fetches). While checks are pending it refetches every 20 s; the daemon enforces the rule
 * below the RPC, so this is a request, not a budget.
 */

const PENDING_REFETCH_MS = 20_000;

/**
 * Tells the daemon this task's PR tab is open and focused (`task.prDetailsFocus {taskId}`), and clears it
 * (`{taskId: null}`) on blur, unmount, or a client change; the focus is per connection, so a reconnect sends it
 * again. Returns true once the daemon has acknowledged it for the current client: reads before that would be
 * `not_focused`.
 */
export function usePRDetailsFocus(taskId: number | null, focused: boolean): boolean {
  const { client } = useDaemonConnection();
  const [ready, setReady] = useState<{ client: object; taskId: number } | null>(null);

  useEffect(() => {
    if (!client || !focused || taskId === null) return;
    let live = true;
    client.call("task.prDetailsFocus", { taskId }).then(
      () => {
        if (live) setReady({ client, taskId });
      },
      () => {
        // Not delivered: stay not-ready, so no read is made that the daemon would refuse to fetch.
      },
    );
    return () => {
      live = false;
      setReady(null);
      client.call("task.prDetailsFocus", { taskId: null }).catch(() => {
        // The connection may be gone; the daemon drops the focus with it.
      });
    };
  }, [client, focused, taskId]);

  return focused && taskId !== null && ready?.client === client && ready?.taskId === taskId;
}

/** One task's PR details; `data` stays cached while blurred but nothing is read or polled. */
export function usePRDetails(taskId: number | null, { focused }: { focused: boolean }) {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const git = useTaskGit(taskId);
  const hasPR = git?.pr !== undefined;
  const focusSent = usePRDetailsFocus(taskId, focused && hasPR);

  const query = useQuery({
    queryKey: queryKeys.taskPRDetails(taskId ?? -1),
    enabled: client !== null && taskId !== null && focused && hasPR && focusSent,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    refetchInterval: (q) => (focused && q.state.data?.checks.state === "pending" ? PENDING_REFETCH_MS : false),
    queryFn: async (): Promise<PRDetails> => {
      if (!client || taskId === null) throw new Error("no daemon connection");
      return client.call<PRDetails>("task.prDetails", { taskId });
    },
  });

  // The header snapshot moved (a push, a check finished): the next read is worth a call. Invalidation only marks a
  // query that is not enabled stale, so a blurred tab reads once when it is focused again.
  const pr = git?.pr;
  const signature = pr ? `${pr.headSha}|${pr.checks?.state}|${pr.checks?.passed}|${pr.checks?.failed}|${pr.checks?.pending}|${pr.checks?.total}` : "";
  const seen = useRef(signature);
  useEffect(() => {
    if (seen.current === signature) return;
    seen.current = signature;
    if (taskId !== null) void queryClient.invalidateQueries({ queryKey: queryKeys.taskPRDetails(taskId) });
  }, [signature, taskId, queryClient]);

  return query;
}

/** The panel's Refresh: an explicit `refresh: true` read whose result replaces the cache. */
export function useRefreshPRDetails(taskId: number) {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const mutation = useMutation({
    mutationFn: async (): Promise<PRDetails> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<PRDetails>("task.prDetails", { taskId, refresh: true });
    },
    onSuccess: (details) => queryClient.setQueryData(queryKeys.taskPRDetails(taskId), details),
  });
  const error: PrErrorView | null = mutation.error ? describePrError(mutation.error, "details") : null;
  return { refresh: mutation.mutate, isPending: mutation.isPending, error, reset: mutation.reset };
}

/** A failing check's log tail, read only when its "Send to chat" is clicked. */
export function usePRCheckLog() {
  const { client } = useDaemonConnection();
  return useMutation({
    mutationFn: async ({ taskId, checkId }: { taskId: number; checkId: string }): Promise<CheckLogTail> => {
      if (!client) throw new Error("no daemon connection");
      return client.call<CheckLogTail>("task.prCheckLog", { taskId, checkId });
    },
  });
}

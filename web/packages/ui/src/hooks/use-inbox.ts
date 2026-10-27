import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import type { PendingPermission } from "@/hooks/use-run-timeline";
import { canSnoozeOrResolve, snoozeUntil, type InboxCounts, type InboxItem } from "@/lib/inbox-types";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { queryKeys } from "@/lib/query-keys";
import type { RunLogsResult } from "@/lib/types";

/**
 * The inbox badge (`inbox.count`, ADR-0045): ONE cache entry per connection, refetched by the event bridge on
 * `inbox.changed`. Reads are open to relay sessions.
 */
export function useInboxCount() {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.inboxCount(),
    enabled: client !== null,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<InboxCounts> => {
      if (!client) throw new Error("no daemon connection");
      const c = await client.call<Partial<InboxCounts>>("inbox.count");
      return { total: c?.total ?? 0, unseen: c?.unseen ?? 0, byKind: c?.byKind ?? {} };
    },
  });
}

/** The visible inbox items (`inbox.list`: new and seen), already ordered by the daemon. */
export function useInboxList(enabled = true) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.inboxList(),
    enabled: client !== null && enabled,
    staleTime: Infinity,
    retry: false,
    retryOnMount: false,
    queryFn: async (): Promise<InboxItem[]> => {
      if (!client) throw new Error("no daemon connection");
      return (await client.call<{ items: InboxItem[] }>("inbox.list", {}))?.items ?? [];
    },
  });
}

/**
 * The permission request behind a `perm:` row, with its options and detail: the unresolved `permission_request`
 * of the run's own log (`run.logs`, the same source the task page backfills from). null once it is answered.
 */
export function useInboxRequest(runId: string | undefined, requestId: string | undefined, enabled: boolean) {
  const { client } = useDaemonConnection();
  return useQuery({
    queryKey: queryKeys.inboxRequest(runId ?? "", requestId ?? ""),
    enabled: client !== null && enabled && !!runId && !!requestId,
    staleTime: Infinity,
    retry: false,
    queryFn: async (): Promise<PendingPermission | null> => {
      if (!client || !runId) throw new Error("no daemon connection");
      const logs = await client.call<RunLogsResult>("run.logs", { runId });
      const resolved = new Set(logs.events.filter((e) => e.type === "permission_resolved").map((e) => e.requestId));
      const ev = logs.events.find((e) => e.type === "permission_request" && e.requestId === requestId && !resolved.has(e.requestId));
      if (!ev?.requestId) return null;
      return { requestId: ev.requestId, summary: ev.summary ?? "", options: ev.options ?? [], detail: ev.detail };
    },
  });
}

/**
 * The inbox actions. seen / snooze / resolve change only the Human's own display state (the daemon refuses a
 * permission key for the last two). Answering a permission is NOT here: it is the task page's own
 * `run.respondPermission`, called through {@link useRespondPermission}.
 */
export function useInboxActions() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  const call = useCallback(
    async (method: string, params: unknown) => {
      if (!client) throw new Error("no daemon connection");
      try {
        await client.call(method, params);
      } finally {
        void queryClient.invalidateQueries({ queryKey: queryKeys.inboxAll() });
      }
    },
    [client, queryClient],
  );
  return {
    seen: (keys: string[]) => call("inbox.seen", { keys }),
    snooze: (items: InboxItem[], ms: number) => {
      const keys = items.filter(canSnoozeOrResolve).map((i) => i.key);
      if (keys.length === 0) return Promise.resolve();
      return call("inbox.snooze", { keys, until: snoozeUntil(ms) });
    },
    resolve: (items: InboxItem[]) => {
      const keys = items.filter(canSnoozeOrResolve).map((i) => i.key);
      if (keys.length === 0) return Promise.resolve();
      return call("inbox.resolve", { keys });
    },
  };
}

/** `run.respondPermission`, exactly as the task page calls it; the inbox refreshes afterwards. */
export function useRespondPermission() {
  const { client } = useDaemonConnection();
  const queryClient = useQueryClient();
  return useCallback(
    async (runId: string, requestId: string, optionId: string) => {
      if (!client) throw new Error("no daemon connection");
      await client.call("run.respondPermission", { runId, requestId, optionId });
      void queryClient.invalidateQueries({ queryKey: queryKeys.inboxAll() });
    },
    [client, queryClient],
  );
}

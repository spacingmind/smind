import { useCallback, useEffect, useRef, useState } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { TaskStat, TaskStatsResult } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/** Per-task branch and diff size, keyed by task id. A task absent from this map has no stat -- not a zeroed one. */
export type TaskStats = ReadonlyMap<number, TaskStat>;

/**
 * Fetches `task.stats` once per workspace and keeps it roughly current.
 *
 * "Roughly" is the honest word: a diff stat is derived git state with no
 * event topic behind it (ADR 0009 explicitly declines to make git state a
 * lifecycle event, since `task.commit` changes the diff without changing
 * the task row). What actually moves a task's diff is an agent run, so a
 * run reaching a terminal state refetches the stats for *that run's
 * workspace only* -- each stat costs a `git add -A` into a throwaway index
 * (internal/workspace's snapshotIndex), so refetching every workspace on
 * every run would scan worktrees nobody is looking at.
 *
 * A task whose workspace this hook has not yet mapped -- one created since
 * the last fetch -- triggers no refetch, which is right: a brand-new task
 * has an empty diff, and the next run it finishes maps it.
 */
export function useTaskStats(
  client: WsClientLike | null,
  events: DaemonEvents | null,
  workspaceIds: readonly number[],
): TaskStats {
  const [stats, setStats] = useState<TaskStats>(new Map());
  /** task id -> the workspace whose fetch produced its stat, so a run.status can refetch just that one. */
  const workspaceByTaskRef = useRef<Map<number, number>>(new Map());

  const fetchWorkspace = useCallback(
    (fetchClient: WsClientLike, workspaceId: number, isStale: () => boolean) =>
      fetchClient
        .call<TaskStatsResult>("task.stats", { workspaceId })
        .then((result) => {
          if (isStale()) return;
          const incoming = result?.stats ?? [];
          for (const stat of incoming) workspaceByTaskRef.current.set(stat.taskId, workspaceId);
          setStats((prev) => {
            const next = new Map(prev);
            // Drop this workspace's previous entries first, so a task
            // that stopped reporting a stat (worktree removed) loses its
            // row signal instead of keeping a stale one.
            for (const [taskId, ws] of workspaceByTaskRef.current) {
              if (ws === workspaceId) next.delete(taskId);
            }
            for (const stat of incoming) next.set(stat.taskId, stat);
            return next;
          });
        })
        .catch(() => {
          // Best-effort: a failed stat fetch leaves the rows without this
          // signal, never in an error state. The sidebar tree itself is
          // fetched separately and is what actually has to succeed.
        }),
    [],
  );

  // `workspaceIds` is joined into a string so a caller re-deriving the
  // same ids from a refetched tree doesn't re-run this effect on identity
  // alone.
  const workspaceKey = workspaceIds.join(",");

  useEffect(() => {
    workspaceByTaskRef.current = new Map();
    setStats(new Map());
    if (!client || workspaceKey === "") return;

    let cancelled = false;
    const isStale = () => cancelled;
    for (const id of workspaceKey.split(",").map(Number)) {
      void fetchWorkspace(client, id, isStale);
    }

    return () => {
      cancelled = true;
    };
  }, [client, workspaceKey, fetchWorkspace]);

  useEffect(() => {
    if (!client || !events) return;
    let cancelled = false;
    const isStale = () => cancelled;

    const offRunStatus = events.subscribe("run.status", (payload) => {
      if (typeof payload !== "object" || payload === null) return;
      const p = payload as { taskId?: unknown; status?: unknown };
      if (typeof p.taskId !== "number" || typeof p.status !== "string") return;
      // Only a finished run can have changed the worktree in a way the
      // user is now looking at; a run *starting* has changed nothing yet.
      if (p.status === "running") return;
      const workspaceId = workspaceByTaskRef.current.get(p.taskId);
      if (workspaceId === undefined) return;
      void fetchWorkspace(client, workspaceId, isStale);
    });

    // ADR 0005's queue is per-connection: a drop can just as well have
    // swallowed the run.status that would have triggered the refetch
    // above, leaving a stat stale with nothing left to correct it short
    // of a reconnect. Re-fetch every workspace this hook currently knows
    // about, same as the initial mount does.
    const offDropped = events.subscribe("event.dropped", () => {
      for (const workspaceId of workspaceIds) void fetchWorkspace(client, workspaceId, isStale);
    });

    return () => {
      cancelled = true;
      offRunStatus();
      offDropped();
    };
  }, [client, events, fetchWorkspace, workspaceIds]);

  return stats;
}
